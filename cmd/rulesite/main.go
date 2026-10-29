// Command rulesite writes a static website with one page per knowledge-base rule,
// so people searching "can I delete X" find the answer Unhoard already has.
//
//	go run ./cmd/rulesite -out site
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ridoyf/unhoard/internal/cleanup"
	"github.com/ridoyf/unhoard/internal/kb"
	"github.com/ridoyf/unhoard/rules"
)

//go:embed site.html
var siteHTML string

var categories = map[string]string{
	"windows":  "Windows",
	"browsers": "Browsers",
	"apps":     "Apps",
	"cloud":    "Cloud storage",
	"dev":      "Developer tools",
	"games":    "Games",
}

var safetyText = map[kb.Safety]string{
	kb.Safe:         "Safe to delete",
	kb.Rebuilds:     "Rebuilds itself",
	kb.DeleteViaApp: "Delete via the app",
	kb.Never:        "Never touch",
}

type page struct {
	*kb.Rule
	Safety      string
	Category    string
	ActionTitle string
	Shared      bool // another rule has the same name, so the path tells them apart
	Title       string
}

type group struct {
	Name  string
	Pages []page
}

func main() {
	out := flag.String("out", "site", "folder to write the site to")
	base := flag.String("base", "https://ridoyf.github.io/unhoard/", "URL the site is served from, for the sitemap")
	flag.Parse()
	if err := build(*out, *base); err != nil {
		log.Fatal(err)
	}
}

func build(out, base string) error {
	pages, err := load()
	if err != nil {
		return err
	}
	t, err := template.New("site").Parse(siteHTML)
	if err != nil {
		return err
	}
	if err := write(filepath.Join(out, "index.html"), func(w io.Writer) error {
		return t.ExecuteTemplate(w, "index", groups(pages))
	}); err != nil {
		return err
	}
	for _, p := range pages {
		if err := write(filepath.Join(out, p.ID, "index.html"), func(w io.Writer) error {
			return t.ExecuteTemplate(w, "rule", p)
		}); err != nil {
			return err
		}
	}
	return write(filepath.Join(out, "sitemap.xml"), func(w io.Writer) error {
		fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`)
		fmt.Fprintln(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		fmt.Fprintf(w, "<url><loc>%s</loc></url>\n", base)
		for _, p := range pages {
			fmt.Fprintf(w, "<url><loc>%s%s/</loc></url>\n", base, p.ID)
		}
		_, err := fmt.Fprintln(w, `</urlset>`)
		return err
	})
}

// load reads the embedded rules. Every variable gets a stand-in value so no rule is
// skipped for a variable this machine does not set; the pages show the raw pattern.
func load() ([]page, error) {
	k, err := kb.Load(func(v string) string { return `C:\` + v }, rules.FS)
	if err != nil {
		return nil, err
	}
	var pages []page
	for _, r := range k.Rules() {
		p := page{Rule: r, Safety: safetyText[r.Safety], Category: categories[r.Category]}
		if p.Category == "" {
			return nil, fmt.Errorf("rule %s: no display name for category %q", r.ID, r.Category)
		}
		if a, ok := cleanup.Find(r.Action); ok {
			p.ActionTitle = a.Title
		}
		pages = append(pages, p)
	}
	names := map[string]int{}
	for _, p := range pages {
		names[p.Name]++
	}
	for i := range pages {
		p := &pages[i]
		p.Shared = names[p.Name] > 1
		p.Title = p.Name
		if p.Shared {
			p.Title += " (" + p.Match + ")"
		}
		p.Title += ": what it is and whether you can delete it"
	}
	slices.SortFunc(pages, func(a, b page) int {
		if c := strings.Compare(a.Category, b.Category); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return pages, nil
}

func groups(pages []page) []group {
	var out []group
	for _, p := range pages {
		if len(out) == 0 || out[len(out)-1].Name != p.Category {
			out = append(out, group{Name: p.Category})
		}
		g := &out[len(out)-1]
		g.Pages = append(g.Pages, p)
	}
	return out
}

func write(path string, render func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := render(f); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return f.Close()
}
