package scanner

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/tree"
)

// The MFT (Master File Table) is NTFS's index of every file: one fixed-size record
// (usually 1 KiB) per file or folder, numbered from 0. Reading it straight off the
// raw volume is far faster than asking Windows to list every folder.

const (
	mftChunk   = 4 << 20 // bytes read per ReadAt call
	rootRecord = 5       // the record of the volume's root folder
	refMask    = 1<<48 - 1

	// Attribute type codes.
	attrStdInfo  = 0x10 // $STANDARD_INFORMATION: times and file attributes
	attrFileName = 0x30 // $FILE_NAME: one per name (hard link), holds the parent folder
	attrData     = 0x80 // $DATA: the file's contents
	attrReparse  = 0xC0 // $REPARSE_POINT: junction/symlink target, starts with the tag
	attrEnd      = 0xFFFFFFFF

	// Record header flags.
	recFlagInUse = 0x01
	recFlagDir   = 0x02

	// $DATA attribute flags.
	dataCompressed = 0x0001
	dataSparse     = 0x8000

	nsDOS = 2 // $FILE_NAME namespace for 8.3 short names, which duplicate the long name
)

func scanMFT(ctx context.Context, root string, onProgress func(Progress)) (*Result, error) {
	root = normaliseRoot(root)
	if !isDriveRoot(root) {
		return nil, fmt.Errorf("MFT scan needs a drive root, got %s", root)
	}
	if err := checkNTFS(root); err != nil {
		return nil, err
	}
	start := time.Now()
	var c counters
	stop := c.report("mft", start, onProgress)
	m, err := readMFT(ctx, root, &c)
	if err != nil {
		stop()
		return nil, err
	}
	c.current.Store("Building tree")
	t := buildTree(root, m)
	stop()
	t.Finalize()

	used, err := volumeUsed(root)
	if err != nil {
		return nil, err
	}
	if err := checkTotals(t.Alloc(0), used); err != nil {
		return nil, err
	}
	return &Result{Tree: t, Method: "mft", Errors: c.errors, ErrCount: int(c.errs.Load()), Duration: time.Since(start)}, nil
}

func checkNTFS(root string) error {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return err
	}
	var fs [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); err != nil {
		return fmt.Errorf("reading volume information for %s: %w", root, err)
	}
	if name := windows.UTF16ToString(fs[:]); name != "NTFS" {
		return fmt.Errorf("%s is %s, not NTFS", root, name)
	}
	return nil
}

func volumeUsed(root string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, fmt.Errorf("reading free space for %s: %w", root, err)
	}
	return total - free, nil
}

// checkTotals rejects a scan whose total size on disk is far from what the volume
// says is in use: it means the MFT was misread, and the walker should run instead.
func checkTotals(alloc, used uint64) error {
	switch {
	case float64(alloc) > 1.1*float64(used):
		return fmt.Errorf("MFT scan found %d bytes on disk but the volume only uses %d", alloc, used)
	case float64(alloc) < 0.5*float64(used):
		return fmt.Errorf("MFT scan found only %d bytes on disk but the volume uses %d", alloc, used)
	}
	return nil
}

