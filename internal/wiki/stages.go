package wiki

import (
	"fmt"
	"sort"
	"strings"
)

type Stages struct {
	Pages bool
	Files bool
	Forum bool
}

func AllStages() Stages { return Stages{Pages: true, Files: true, Forum: true} }

func (s Stages) needSitemap() bool { return s.Pages || s.Files }

func (s Stages) all() bool { return s.Pages && s.Files && s.Forum }

func (s Stages) String() string {
	var parts []string
	if s.Pages {
		parts = append(parts, "pages")
	}
	if s.Files {
		parts = append(parts, "files")
	}
	if s.Forum {
		parts = append(parts, "forum")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

func ParseStages(s string) (Stages, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		return AllStages(), nil
	}
	var out Stages
	seen := map[string]bool{}
	for _, f := range fields {
		name := strings.ToLower(strings.TrimSpace(f))
		switch name {
		case "all":
			out = AllStages()
		case "pages", "page":
			out.Pages = true
		case "files", "file":
			out.Files = true
		case "forum", "forums":
			out.Forum = true
		default:
			return Stages{}, fmt.Errorf("unknown stage %q: expected pages, files, forum or all", f)
		}
		seen[name] = true
	}
	if len(seen) > 1 && seen["all"] {
		names := make([]string, 0, len(seen))
		for n := range seen {
			names = append(names, n)
		}
		sort.Strings(names)
		return Stages{}, fmt.Errorf("\"all\" cannot be combined with other stages (got %s)", strings.Join(names, ", "))
	}
	return out, nil
}
