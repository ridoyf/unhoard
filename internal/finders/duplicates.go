package finders

import (
	"context"
	"crypto/sha256"
	"io"
	"os"
	"sort"

	"github.com/ridoyf/unhoard/internal/tree"
)

// DupGroup is a set of files with identical content.
type DupGroup struct {
	IDs   []tree.ID
	Bytes uint64 // size on disk of one copy
}

// LiveDups drops files removed from t since the search, and groups left with one file.
func LiveDups(t *tree.Tree, groups []DupGroup) []DupGroup {
	var out []DupGroup
	for _, g := range groups {
		var ids []tree.ID
		for _, id := range g.IDs {
			if t.Flags(id)&tree.Removed == 0 {
				ids = append(ids, id)
			}
		}
		if len(ids) > 1 {
			out = append(out, DupGroup{IDs: ids, Bytes: g.Bytes})
		}
	}
	return out
}

// DupProgress reports duplicate-search progress.
type DupProgress struct {
	Checked uint64 `json:"checked"`
	Total   uint64 `json:"total"`
}

// DupCandidate is a file that shares its size with at least one other file.
type DupCandidate struct {
	ID    tree.ID
	Path  string
	Size  uint64
	Alloc uint64
}

const partialBytes = 64 * 1024

// DupCandidates groups files under root of at least minBytes by size and keeps
// sizes shared by two or more files. It is the only step that reads the tree, so
// callers can hold a lock for it and hash without one. Cloud placeholders are
// skipped (reading them would download them) and extra hard links are not duplicates.
func DupCandidates(t *tree.Tree, root tree.ID, minBytes uint64) [][]DupCandidate {
	minBytes = max(minBytes, 1)
	bySize := map[uint64][]tree.ID{}
	t.Walk(root, func(id tree.ID) bool {
		if !t.IsDir(id) && t.Flags(id)&(skip|tree.Cloud|tree.Reparse) == 0 && t.Size(id) >= minBytes {
			bySize[t.Size(id)] = append(bySize[t.Size(id)], id)
		}
		return true
	})
	var out [][]DupCandidate
	for size, ids := range bySize {
		if len(ids) < 2 {
			continue
		}
		g := make([]DupCandidate, 0, len(ids))
		for _, id := range ids {
			g = append(g, DupCandidate{id, t.Path(id), size, t.Alloc(id)})
		}
		out = append(out, g)
	}
	return out
}

// Duplicates confirms candidates by hashing the first 64 KB, then the whole file.
func Duplicates(ctx context.Context, candidates [][]DupCandidate, onProgress func(DupProgress)) ([]DupGroup, error) {
	var total, checked uint64
	for _, g := range candidates {
		total += uint64(len(g))
	}
	var groups []DupGroup
	for _, sameSize := range candidates {
		for _, sameStart := range groupBy(ctx, sameSize, partialBytes) {
			if sameStart[0].Size <= partialBytes { // the partial hash already covered the whole file
				groups = append(groups, group(sameStart))
				continue
			}
			for _, g := range groupBy(ctx, sameStart, -1) {
				groups = append(groups, group(g))
			}
		}
		checked += uint64(len(sameSize))
		if onProgress != nil {
			onProgress(DupProgress{Checked: checked, Total: total})
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Bytes*uint64(len(groups[i].IDs)-1) > groups[j].Bytes*uint64(len(groups[j].IDs)-1)
	})
	return groups, nil
}

func group(cs []DupCandidate) DupGroup {
	g := DupGroup{Bytes: cs[0].Alloc}
	for _, c := range cs {
		g.IDs = append(g.IDs, c.ID)
	}
	return g
}

// groupBy hashes the first n bytes of each file (n < 0 means the whole file) and
// returns groups of two or more with equal hashes. Unreadable files are dropped.
func groupBy(ctx context.Context, cs []DupCandidate, n int64) [][]DupCandidate {
	byHash := map[[32]byte][]DupCandidate{}
	for _, c := range cs {
		if ctx.Err() != nil {
			return nil
		}
		sum, err := hashFile(c.Path, n)
		if err != nil {
			continue
		}
		byHash[sum] = append(byHash[sum], c)
	}
	var out [][]DupCandidate
	for _, g := range byHash {
		if len(g) > 1 {
			out = append(out, g)
		}
	}
	return out
}

func hashFile(path string, n int64) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	h := sha256.New()
	var r io.Reader = f
	if n >= 0 {
		r = io.LimitReader(f, n)
	}
	if _, err := io.Copy(h, r); err != nil {
		return [32]byte{}, err
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
