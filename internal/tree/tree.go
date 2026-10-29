package tree

import (
	"math"
	"sort"
	"strings"
)

// ID indexes a node. IDs are only meaningful for the Tree that issued them.
type ID = uint32

// None marks "no node" in parent/child/sibling links.
const None ID = math.MaxUint32

// Flags describe a node. They fit in a uint16 to keep nodes small.
type Flags uint16

const (
	Dir          Flags = 1 << iota
	HardLink           // an extra link to a file already counted elsewhere; its size is not counted
	Reparse            // a junction or symlink; never descended into
	Cloud              // a cloud placeholder whose data is not on disk
	Inaccessible       // a folder we could not read
	Removed            // moved to quarantine since the scan
	Compressed
	Sparse
	Hidden
	System
)

// Tree stores scan results as parallel slices (struct-of-arrays) rather than
// one heap object per file; BenchmarkBuild reports the bytes per node. It is not
// safe for concurrent use.
//
// Invariant: a node's parent always has a smaller ID than the node, which lets
// Finalize total every folder in one reverse pass.
type Tree struct {
	parent      []ID
	firstChild  []ID
	nextSibling []ID
	nameOff     []uint32
	nameLen     []uint16
	flags       []Flags
	size        []uint64 // logical bytes; folders hold their subtree total after Finalize
	alloc       []uint64 // bytes on disk
	mtime       []int64  // Unix seconds
	files       []uint32 // files in the subtree (folders only)
	names       []byte   // every name, UTF-8, back to back
}

// New creates a tree whose root is the given path, for example `C:\`.
func New(rootPath string) *Tree {
	t := &Tree{}
	t.Add(None, rootPath, Dir, 0, 0, 0)
	return t
}

// Reserve grows capacity up front when the entry count is known (the MFT scanner knows it).
func (t *Tree) Reserve(n int) {
	grow := func(s []ID) []ID { return append(make([]ID, 0, n), s...) }
	t.parent, t.firstChild, t.nextSibling = grow(t.parent), grow(t.firstChild), grow(t.nextSibling)
	t.nameOff = append(make([]uint32, 0, n), t.nameOff...)
	t.nameLen = append(make([]uint16, 0, n), t.nameLen...)
	t.flags = append(make([]Flags, 0, n), t.flags...)
	t.size = append(make([]uint64, 0, n), t.size...)
	t.alloc = append(make([]uint64, 0, n), t.alloc...)
	t.mtime = append(make([]int64, 0, n), t.mtime...)
	t.files = append(make([]uint32, 0, n), t.files...)
	t.names = append(make([]byte, 0, n*16), t.names...)
}

// Root is always node 0.
func (t *Tree) Root() ID { return 0 }

// Len is the number of nodes, including the root.
func (t *Tree) Len() int { return len(t.parent) }

// Add appends a child of parent and returns its ID. Folder sizes are filled in by Finalize.
func (t *Tree) Add(parent ID, name string, flags Flags, size, alloc uint64, mtime int64) ID {
	id := ID(len(t.parent))
	if len(name) > math.MaxUint16 {
		name = name[:math.MaxUint16]
	}
	t.parent = append(t.parent, parent)
	t.firstChild = append(t.firstChild, None)
	t.nextSibling = append(t.nextSibling, None)
	t.nameOff = append(t.nameOff, uint32(len(t.names)))
	t.nameLen = append(t.nameLen, uint16(len(name)))
	t.names = append(t.names, name...)
	t.flags = append(t.flags, flags)
	t.size = append(t.size, size)
	t.alloc = append(t.alloc, alloc)
	t.mtime = append(t.mtime, mtime)
	t.files = append(t.files, 0)
	if parent != None {
		t.nextSibling[id] = t.firstChild[parent]
		t.firstChild[parent] = id
	}
	return id
}

// Finalize totals sizes and file counts into every folder. Call it once, after the last Add.
func (t *Tree) Finalize() {
	for id := len(t.parent) - 1; id > 0; id-- {
		if t.flags[id]&Removed != 0 {
			continue
		}
		p := t.parent[id]
		t.size[p] += t.size[id]
		t.alloc[p] += t.alloc[id]
		if t.flags[id]&Dir != 0 {
			t.files[p] += t.files[id]
		} else {
			t.files[p]++
		}
	}
}

