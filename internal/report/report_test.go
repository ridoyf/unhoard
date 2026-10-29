package report

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/ridoyf/unhoard/internal/tree"
)

func TestReports(t *testing.T) {
	tr := tree.New(`C:\`)
	d := tr.Add(0, "<script>", tree.Dir, 0, 0, 0)
	tr.Add(d, "a,b.txt", 0, 2048, 4096, 1700000000)
	tr.Finalize()
	rows := Rows(tr)
	if len(rows) != 3 || rows[0].Section != "Top-level item" || rows[1].Path != `C:\<script>\a,b.txt` || rows[2].Path != ".txt" {
		t.Fatalf("rows = %+v", rows)
	}
	var buf bytes.Buffer
	if err := CSV(&buf, rows); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(recs) != 4 || recs[2][1] != `C:\<script>\a,b.txt` || recs[2][2] != "4096" {
		t.Fatalf("csv = %v, %v", recs, err)
	}
	buf.Reset()
	if err := HTML(&buf, `C:\`, 4096, time.Now(), rows); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "<script>\\") || !strings.Contains(buf.String(), "&lt;script&gt;") {
		t.Errorf("HTML report did not escape paths")
	}
}

func TestHuman(t *testing.T) {
	for in, want := range map[uint64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB", 5 << 30: "5.0 GB"} {
		if got := Human(in); got != want {
			t.Errorf("Human(%d) = %q, want %q", in, got, want)
		}
	}
}
