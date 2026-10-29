package kb

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Safety ratings, from least to most cautious.
type Safety string

const (
	Safe         Safety = "safe"           // delete freely
	Rebuilds     Safety = "rebuilds"       // delete freely; the owner recreates it as needed
	DeleteViaApp Safety = "delete-via-app" // only remove through the owning app or a cleanup action
	Never        Safety = "never"          // do not touch
)

// Rule is one knowledge-base entry. See rules/*.yaml.
type Rule struct {
	ID          string `yaml:"id" json:"id"`
	Match       string `yaml:"match" json:"match"`
	Name        string `yaml:"name" json:"name"`
	Owner       string `yaml:"owner" json:"owner"`
	Description string `yaml:"description" json:"description"`
	Safety      Safety `yaml:"safety" json:"safety"`
	Action      string `yaml:"action" json:"action"`
	LearnMore   string `yaml:"learn_more" json:"learnMore"`
	Category    string `yaml:"-" json:"category"` // the rule file's name, e.g. "browsers"
	Tests       struct {
		Match   []string `yaml:"match"`
		NoMatch []string `yaml:"no_match"`
	} `yaml:"tests" json:"-"`

	segs        []string // expanded, lower-case pattern segments
	specificity int
}

// KB holds every loaded rule, indexed for fast matching.
type KB struct {
	rules  []*Rule
	byName map[string][]*Rule // rules whose last segment is a literal name
	wild   []*Rule            // rules whose last segment has a wildcard
}

var envVar = regexp.MustCompile(`%([^%]+)%`)

// Load reads every *.yaml file in each fsys. A rule in a later fsys replaces one with
// the same id from an earlier one, so updated rule files can override the embedded set.
// getenv expands %VARS%; a rule using a variable that is not set is skipped.
func Load(getenv func(string) string, fsyss ...fs.FS) (*KB, error) {
	byID := map[string]*Rule{}
	var order []string
	for _, fsys := range fsyss {
		files, err := fs.Glob(fsys, "*.yaml")
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			data, err := fs.ReadFile(fsys, f)
			if err != nil {
				return nil, err
			}
			var rules []*Rule
			if err := yaml.Unmarshal(data, &rules); err != nil {
				return nil, fmt.Errorf("parsing %s: %w", f, err)
			}
			for _, r := range rules {
				r.Category = strings.TrimSuffix(f, ".yaml")
				if err := r.validate(); err != nil {
					return nil, fmt.Errorf("%s: %w", f, err)
				}
				if _, ok := byID[r.ID]; !ok {
					order = append(order, r.ID)
				}
				byID[r.ID] = r
			}
		}
	}
	k := &KB{byName: map[string][]*Rule{}}
	for _, id := range order {
		r := byID[id]
		if !r.compile(getenv) {
			continue
		}
		k.rules = append(k.rules, r)
		last := r.segs[len(r.segs)-1]
		if strings.ContainsAny(last, "*?[") {
			k.wild = append(k.wild, r)
		} else {
			k.byName[last] = append(k.byName[last], r)
		}
	}
	return k, nil
}

func (r *Rule) validate() error {
	if r.ID == "" || r.Match == "" || r.Name == "" || r.Description == "" {
		return fmt.Errorf("rule %q: id, match, name and description are required", r.ID)
	}
	switch r.Safety {
	case Safe, Rebuilds, DeleteViaApp, Never:
	case "":
		r.Safety = Never // unsure means never
	default:
		return fmt.Errorf("rule %s: unknown safety %q", r.ID, r.Safety)
	}
	return nil
}

// compile expands env vars and splits the pattern. It returns false if a variable is unset.
func (r *Rule) compile(getenv func(string) string) bool {
	expanded, ok := expandVars(r.Match, getenv)
	if !ok {
		return false
	}
	r.segs = splitLower(expanded)
	r.specificity = 0
	for _, s := range r.segs {
		if !strings.ContainsAny(s, "*?[") {
			r.specificity += 1000
		}
		r.specificity += len(strings.NewReplacer("*", "", "?", "").Replace(s))
	}
	return true
}

// expandVars replaces %VAR% references. ok is false if any variable is unset.
func expandVars(s string, getenv func(string) string) (out string, ok bool) {
	ok = true
	out = envVar.ReplaceAllStringFunc(s, func(m string) string {
		v := getenv(m[1 : len(m)-1])
		if v == "" {
			ok = false
		}
		return v
	})
	return out, ok
}

func splitLower(p string) []string {
	p = strings.ToLower(strings.TrimRight(filepath.Clean(p), `\`))
	return strings.Split(p, `\`)
}

// Matches reports whether the rule's pattern matches the full path.
func (r *Rule) Matches(p string) bool { return matchSegs(r.segs, splitLower(p)) }

func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if matchSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// Rules returns every loaded rule.
func (k *KB) Rules() []*Rule { return k.rules }

// Match returns the most specific rule matching exactly this path, or nil.
func (k *KB) Match(p string) *Rule {
	segs := splitLower(p)
	var best *Rule
	try := func(r *Rule) {
		if (best == nil || r.specificity > best.specificity) && matchSegs(r.segs, segs) {
			best = r
		}
	}
	for _, r := range k.byName[segs[len(segs)-1]] {
		try(r)
	}
	for _, r := range k.wild {
		try(r)
	}
	return best
}

// MayMatchName is a cheap pre-filter for tree walks: a node whose name returns
// false here cannot match any rule, so its full path need not be built.
func (k *KB) MayMatchName(name string) bool {
	name = strings.ToLower(name)
	if len(k.byName[name]) > 0 {
		return true
	}
	for _, r := range k.wild {
		if ok, _ := path.Match(r.segs[len(r.segs)-1], name); ok {
			return true
		}
	}
	return false
}

// Explain finds the rule for p or, failing that, its nearest ancestor (a file deep
// inside node_modules is explained by the node_modules rule). at is the matched path.
func (k *KB) Explain(p string) (r *Rule, at string) {
	for cur := filepath.Clean(p); ; {
		if r := k.Match(cur); r != nil {
			return r, cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil, ""
		}
		cur = parent
	}
}

// Forbids reports whether the knowledge base forbids quarantining p, and why.
// A rule on p itself that says never or delete-via-app forbids it. Otherwise the
// nearest parent folder with a rule decides: a never rule there forbids p (a file
// inside a browser profile, for example), and a more specific rule in between wins.
func (k *KB) Forbids(p string) (bool, string) {
	if r := k.Match(p); r != nil {
		switch r.Safety {
		case Never:
			return true, r.Name + " must never be deleted"
		case DeleteViaApp:
			owner := r.Owner
			if owner == "" {
				owner = "its app"
			}
			return true, r.Name + " should be removed through " + owner + ", not deleted"
		}
	}
	if r, at := k.Explain(p); r != nil && r.Safety == Never {
		return true, "it is inside " + at + " (" + r.Name + "), which must never be touched"
	}
	return false, ""
}
