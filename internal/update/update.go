package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const repo = "ridoyf/unhoard"

// Info describes the newest release.
type Info struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
}

// Check asks GitHub for the latest release. It only reads; nothing is downloaded or installed.
func Check(ctx context.Context, current string) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Info{}, err
	}
	return Info{Current: current, Latest: rel.TagName, URL: rel.HTMLURL, Available: Newer(rel.TagName, current)}, nil
}

// Newer reports whether version a is newer than b. Both look like "v1.2.3"; pre-release suffixes are ignored.
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	var out [3]int
	v, _, _ = strings.Cut(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-")
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
