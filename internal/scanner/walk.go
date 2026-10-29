package scanner

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/tree"
)

// walkWorkers bounds concurrent directory reads.
// shortcut: one fixed number for SSDs and HDDs, tune per drive type if HDD scans thrash.
const walkWorkers = 16

type walkJob struct {
	id   tree.ID
	path string
}

// Walk scans root with a parallel directory walk. It needs no admin rights.
func Walk(ctx context.Context, root string, onProgress func(Progress)) (*Result, error) {
	root = normaliseRoot(root)
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("cannot scan %s: not a readable folder", root)
	}
	start := time.Now()
	t := tree.New(root)
	var c counters
	stop := c.report("walk", start, onProgress)

	var (
		mu     sync.Mutex // guards t and seen
		seen   = map[uint64]struct{}{}
		qmu    sync.Mutex
		cond   = sync.NewCond(&qmu)
		queue  = []walkJob{{0, root}}
		active int
		wg     sync.WaitGroup
	)
	for range walkWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				qmu.Lock()
				for len(queue) == 0 && active > 0 {
					cond.Wait()
				}
				if len(queue) == 0 || ctx.Err() != nil {
					qmu.Unlock()
					cond.Broadcast()
					return
				}
				job := queue[len(queue)-1]
				queue = queue[:len(queue)-1]
				active++
				qmu.Unlock()

				c.current.Store(job.path)
				entries, err := readDir(job.path)
				var next []walkJob
				mu.Lock()
				if err != nil {
					t.SetFlag(job.id, tree.Inaccessible)
					c.fail(job.path, err)
				}
				for _, e := range entries {
					flags, size, alloc := e.flags, e.size, e.alloc
					if flags&tree.Dir == 0 && e.fileID != 0 {
						if _, dup := seen[e.fileID]; dup {
							flags |= tree.HardLink
							size, alloc = 0, 0
						} else {
							seen[e.fileID] = struct{}{}
						}
					}
					id := t.Add(job.id, e.name, flags, size, alloc, e.mtime)
					if flags&tree.Dir != 0 {
						c.dirs.Add(1)
						if flags&tree.Reparse == 0 {
							next = append(next, walkJob{id, joinPath(job.path, e.name)})
						}
					} else {
						c.files.Add(1)
						c.bytes.Add(alloc)
					}
				}
				mu.Unlock()

				qmu.Lock()
				queue = append(queue, next...)
				active--
				qmu.Unlock()
				cond.Broadcast()
			}
		}()
	}
	wg.Wait()
	stop()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.Finalize()
	return &Result{Tree: t, Method: "walk", Errors: c.errors, ErrCount: int(c.errs.Load()), Duration: time.Since(start)}, nil
}

func joinPath(dir, name string) string {
	if dir[len(dir)-1] == '\\' {
		return dir + name
	}
	return dir + `\` + name
}

type dirEntry struct {
	name   string
	flags  tree.Flags
	size   uint64
	alloc  uint64
	mtime  int64
	fileID uint64
}

// readDir lists one folder using GetFileInformationByHandleEx(FileIdBothDirectoryInfo).
// Each call fills a buffer with many FILE_ID_BOTH_DIR_INFO records, so a folder of
// thousands of files costs a handful of syscalls instead of one stat per file.
func readDir(dir string) ([]dirEntry, error) {
	// The \\?\ prefix lifts the 260-character MAX_PATH limit.
	p, err := windows.UTF16PtrFromString(`\\?\` + dir)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)

	buf := make([]byte, 64*1024)
	var out []dirEntry
	for {
		err := windows.GetFileInformationByHandleEx(h, windows.FileIdBothDirectoryInfo, &buf[0], uint32(len(buf)))
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = parseDirInfo(buf, out)
	}
}

// FILE_ID_BOTH_DIR_INFO field offsets.
const (
	offNext       = 0
	offWriteTime  = 24
	offEndOfFile  = 40
	offAllocation = 48
	offAttributes = 56
	offNameLength = 60
	offEaSize     = 64 // holds the reparse tag when the entry is a reparse point
	offFileID     = 96
	offName       = 104
)

func parseDirInfo(buf []byte, out []dirEntry) []dirEntry {
	le := binary.LittleEndian
	for off := 0; ; {
		rec := buf[off:]
		nameLen := int(le.Uint32(rec[offNameLength:]))
		if s := string(appendUTF16(nil, rec[offName:offName+nameLen])); s != "." && s != ".." {
			attrs := le.Uint32(rec[offAttributes:])
			e := dirEntry{
				name:   s,
				size:   le.Uint64(rec[offEndOfFile:]),
				alloc:  le.Uint64(rec[offAllocation:]),
				mtime:  filetimeToUnix(int64(le.Uint64(rec[offWriteTime:]))),
				fileID: le.Uint64(rec[offFileID:]),
				flags:  attrFlags(attrs),
			}
			if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 && isNameSurrogate(le.Uint32(rec[offEaSize:])) {
				e.flags |= tree.Reparse
			}
			if e.flags&tree.Dir != 0 {
				e.size, e.alloc = 0, 0
			}
			out = append(out, e)
		}
		next := le.Uint32(rec[offNext:])
		if next == 0 {
			return out
		}
		off += int(next)
	}
}

func attrFlags(attrs uint32) tree.Flags {
	var f tree.Flags
	if attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		f |= tree.Dir
	}
	if attrs&(windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS|windows.FILE_ATTRIBUTE_OFFLINE) != 0 {
		f |= tree.Cloud
	}
	if attrs&windows.FILE_ATTRIBUTE_COMPRESSED != 0 {
		f |= tree.Compressed
	}
	if attrs&windows.FILE_ATTRIBUTE_SPARSE_FILE != 0 {
		f |= tree.Sparse
	}
	if attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0 {
		f |= tree.Hidden
	}
	if attrs&windows.FILE_ATTRIBUTE_SYSTEM != 0 {
		f |= tree.System
	}
	return f
}

// isNameSurrogate reports reparse tags that point somewhere else (junctions,
// symlinks). Following them would double count or loop. Cloud-file and dedup tags
// are not surrogates: those folders and files hold real data and are scanned.
func isNameSurrogate(tag uint32) bool { return tag&0x20000000 != 0 }

// filetimeToUnix converts 100ns ticks since 1601 to Unix seconds.
func filetimeToUnix(ft int64) int64 {
	if ft == 0 {
		return 0
	}
	return (ft - 116444736000000000) / 10_000_000
}
