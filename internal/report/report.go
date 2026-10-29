package report

import (
	"encoding/csv"
	"fmt"
	"html/template"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/ridoyf/unhoard/internal/finders"
	"github.com/ridoyf/unhoard/internal/tree"
)

// Row is one line of a report.
type Row struct {
	Section  string
	Path     string
	Bytes    uint64
	Logical  uint64
	Files    uint32
	Modified time.Time
}

// Rows collects the report's content: the root's folders, the 100 largest files and the file types.
func Rows(t *tree.Tree) []Row {
	var rows []Row
	row := func(section string, id tree.ID) Row {
		return Row{section, t.Path(id), t.Alloc(id), t.Size(id), t.Files(id), time.Unix(t.ModTime(id), 0)}
	}
	children := t.Children(0)
	t.Sort(children, "size", false)
	for _, c := range children {
		rows = append(rows, row("Top-level item", c))
	}
	for _, id := range finders.Largest(t, 0, 100, false) {
		rows = append(rows, row("Largest file", id))
	}
	for _, s := range finders.TypeBreakdown(t, 0, 50) {
		name := "." + s.Ext
		if s.Ext == "" {
			name = "(no extension)"
		} else if s.Ext == "(other)" {
			name = s.Ext
		}
		rows = append(rows, Row{Section: "File type", Path: name, Bytes: s.Bytes, Files: uint32(s.Files)})
	}
	return rows
}

// CSV writes rows as CSV with a header.
func CSV(w io.Writer, rows []Row) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"Section", "Path", "Size on disk (bytes)", "Size (bytes)", "Files", "Modified"})
	for _, r := range rows {
		mod := ""
		if r.Modified.Unix() > 0 {
			mod = r.Modified.Format(time.RFC3339)
		}
		cw.Write([]string{r.Section, r.Path, strconv.FormatUint(r.Bytes, 10), strconv.FormatUint(r.Logical, 10),
			strconv.FormatUint(uint64(r.Files), 10), mod})
	}
	cw.Flush()
	return cw.Error()
}

var page = template.Must(template.New("report").Funcs(template.FuncMap{"size": Human}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Unhoard report: {{.Root}}</title>
<style>
body{font-family:"Segoe UI",sans-serif;margin:2rem;color:#1b1b1f;background:#fff}
@media (prefers-color-scheme:dark){body{color:#e6e6ea;background:#18181b}th{background:#26262b!important}td{border-color:#333!important}}
table{border-collapse:collapse;width:100%;margin-bottom:2rem}th,td{text-align:left;padding:.35rem .6rem;border-bottom:1px solid #ddd}
th{background:#f3f3f5}td.n{text-align:right;font-variant-numeric:tabular-nums}
</style></head><body>
<h1>Disk usage: {{.Root}}</h1><p>Scanned {{.At}}. Total {{size .Total}} on disk.</p>
{{range .Sections}}<h2>{{.Name}}</h2><table><thead><tr><th>Item</th><th>Size on disk</th><th>Files</th></tr></thead><tbody>
{{range .Rows}}<tr><td>{{.Path}}</td><td class="n">{{size .Bytes}}</td><td class="n">{{.Files}}</td></tr>{{end}}
</tbody></table>{{end}}
</body></html>`))

// HTML writes a standalone HTML page. html/template escapes every path.
func HTML(w io.Writer, root string, total uint64, at time.Time, rows []Row) error {
	type section struct {
		Name string
		Rows []Row
	}
	var sections []section
	for _, r := range rows {
		if len(sections) == 0 || sections[len(sections)-1].Name != r.Section {
			sections = append(sections, section{Name: r.Section})
		}
		s := &sections[len(sections)-1]
		s.Rows = append(s.Rows, r)
	}
	return page.Execute(w, map[string]any{"Root": root, "Total": total, "At": at.Format("2 January 2006 15:04"), "Sections": sections})
}

// Human formats bytes with binary units, matching the app's display.
func Human(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Write saves the report for t as "csv" or "html".
func Write(path, format, root string, at time.Time, t *tree.Tree) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	rows := Rows(t)
	if format == "csv" {
		err = CSV(f, rows)
	} else {
		err = HTML(f, root, t.Alloc(0), at, rows)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
