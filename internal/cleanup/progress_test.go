package cleanup

import (
	"bufio"
	"context"
	"slices"
	"strings"
	"testing"
)

func TestStepLines(t *testing.T) {
	tests := []struct {
		name        string
		step, steps int
		line        string
		want        float64
	}{
		{"DISM bar", 0, 1, "[==========                 25.0%                          ]", 25},
		{"diskpart", 1, 2, "  50 percent completed", 75},
		{"plain line, one step", 0, 1, "Image Version: 10.0.26100.1", -1},
		{"plain line, later step", 2, 4, "Deleted Images:", 50},
		{"not a percentage", 0, 1, "Reclaimed 250%? no", -1},
	}
	for _, tc := range tests {
		var got Progress
		Env{Progress: func(p Progress) { got = p }}.stepLines(tc.step, tc.steps)(tc.line)
		if got.Percent != tc.want || got.Detail != tc.line {
			t.Errorf("%s: got %+v, want percent %v", tc.name, got, tc.want)
		}
	}
}

func TestScanLinesSplitsCarriageReturns(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("Starting\r\n[= 10.0% =]\r[== 20.0% ==]\rDone\n"))
	sc.Split(scanLines)
	var lines []string
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	want := []string{"Starting", "[= 10.0% =]", "[== 20.0% ==]", "Done"}
	if !slices.Equal(lines, want) {
		t.Errorf("got %q, want %q", lines, want)
	}
}

// Runs cmd's built-in echo, which changes nothing on the machine.
func TestExecStreamsLines(t *testing.T) {
	var lines []string
	out, err := Exec(context.Background(), func(l string) { lines = append(lines, l) }, "cmd", "/c", "echo one& echo two")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lines, []string{"one", "two"}) || !strings.Contains(out, "two") {
		t.Errorf("lines %q, output %q", lines, out)
	}
}

func TestRunAllReportsEachStep(t *testing.T) {
	f := &fakeRunner{}
	var got []Progress
	env := Env{Run: f.run, Progress: func(p Progress) { got = append(got, p) }}
	if _, err := runAll(context.Background(), env, [][]string{{"first"}, {"second"}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Percent != 0 || got[1].Percent != 50 || got[1].Detail != "Running second" {
		t.Errorf("got %+v", got)
	}
}
