package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWritesEveryRule(t *testing.T) {
	out := t.TempDir()
	if err := build(out, "https://example.test/"); err != nil {
		t.Fatal(err)
	}
	pages, err := load()
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		b, err := os.ReadFile(filepath.Join(out, p.ID, "index.html"))
		if err != nil {
			t.Fatalf("rule %s: %v", p.ID, err)
		}
		if !strings.Contains(string(b), p.Safety) || !strings.Contains(string(index), `href="./`+p.ID+`/"`) {
			t.Errorf("rule %s: page or index link is missing", p.ID)
		}
	}
}
