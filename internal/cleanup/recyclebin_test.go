package cleanup

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWatchedReportsProgress(t *testing.T) {
	var left atomic.Int64
	left.Store(10)
	// The fake work removes one item per millisecond, so several readings land mid-way.
	work := func() error {
		for left.Load() > 0 {
			time.Sleep(time.Millisecond)
			left.Add(-1)
		}
		return nil
	}
	remaining := func() (uint64, error) { return uint64(left.Load()), nil }
	var got []Progress
	err := runWatched(work, remaining, 10, time.Millisecond, func(p Progress) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || got[0] != (Progress{Percent: 0, Detail: "0 of 10 items removed"}) {
		t.Fatalf("want a starting report and at least one update, got %+v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Percent < got[i-1].Percent || got[i].Percent > 100 {
			t.Errorf("progress went backwards or past 100%%: %+v", got)
		}
	}
}

func TestRunWatchedIgnoresCountsAboveTotal(t *testing.T) {
	release := make(chan struct{})
	work := func() error {
		<-release
		return errors.New("shell error")
	}
	// Something else adds files to the bin during the run.
	remaining := func() (uint64, error) { return 15, nil }
	var got []Progress
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(release)
	}()
	err := runWatched(work, remaining, 10, time.Millisecond, func(p Progress) { got = append(got, p) })
	if err == nil || err.Error() != "shell error" {
		t.Errorf("work's error should come back, got %v", err)
	}
	if len(got) != 1 {
		t.Errorf("readings above the total should be skipped, got %+v", got)
	}
}

func TestRecycleBinNeedsTheRealFunction(t *testing.T) {
	a, _ := Find("empty-recycle-bin")
	if _, err := a.Run(context.Background(), Env{}, Plan{}); err == nil {
		t.Error("an Env without EmptyRecycleBin must refuse to run")
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[uint64]string{0: "0", 999: "999", 1000: "1,000", 188042: "188,042", 1234567: "1,234,567"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q, want %q", n, got, want)
		}
	}
}