// Remove marks a node as gone and takes its size out of every ancestor.
func (t *Tree) Remove(id ID) {
	if id == 0 || t.flags[id]&Removed != 0 {
		return
	}
	t.flags[id] |= Removed
	files := t.files[id]
	if t.flags[id]&Dir == 0 {
		files = 1
	}
	for p := t.parent[id]; p != None; p = t.parent[p] {
		t.size[p] -= t.size[id]
		t.alloc[p] -= t.alloc[id]
		t.files[p] -= files
	}
}

func (t *Tree) Parent(id ID) ID     { return t.parent[id] }
func (t *Tree) Flags(id ID) Flags   { return t.flags[id] }
func (t *Tree) IsDir(id ID) bool    { return t.flags[id]&Dir != 0 }
func (t *Tree) Size(id ID) uint64   { return t.size[id] }
func (t *Tree) Alloc(id ID) uint64  { return t.alloc[id] }
func (t *Tree) ModTime(id ID) int64 { return t.mtime[id] }
func (t *Tree) Files(id ID) uint32  { return t.files[id] }

// SetFlag adds flags to a node.
func (t *Tree) SetFlag(id ID, f Flags) { t.flags[id] |= f }

func (t *Tree) Name(id ID) string {
	off := t.nameOff[id]
	return string(t.names[off : off+uint32(t.nameLen[id])])
}

// Children returns the live (not removed) children of id, in no particular order.
func (t *Tree) Children(id ID) []ID {
	var out []ID
	for c := t.firstChild[id]; c != None; c = t.nextSibling[c] {
		if t.flags[c]&Removed == 0 {
			out = append(out, c)
		}
	}
	return out
}

// Sort orders ids by a column: "name", "files", "modified", "logical" (file length)
// or, for anything else, size on disk. Ties keep their order.
func (t *Tree) Sort(ids []ID, by string, ascending bool) {
	less := func(x, y ID) bool { return t.alloc[x] < t.alloc[y] }
	switch by {
	case "name":
		less = func(x, y ID) bool { return strings.ToLower(t.Name(x)) < strings.ToLower(t.Name(y)) }
	case "files":
		less = func(x, y ID) bool { return t.files[x] < t.files[y] }
	case "modified":
		less = func(x, y ID) bool { return t.mtime[x] < t.mtime[y] }
	case "logical":
		less = func(x, y ID) bool { return t.size[x] < t.size[y] }
	}
	sort.SliceStable(ids, func(i, j int) bool {
		if ascending {
			return less(ids[i], ids[j])
		}
		return less(ids[j], ids[i])
	})
}

// Path returns the full Windows path of a node.
func (t *Tree) Path(id ID) string {
	var parts []string
	for n := id; n != None; n = t.parent[n] {
		parts = append(parts, t.Name(n))
	}
	var b strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteString(parts[i])
		if i > 0 && !strings.HasSuffix(parts[i], `\`) {
			b.WriteByte('\\')
		}
	}
	return b.String()
}

// Find returns the node for a full path, matching names case-insensitively as Windows does.
func (t *Tree) Find(path string) (ID, bool) {
	root := t.Name(0)
	if len(path) < len(root) || !strings.EqualFold(path[:len(root)], root) {
		return None, false
	}
	id := ID(0)
	for _, seg := range strings.Split(strings.Trim(path[len(root):], `\`), `\`) {
		if seg == "" {
			continue
		}
		next := None
		for c := t.firstChild[id]; c != None; c = t.nextSibling[c] {
			if t.flags[c]&Removed == 0 && strings.EqualFold(t.Name(c), seg) {
				next = c
				break
			}
		}
		if next == None {
			return None, false
		}
		id = next
	}
	return id, true
}

// Count is how many live nodes under id, including id, have any of flags.
func (t *Tree) Count(id ID, flags Flags) int {
	n := 0
	t.Walk(id, func(c ID) bool {
		if t.flags[c]&flags != 0 {
			n++
		}
		return true
	})
	return n
}

// Walk visits id and every live descendant, depth first. Returning false from fn skips that node's children.
func (t *Tree) Walk(id ID, fn func(ID) bool) {
	stack := []ID{id}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if t.flags[n]&Removed != 0 || !fn(n) {
			continue
		}
		for c := t.firstChild[n]; c != None; c = t.nextSibling[c] {
			stack = append(stack, c)
		}
	}
}
