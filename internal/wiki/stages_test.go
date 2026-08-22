package wiki

import "testing"

func TestParseStages(t *testing.T) {
	cases := []struct {
		in   string
		want Stages
	}{
		{"", AllStages()},
		{"all", AllStages()},
		{"pages", Stages{Pages: true}},
		{"files", Stages{Files: true}},
		{"forum", Stages{Forum: true}},
		{"Page", Stages{Pages: true}},
		{"FORUMS", Stages{Forum: true}},
		{"pages,files", Stages{Pages: true, Files: true}},
		{"forum pages", Stages{Pages: true, Forum: true}},
		{"pages, files, forum", AllStages()},
	}
	for _, c := range cases {
		got, err := ParseStages(c.in)
		if err != nil {
			t.Errorf("ParseStages(%q) returned %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseStages(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}

	for _, bad := range []string{"revisions", "pages,forums,users", "all,pages"} {
		if _, err := ParseStages(bad); err == nil {
			t.Errorf("ParseStages(%q) accepted an invalid value", bad)
		}
	}
}

func TestStagesNeedSitemap(t *testing.T) {
	if (Stages{Forum: true}).needSitemap() {
		t.Error("a forum-only run should not walk the sitemap")
	}
	for _, s := range []Stages{{Pages: true}, {Files: true}, AllStages()} {
		if !s.needSitemap() {
			t.Errorf("%v should walk the sitemap", s)
		}
	}
}
