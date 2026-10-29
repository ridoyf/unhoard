package treemap

import (
	"github.com/ridoyf/unhoard/internal/tree"
)

// Tile is one rectangle of a folder's treemap, in pixels. ID is tree.None for
// the tile that stands in for Others items too small to draw.
type Tile struct {
	ID         tree.ID
	Bytes      uint64
	Others     int
	Depth      int
	X, Y, W, H float64
}

const (
	header  = 18 // px reserved at the top of a folder tile for its name
	minSide = 3  // px; smaller items are merged into one "smaller items" tile
)

// maxTiles caps the tiles per folder at each depth, so the UI never draws thousands of elements.
var maxTiles = [...]int{60, 25, 12}

// Layout lays out the children of id in a w x h box. Folder tiles hold their own
// children one level down, and a second level down when they are big enough.
func Layout(t *tree.Tree, id tree.ID, w, h float64) []Tile {
	return layout(t, id, 0, 0, w, h, 0)
}

func layout(t *tree.Tree, id tree.ID, x, y, w, h float64, depth int) []Tile {
	total := float64(t.Alloc(id))
	if total == 0 || w < 1 || h < 1 {
		return nil
	}
	kids := t.Children(id)
	t.Sort(kids, "size", false)
	minBytes := total * minSide * minSide / (w * h)
	shown := kids
	var rest uint64
	for i, k := range kids {
		if i >= maxTiles[depth] || float64(t.Alloc(k)) < minBytes {
			shown = kids[:i]
			for _, r := range kids[i:] {
				rest += t.Alloc(r)
			}
			break
		}
	}
	values := make([]float64, 0, len(shown)+1)
	for _, k := range shown {
		values = append(values, float64(t.Alloc(k)))
	}
	if rest > 0 {
		values = append(values, float64(rest))
	}
	var out []Tile
	for _, r := range Squarify(values, x, y, w, h) {
		tile := Tile{ID: tree.None, Depth: depth, X: r.X, Y: r.Y, W: r.W, H: r.H}
		if r.Index == len(shown) {
			tile.Bytes, tile.Others = rest, len(kids)-len(shown)
			out = append(out, tile)
			continue
		}
		k := shown[r.Index]
		tile.ID, tile.Bytes = k, t.Alloc(k)
		out = append(out, tile)
		big := r.W > 48*float64(depth+1) && r.H > header+24*float64(depth+1)
		if t.IsDir(k) && depth+1 < len(maxTiles) && big {
			out = append(out, layout(t, k, r.X+2, r.Y+header, r.W-4, r.H-header-2, depth+1)...)
		}
	}
	return out
}
