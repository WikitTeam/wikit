package main

import "testing"

func TestResolveWikiURL(t *testing.T) {
	cases := []struct {
		name, url, defScheme, want string
	}{
		// url with an explicit scheme is honored verbatim (per-wiki control)
		{"a", "https://a.wikidot.com", "http", "https://a.wikidot.com"},
		{"b", "http://b.example.com", "https", "http://b.example.com"},
		// scheme-less url gets the default scheme
		{"c", "c.example.com", "http", "http://c.example.com"},
		{"d", "//d.example.com", "https", "https://d.example.com"},
		// empty url is derived from the name using the default scheme
		{"scp-wiki", "", "https", "https://scp-wiki.wikidot.com"},
		{"old", "", "http", "http://old.wikidot.com"},
	}
	for _, c := range cases {
		if got := resolveWikiURL(c.name, c.url, c.defScheme); got != c.want {
			t.Errorf("resolveWikiURL(%q, %q, %q) = %q, want %q", c.name, c.url, c.defScheme, got, c.want)
		}
	}
}

func TestParseBackupArgsOnly(t *testing.T) {
	opts, targets, err := parseBackupArgs([]string{"--only", "forum", "scp-wiki"})
	if err != nil {
		t.Fatalf("parseBackupArgs returned %v", err)
	}
	if len(targets) != 1 || targets[0] != "scp-wiki" {
		t.Errorf("targets = %v, want [scp-wiki]", targets)
	}
	if opts.only == nil || *opts.only != "forum" {
		t.Fatalf("only = %v, want \"forum\"", opts.only)
	}

	if opts, _, err := parseBackupArgs([]string{"scp-wiki"}); err != nil || opts.only != nil {
		t.Errorf("only = %v (err %v), want nil", opts.only, err)
	}

	if _, _, err := parseBackupArgs([]string{"--only", "revisions", "scp-wiki"}); err == nil {
		t.Error("parseBackupArgs accepted --only revisions")
	}
}
