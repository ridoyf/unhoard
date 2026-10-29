package treemap

import (
	"math"
	"strconv"
	"testing"

	"github.com/ridoyf/unhoard/internal/tree"
)

func TestSquarify(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		w, h   float64
	}{
		{"classic example", []float64{6, 6, 4, 3, 2, 2, 1}, 6, 4},
		{"tall", []float64{50, 30, 20}, 100, 400},
		{"one", []float64{7}, 10, 10},
		{"with zeros", []float64{0, 5, 0, 5}, 10, 10},
		{"skewed", []float64{1000, 1, 1, 1, 1}, 300, 200},
	}
	for _, tc := range tests {
		rects := Squarify(tc.values, 0, 0, tc.w, tc.h)
		var total, area float64
		nonzero := 0
		for _, v := range tc.values {
			total += v
			if v > 0 {
				nonzero++
			}
		}
		if len(rects) != nonzero {
			t.Errorf("%s: %d rects for %d non-zero values", tc.name, len(rects), nonzero)
		}
		for _, r := range rects {
			want := tc.values[r.Index] / total * tc.w * tc.h
			if got := r.W * r.H; math.Abs(got-want) > 1e-6 {
				t.Errorf("%s: tile %d area %.3f, want %.3f", tc.name, r.Index, got, want)
			}
			if r.X < -1e-9 || r.Y < -1e-9 || r.X+r.W > tc.w+1e-6 || r.Y+r.H > tc.h+1e-6 {
				t.Errorf("%s: tile %d %+v outside the box", tc.name, r.Index, r)
			}
			area += r.W * r.H
		}
		if math.Abs(area-tc.w*tc.h) > 1e-6 {
			t.Errorf("%s: tiles cover %.3f of %.3f", tc.name, area, tc.w*tc.h)
		}
		// No two tiles overlap.
		for i := range rects {
			for j := i + 1; j < len(rects); j++ {
				a, b := rects[i], rects[j]
				ox := math.Min(a.X+a.W, b.X+b.W) - math.Max(a.X, b.X)
				oy := math.Min(a.Y+a.H, b.Y+b.H) - math.Max(a.Y, b.Y)
				if ox > 1e-6 && oy > 1e-6 {
					t.Errorf("%s: tiles %d and %d overlap", tc.name, a.Index, b.Index)
				}
			}
		}
	}
	if Squarify(nil, 0, 0, 10, 10) != nil || Squarify([]float64{1}, 0, 0, 0, 10) != nil {
		t.Errorf("empty input or empty box should give no tiles")
	}
}

func TestSquarifyKeepsTilesSquareish(t *testing.T) {
	values := make([]float64, 20)
	for i := range values {
		values[i] = 1
	}
	for _, r := range Squarify(values, 0, 0, 500, 400) {
		if ratio := max(r.W/r.H, r.H/r.W); ratio > 3 {
			t.Errorf("equal values produced a %.1f:1 tile", ratio)
		}
	}
}

func TestLayout(t *testing.T) {
	tr := tree.New(`C:\`)
	dir := tr.Root()
	// A chain of big folders, each holding one more folder and a file.
	for range 5 {
		next := tr.Add(dir, "d", tree.Dir, 0, 0, 0)
		tr.Add(dir, "f", 0, 100, 100, 0)
		dir = next
	}
	tr.Add(dir, "leaf", 0, 1000, 1000, 0)
	for i := range 200 {
		tr.Add(tr.Root(), "tiny"+strconv.Itoa(i), 0, 1, 1, 0)
	}
	tr.Finalize()

	tiles := Layout(tr, tr.Root(), 1000, 800)
	others, deepest := 0, 0
	for _, tile := range tiles {
		deepest = max(deepest, tile.Depth)
		if tile.ID == tree.None && tile.Depth == 0 {
			others += tile.Others
		}
	}
	if deepest != len(maxTiles)-1 {
		t.Errorf("tiles nest %d levels deep, want %d", deepest, len(maxTiles)-1)
	}
	if want := 202 - maxTiles[0]; others != want {
		t.Errorf("%d root items merged into the smaller-items tile, want %d", others, want)
	}
	if Layout(tr, tr.Root(), 0, 800) != nil {
		t.Error("an empty box should give no tiles")
	}
}