// readMFT opens the raw volume, finds the MFT through the boot sector and parses every record.
func readMFT(ctx context.Context, root string, c *counters) (*mftCollector, error) {
	path := `\\.\` + root[:2]
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// Opening a volume needs admin. Reads on it must be whole sectors at sector-aligned offsets.
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	f := os.NewFile(uintptr(h), path) // gives us ReadAt; Close closes h
	defer f.Close()

	boot := make([]byte, 4096) // one whole sector even on 4K-sector disks
	if _, err := f.ReadAt(boot, 0); err != nil {
		return nil, fmt.Errorf("reading boot sector: %w", err)
	}
	g, err := parseBoot(boot)
	if err != nil {
		return nil, err
	}

	// Record 0 is $MFT itself; its $DATA runlist says where the rest of the MFT lives.
	rec0 := make([]byte, max(g.recordSize, g.sectorSize))
	if _, err := f.ReadAt(rec0, g.mftLCN*int64(g.clusterSize)); err != nil {
		return nil, fmt.Errorf("reading MFT record 0: %w", err)
	}
	runs, mftSize, err := mftDataRuns(rec0[:g.recordSize], g.clusterSize)
	if err != nil {
		return nil, err
	}
	nrec := int(mftSize / uint64(g.recordSize))
	if uint64(nrec) >= math.MaxUint32 {
		return nil, fmt.Errorf("MFT has %d records, too many for 32-bit IDs", nrec)
	}

	m := newCollector(nrec)
	buf := make([]byte, mftChunk+g.recordSize)
	var scratch []fileName
	carry, recNo := 0, 0 // carry: bytes of a record split across two runs, kept at buf's start
	for _, r := range runs {
		if r.sparse {
			return nil, errors.New("MFT runlist has a sparse run")
		}
		off := r.lcn * int64(g.clusterSize)
		left := int64(r.length) * int64(g.clusterSize)
		for left > 0 && recNo < nrec {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			n := int(min(left, mftChunk))
			if _, err := f.ReadAt(buf[carry:carry+n], off); err != nil {
				return nil, fmt.Errorf("reading MFT at volume offset %d: %w", off, err)
			}
			data := buf[:carry+n]
			for len(data) >= g.recordSize && recNo < nrec {
				info, names, err := parseRecord(data[:g.recordSize], scratch[:0])
				scratch = names
				switch {
				case err != nil:
					c.fail(fmt.Sprintf("MFT record %d", recNo), err)
				case info.inUse:
					m.add(uint32(recNo), info, names)
					if !info.hasBase {
						if info.isDir {
							c.dirs.Add(1)
						} else {
							c.files.Add(1)
						}
					}
					c.bytes.Add(info.alloc)
				}
				data = data[g.recordSize:]
				recNo++
			}
			carry = copy(buf, data)
			off += int64(n)
			left -= int64(n)
			c.current.Store(fmt.Sprintf("Reading MFT %d%%", recNo*100/max(nrec, 1)))
		}
	}
	if recNo < nrec {
		return nil, fmt.Errorf("MFT runlist ends after %d of %d records", recNo, nrec)
	}
	return m, nil
}

// geometry is what the boot sector tells us about the volume's layout.
type geometry struct {
	sectorSize  int
	clusterSize int
	recordSize  int   // bytes per MFT record
	mftLCN      int64 // cluster where the MFT starts
}

func parseBoot(b []byte) (geometry, error) {
	le := binary.LittleEndian
	if len(b) < 512 || string(b[3:11]) != "NTFS    " {
		return geometry{}, errors.New("not an NTFS boot sector")
	}
	var g geometry
	g.sectorSize = int(le.Uint16(b[0x0B:]))
	spc := int(b[0x0D])
	if spc > 0x80 { // clusters of 64 KiB and up store sectors-per-cluster as a negative power of two
		spc = 1 << (256 - spc)
	}
	g.clusterSize = g.sectorSize * spc
	g.mftLCN = int64(le.Uint64(b[0x30:]))
	// Signed: positive means clusters per record, negative n means 2^-n bytes (the usual case: -10 = 1 KiB).
	if cpr := int(int8(b[0x40])); cpr > 0 {
		g.recordSize = cpr * g.clusterSize
	} else if -cpr < 31 {
		g.recordSize = 1 << -cpr
	}
	switch {
	case !isPow2(g.sectorSize) || g.sectorSize < 512 || g.sectorSize > 4096:
		return geometry{}, fmt.Errorf("unsupported sector size %d", g.sectorSize)
	case !isPow2(g.clusterSize) || g.clusterSize < g.sectorSize || g.clusterSize > 2<<20:
		return geometry{}, fmt.Errorf("unsupported cluster size %d", g.clusterSize)
	case !isPow2(g.recordSize) || g.recordSize < 512 || g.recordSize > 64<<10:
		return geometry{}, fmt.Errorf("unsupported MFT record size %d", g.recordSize)
	case g.mftLCN <= 0:
		return geometry{}, fmt.Errorf("bad MFT start cluster %d", g.mftLCN)
	}
	return g, nil
}

func isPow2(n int) bool { return n > 0 && n&(n-1) == 0 }

// applyFixup undoes NTFS's torn-write protection. When writing a record, NTFS copies
// the last two bytes of every 512-byte stride into the update sequence array (USA)
// and stamps a check value there instead. Reading means checking each stamp and
// putting the real bytes back. A mismatch means the record was half written.
func applyFixup(rec []byte) error {
	le := binary.LittleEndian
	if len(rec) < 0x30 {
		return errors.New("record too short")
	}
	usaOff, usaCount := int(le.Uint16(rec[4:])), int(le.Uint16(rec[6:]))
	if usaCount < 2 || usaOff+2*usaCount > len(rec) {
		return fmt.Errorf("bad update sequence array (offset %d, count %d)", usaOff, usaCount)
	}
	stride := len(rec) / (usaCount - 1)
	if stride*(usaCount-1) != len(rec) || !isPow2(stride) || stride < 512 {
		return fmt.Errorf("update sequence count %d does not fit a %d-byte record", usaCount, len(rec))
	}
	check := rec[usaOff : usaOff+2]
	for i := 1; i < usaCount; i++ {
		end := i*stride - 2
		if rec[end] != check[0] || rec[end+1] != check[1] {
			return fmt.Errorf("update sequence mismatch in stride %d (torn write)", i)
		}
		copy(rec[end:end+2], rec[usaOff+2*i:])
	}
	return nil
}

// forEachAttr calls fn for every attribute in a record whose fixups are applied.
// It bounds-checks the header fields fn relies on: the length, the name, and for
// resident attributes the value; a non-resident attribute is at least 0x40 bytes.
//
// Attribute header: 0x00 type, 0x04 length, 0x08 non-resident flag, 0x09 name length
// (UTF-16 units), 0x0A name offset, 0x0C flags. Resident: 0x10 value length, 0x14 value offset.
func forEachAttr(rec []byte, fn func(typ uint32, a []byte) error) error {
	le := binary.LittleEndian
	first, used := int(le.Uint16(rec[0x14:])), int(le.Uint32(rec[0x18:]))
	if used > len(rec) || first < 0x2A || first >= used {
		return fmt.Errorf("bad record header (first attribute %d, bytes in use %d)", first, used)
	}
	for off := first; ; {
		if off+4 > used {
			return errors.New("attribute list has no end marker")
		}
		typ := le.Uint32(rec[off:])
		if typ == attrEnd {
			return nil
		}
		if off+0x18 > used {
			return fmt.Errorf("attribute at %d is truncated", off)
		}
		n := int(le.Uint32(rec[off+4:]))
		if n < 0x18 || n > used-off {
			return fmt.Errorf("attribute at %d has bad length %d", off, n)
		}
		a := rec[off : off+n]
		if nameEnd := int(le.Uint16(a[0x0A:])) + 2*int(a[9]); nameEnd > n {
			return fmt.Errorf("attribute at %d has its name out of bounds", off)
		}
		if a[8] != 0 {
			if n < 0x40 {
				return fmt.Errorf("non-resident attribute at %d is too short", off)
			}
		} else if int(le.Uint16(a[0x14:]))+int(le.Uint32(a[0x10:])) > n {
			return fmt.Errorf("attribute at %d has its value out of bounds", off)
		}
		if err := fn(typ, a); err != nil {
			return err
		}
		off += n
	}
}

// residentValue returns a resident attribute's value, or nil for a non-resident one.
func residentValue(a []byte) []byte {
	if a[8] != 0 {
		return nil
	}
	le := binary.LittleEndian
	off := int(le.Uint16(a[0x14:]))
	return a[off : off+int(le.Uint32(a[0x10:]))]
}

// recordInfo is what one MFT record contributes. Sizes and attributes may instead
// arrive in an extension record, which carries the base record's reference.
type recordInfo struct {
	inUse, isDir bool
	seq          uint16 // bumped each time the record is reused; parent references must match it
	hasBase      bool
	base         uint64 // base record reference (record number in the low 48 bits)
	hasStdInfo   bool
	attrs        uint32 // FILE_ATTRIBUTE_* from $STANDARD_INFORMATION
	mtime        int64  // Unix seconds
	hasData      bool
	size, alloc  uint64
	reparseTag   uint32
}

// fileName is one $FILE_NAME. name is UTF-16LE and aliases the record buffer.
type fileName struct {
	parent uint64 // parent folder reference: record number in the low 48 bits, sequence in the high 16
	name   []byte
}

// parseRecord parses one MFT record in place (it applies the fixups to rec). Names are
// appended to names and alias rec, so use them before rec is overwritten. A free or
// never-used record returns inUse false and no error.
//
// Record header: 0x00 "FILE", 0x04 USA offset, 0x06 USA count, 0x10 sequence number,
// 0x14 first attribute offset, 0x16 flags, 0x18 bytes in use, 0x20 base record reference.
func parseRecord(rec []byte, names []fileName) (recordInfo, []fileName, error) {
	le := binary.LittleEndian
	if len(rec) < 0x30 {
		return recordInfo{}, names, errors.New("record too short")
	}
	switch string(rec[:4]) {
	case "FILE":
	case "\x00\x00\x00\x00":
		return recordInfo{}, names, nil // never used
	default:
		return recordInfo{}, names, fmt.Errorf("bad record signature %q", rec[:4])
	}
	flags := le.Uint16(rec[0x16:])
	if flags&recFlagInUse == 0 {
		return recordInfo{}, names, nil
	}
	if err := applyFixup(rec); err != nil {
		return recordInfo{}, names, err
	}
	base := le.Uint64(rec[0x20:])
	info := recordInfo{
		inUse:   true,
		isDir:   flags&recFlagDir != 0,
		seq:     le.Uint16(rec[0x10:]),
		hasBase: base != 0,
		base:    base,
	}
	err := forEachAttr(rec, func(typ uint32, a []byte) error {
		switch typ {
		case attrStdInfo:
			// Value: 0x00 created, 0x08 modified, 0x10 MFT changed, 0x18 accessed, 0x20 attributes.
			v := residentValue(a)
			if len(v) < 0x24 {
				return errors.New("$STANDARD_INFORMATION too short")
			}
			info.hasStdInfo = true
			info.mtime = filetimeToUnix(int64(le.Uint64(v[0x08:])))
			info.attrs = le.Uint32(v[0x20:])
		case attrFileName:
			// Value: 0x00 parent reference, 0x40 name length (UTF-16 units), 0x41 namespace, 0x42 name.
			v := residentValue(a)
			if len(v) < 0x42 {
				return errors.New("$FILE_NAME too short")
			}
			n := 2 * int(v[0x40])
			if n == 0 || 0x42+n > len(v) {
				return errors.New("$FILE_NAME has a bad name length")
			}
			if v[0x41] != nsDOS {
				names = append(names, fileName{parent: le.Uint64(v), name: v[0x42 : 0x42+n]})
			}
		case attrData:
			if a[9] != 0 {
				return nil // a named stream (alternate data stream), not the file's contents
			}
			if a[8] == 0 {
				// Resident: the bytes live inside this MFT record, which $MFT already counts.
				info.hasData, info.size, info.alloc = true, uint64(len(residentValue(a))), 0
				return nil
			}
			// Non-resident: 0x10 starting VCN, 0x20 runlist offset, 0x28 allocated size,
			// 0x30 real size, 0x40 total allocated (only when compressed or sparse).
			// A long runlist is split across records; only the piece starting at VCN 0 has the sizes.
			if le.Uint64(a[0x10:]) != 0 {
				return nil
			}
			info.hasData = true
			info.size, info.alloc = le.Uint64(a[0x30:]), le.Uint64(a[0x28:])
			if le.Uint16(a[0x0C:])&(dataCompressed|dataSparse) != 0 {
				if len(a) < 0x48 || le.Uint16(a[0x20:]) < 0x48 {
					return errors.New("compressed $DATA has no total allocated size")
				}
				info.alloc = le.Uint64(a[0x40:])
			}
		case attrReparse:
			// shortcut: a non-resident reparse point (huge reparse data) leaves the tag unread, so it is not marked Reparse.
			if v := residentValue(a); len(v) >= 4 {
				info.reparseTag = le.Uint32(v)
			}
		}
		return nil
	})
	if err != nil {
		return recordInfo{}, names, err
	}
	return info, names, nil
}

// mftDataRuns finds where the MFT lives from record 0 ($MFT). It returns the
// runlist and the MFT's size in bytes.
func mftDataRuns(rec []byte, clusterSize int) ([]run, uint64, error) {
	le := binary.LittleEndian
	if len(rec) < 0x30 || string(rec[:4]) != "FILE" {
		return nil, 0, errors.New("MFT record 0 has no FILE signature")
	}
	if err := applyFixup(rec); err != nil {
		return nil, 0, fmt.Errorf("MFT record 0: %w", err)
	}
	var (
		runs  []run
		size  uint64
		found bool
	)
	err := forEachAttr(rec, func(typ uint32, a []byte) error {
		if typ != attrData || a[9] != 0 || a[8] == 0 || le.Uint64(a[0x10:]) != 0 {
			return nil
		}
		mp := int(le.Uint16(a[0x20:]))
		if mp > len(a) {
			return errors.New("$MFT runlist offset out of bounds")
		}
		var err error
		if runs, err = decodeRuns(a[mp:]); err != nil {
			return fmt.Errorf("$MFT runlist: %w", err)
		}
		var clusters uint64
		for _, r := range runs {
			clusters += r.length
		}
		size = le.Uint64(a[0x30:])
		// A very fragmented MFT continues its runlist in extension records found via
		// $ATTRIBUTE_LIST. That is rare enough to leave to the walker.
		if clusters != le.Uint64(a[0x18:])+1 || size > clusters*uint64(clusterSize) {
			return errors.New("$MFT runlist continues in an extension record; not supported")
		}
		found = true
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if !found {
		return nil, 0, errors.New("MFT record 0 has no non-resident $DATA")
	}
	return runs, size, nil
}

// run is one extent of a non-resident attribute: length clusters starting at cluster lcn.
type run struct {
	lcn    int64
	length uint64
	sparse bool // no clusters on disk (reads as zeros)
}

// decodeRuns decodes a runlist ("mapping pairs"). Each run starts with a header byte:
// the low nibble is the size of the length field, the high nibble the size of the
// offset field. The offset is a signed delta from the previous run's LCN; a run with
// no offset is sparse. A zero header byte ends the list.
func decodeRuns(b []byte) ([]run, error) {
	var (
		runs []run
		lcn  int64
	)
	for i := 0; i < len(b) && b[i] != 0; {
		lenSize, offSize := int(b[i]&0x0F), int(b[i]>>4)
		i++
		if lenSize == 0 || lenSize > 8 || offSize > 8 || i+lenSize+offSize > len(b) {
			return nil, fmt.Errorf("bad run header at byte %d", i-1)
		}
		length := uint64(leInt(b[i:i+lenSize], false))
		i += lenSize
		// NTFS volumes have at most 2^32 clusters; this also keeps length*clusterSize from overflowing.
		if length == 0 || length >= 1<<32 {
			return nil, fmt.Errorf("bad run length %d", length)
		}
		if offSize == 0 {
			runs = append(runs, run{length: length, sparse: true})
			continue
		}
		lcn += leInt(b[i:i+offSize], true)
		i += offSize
		if lcn < 0 {
			return nil, fmt.Errorf("run starts at negative cluster %d", lcn)
		}
		runs = append(runs, run{lcn: lcn, length: length})
	}
	return runs, nil
}

// leInt reads a little-endian integer of 1-8 bytes, sign-extending it if signed.
func leInt(b []byte, signed bool) int64 {
	var v uint64
	for k := len(b) - 1; k >= 0; k-- {
		v = v<<8 | uint64(b[k])
	}
	if shift := 64 - 8*len(b); signed && shift > 0 {
		return int64(v<<shift) >> shift
	}
	return int64(v)
}

// mftRec is the merged state of one file or folder, indexed by record number.
type mftRec struct {
	size, alloc uint64
	mtime       int64
	attrs       uint32
	reparseTag  uint32
	seq         uint16
	state       uint8
}

const (
	recInUse   uint8 = 1 << iota
	recDir           // has a folder index
	recCounted       // a link already carries this file's size
	recVisited       // folder already added to the tree
)

// mftName is one name (hard link) of a record; the text is in mftCollector.nameBuf.
type mftName struct {
	rec, parent uint32
	nameOff     uint32
	parentSeq   uint16
	nameLen     uint16
}

// mftCollector gathers parsed records in flat slices, so millions of files cost a few
// large allocations rather than one per file.
type mftCollector struct {
	recs    []mftRec
	names   []mftName
	nameBuf []byte // UTF-8
}

func newCollector(nrec int) *mftCollector {
	return &mftCollector{recs: make([]mftRec, nrec), names: make([]mftName, 0, nrec)}
}

// add merges one in-use record. An extension record's attributes go to its base record,
// which is how files with an $ATTRIBUTE_LIST are handled without parsing that list.
func (m *mftCollector) add(recNo uint32, info recordInfo, names []fileName) {
	target := uint64(recNo)
	if info.hasBase {
		target = info.base & refMask
	}
	if target >= uint64(len(m.recs)) {
		return
	}
	r := &m.recs[target]
	if !info.hasBase {
		r.state |= recInUse
		r.seq = info.seq
		if info.isDir {
			r.state |= recDir
		}
	}
	if info.hasStdInfo {
		r.attrs, r.mtime = info.attrs, info.mtime
	}
	if info.hasData {
		r.size, r.alloc = info.size, info.alloc
	}
	if info.reparseTag != 0 {
		r.reparseTag = info.reparseTag
	}
	for _, n := range names {
		parent := n.parent & refMask
		if parent >= uint64(len(m.recs)) {
			continue
		}
		off := len(m.nameBuf)
		m.nameBuf = appendUTF16(m.nameBuf, n.name)
		m.names = append(m.names, mftName{
			rec: uint32(target), parent: uint32(parent), parentSeq: uint16(n.parent >> 48),
			nameOff: uint32(off), nameLen: uint16(len(m.nameBuf) - off),
		})
	}
}

// appendUTF16 appends UTF-16LE text as UTF-8. Unpaired surrogates become U+FFFD.
func appendUTF16(dst, b []byte) []byte {
	le := binary.LittleEndian
	for i := 0; i+1 < len(b); i += 2 {
		r := rune(le.Uint16(b[i:]))
		if utf16.IsSurrogate(r) && i+3 < len(b) {
			if pair := utf16.DecodeRune(r, rune(le.Uint16(b[i+2:]))); pair != utf8.RuneError {
				r = pair
				i += 2
			}
		}
		dst = utf8.AppendRune(dst, r)
	}
	return dst
}

// buildTree turns the collected records into a tree, breadth first from the root
// record so every parent gets a smaller tree ID than its children. Names whose
// parent chain never reaches the root (orphans) are left out. It updates m's state bits.
func buildTree(root string, m *mftCollector) *tree.Tree {
	n := len(m.recs)
	// Group names by parent record with a counting sort: the children of record p
	// are kids[start[p]:start[p+1]].
	start := make([]uint32, n+1)
	for _, nm := range m.names {
		start[nm.parent+1]++
	}
	for i := 1; i <= n; i++ {
		start[i] += start[i-1]
	}
	kids := make([]uint32, len(m.names))
	next := append([]uint32(nil), start[:n]...)
	for i, nm := range m.names {
		kids[next[nm.parent]] = uint32(i)
		next[nm.parent]++
	}

	t := tree.New(root)
	t.Reserve(len(m.names) + 1)
	if rootRecord >= n || m.recs[rootRecord].state&(recInUse|recDir) != recInUse|recDir {
		return t
	}
	m.recs[rootRecord].state |= recVisited
	type dir struct {
		rec uint32
		id  tree.ID
	}
	queue := []dir{{rootRecord, t.Root()}}
	for qi := 0; qi < len(queue); qi++ {
		d := queue[qi]
		parentSeq := m.recs[d.rec].seq
		for _, k := range kids[start[d.rec]:start[d.rec+1]] {
			nm := m.names[k]
			r := &m.recs[nm.rec]
			// Skips the root's "." self-reference, free records, and names pointing at
			// an earlier occupant of the parent's record (sequence mismatch).
			if nm.rec == d.rec || r.state&recInUse == 0 || nm.parentSeq != parentSeq {
				continue
			}
			flags := attrFlags(r.attrs &^ windows.FILE_ATTRIBUTE_DIRECTORY)
			if isNameSurrogate(r.reparseTag) {
				flags |= tree.Reparse
			}
			// Add copies the bytes, so the string need not outlive nameBuf.
			name := unsafe.String(unsafe.SliceData(m.nameBuf[nm.nameOff:]), int(nm.nameLen))
			if r.state&recDir != 0 {
				if r.state&recVisited != 0 {
					continue // folders cannot be hard linked; a second parent means corruption
				}
				r.state |= recVisited
				id := t.Add(d.id, name, flags|tree.Dir, 0, 0, r.mtime)
				queue = append(queue, dir{nm.rec, id})
				continue
			}
			size, alloc := r.size, r.alloc
			if r.state&recCounted != 0 {
				flags |= tree.HardLink
				size, alloc = 0, 0
			} else {
				r.state |= recCounted
			}
			t.Add(d.id, name, flags, size, alloc, r.mtime)
		}
	}
	return t
}
