package fixarchive

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"wikit/internal/sevenzip"
)

// legacyArchive builds an archive the way older wikit builds did: 7z invoked
// from the directory the command was launched in, with a relative spec, which
// makes 7z store the whole relative prefix in every member name.
func legacyArchive(t *testing.T, root, archiveRel, specRel string, recursive bool) {
	t.Helper()
	bin, err := sevenzip.Bin()
	if err != nil {
		t.Skipf("no 7z available: %v", err)
	}
	args := []string{"a", archiveRel, specRel, "-y", "-bso0", "-bsp0"}
	if recursive {
		args = append(args, "-r")
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build legacy archive: %v: %s", err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func members(t *testing.T, archive string) []string {
	t.Helper()
	got, err := sevenzip.List(archive)
	if err != nil {
		t.Fatalf("list %s: %v", archive, err)
	}
	sort.Strings(got)
	return got
}

func contentOf(t *testing.T, archive, member string) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "x")
	if err := sevenzip.Extract(archive, dest); err != nil {
		t.Fatalf("extract %s: %v", archive, err)
	}
	b, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(member)))
	if err != nil {
		t.Fatalf("read %s from %s: %v", member, archive, err)
	}
	return string(b)
}

// setup lays out base/wikit_data/<wiki> with one prefixed page archive and one
// prefixed forum archive, and returns the root and the wiki directory.
func setup(t *testing.T) (root, wikiDir string) {
	t.Helper()
	root = t.TempDir()
	rel := filepath.Join("wikit_data", "some-wiki")
	wikiDir = filepath.Join(root, rel)

	pageDir := filepath.Join(wikiDir, "pages", "somepage")
	write(t, filepath.Join(pageDir, "0.txt"), "revision zero")
	write(t, filepath.Join(pageDir, "17.txt"), "revision seventeen")
	legacyArchive(t, root,
		filepath.Join(rel, "pages", "somepage.7z"),
		filepath.Join(rel, "pages", "somepage", "*.txt"), false)
	os.RemoveAll(pageDir)

	threadDir := filepath.Join(wikiDir, "forum", "42", "1234")
	write(t, filepath.Join(threadDir, "100", "latest.html"), "post 100 latest")
	write(t, filepath.Join(threadDir, "100", "9189259.html"), "post 100 old")
	write(t, filepath.Join(threadDir, "200", "latest.html"), "post 200 latest")
	legacyArchive(t, root,
		filepath.Join(rel, "forum", "42", "1234.7z"),
		filepath.Join(rel, "forum", "42", "1234", "*.*"), true)
	os.RemoveAll(threadDir)

	return root, wikiDir
}

func TestWikiRepairsPrefixedArchives(t *testing.T) {
	if _, err := sevenzip.Bin(); err != nil {
		t.Skipf("no 7z available: %v", err)
	}
	_, wikiDir := setup(t)

	pageArchive := filepath.Join(wikiDir, "pages", "somepage.7z")
	forumArchive := filepath.Join(wikiDir, "forum", "42", "1234.7z")

	// Precondition: the legacy layout really is broken.
	if got := members(t, pageArchive); !strings.Contains(got[0], "/") {
		t.Fatalf("expected a prefixed legacy archive, got %v", got)
	}

	res, err := Wiki(wikiDir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scanned != 2 || res.Fixed != 2 || res.OK != 0 || res.Failed != 0 {
		t.Fatalf("result = %+v, want 2 scanned / 2 fixed", res)
	}

	if got, want := members(t, pageArchive), []string{"0.txt", "17.txt"}; !equalStrings(got, want) {
		t.Errorf("page members = %v, want %v", got, want)
	}
	want := []string{"100/9189259.html", "100/latest.html", "200/latest.html"}
	if got := members(t, forumArchive); !equalStrings(got, want) {
		t.Errorf("forum members = %v, want %v", got, want)
	}

	// Contents must survive the rebuild untouched.
	if got := contentOf(t, pageArchive, "17.txt"); got != "revision seventeen" {
		t.Errorf("17.txt = %q", got)
	}
	if got := contentOf(t, forumArchive, "100/9189259.html"); got != "post 100 old" {
		t.Errorf("100/9189259.html = %q", got)
	}
}

func TestWikiLeavesCorrectArchivesAlone(t *testing.T) {
	if _, err := sevenzip.Bin(); err != nil {
		t.Skipf("no 7z available: %v", err)
	}
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wikit_data", "some-wiki")
	dir := filepath.Join(wikiDir, "pages", "somepage")
	write(t, filepath.Join(dir, "0.txt"), "revision zero")
	archive := filepath.Join(wikiDir, "pages", "somepage.7z")
	if err := sevenzip.Add(archive, filepath.Join(dir, "*.txt"), false); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dir)

	before, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Wiki(wikiDir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK != 1 || res.Fixed != 0 {
		t.Fatalf("result = %+v, want 1 already-correct archive", res)
	}
	after, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("a correct archive was rewritten")
	}
}

func TestWikiDryRunDoesNotWrite(t *testing.T) {
	if _, err := sevenzip.Bin(); err != nil {
		t.Skipf("no 7z available: %v", err)
	}
	_, wikiDir := setup(t)
	pageArchive := filepath.Join(wikiDir, "pages", "somepage.7z")
	before := members(t, pageArchive)

	res, err := Wiki(wikiDir, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Fixed != 2 {
		t.Fatalf("result = %+v, want 2 archives reported", res)
	}
	if got := members(t, pageArchive); !equalStrings(got, before) {
		t.Errorf("dry run modified the archive: %v -> %v", before, got)
	}
}

// TestWikiMixedArchive covers an archive appended to both before and after the
// naming fix: the same revision exists twice, prefixed and bare. The rebuild
// must keep exactly one copy, and it must be the bare (current) one.
func TestWikiMixedArchive(t *testing.T) {
	if _, err := sevenzip.Bin(); err != nil {
		t.Skipf("no 7z available: %v", err)
	}
	root := t.TempDir()
	rel := filepath.Join("wikit_data", "some-wiki")
	wikiDir := filepath.Join(root, rel)
	dir := filepath.Join(wikiDir, "pages", "somepage")

	write(t, filepath.Join(dir, "0.txt"), "stale prefixed copy")
	legacyArchive(t, root,
		filepath.Join(rel, "pages", "somepage.7z"),
		filepath.Join(rel, "pages", "somepage", "*.txt"), false)

	// Now append the way the fixed code does: bare names.
	archive := filepath.Join(wikiDir, "pages", "somepage.7z")
	write(t, filepath.Join(dir, "0.txt"), "current copy")
	write(t, filepath.Join(dir, "1.txt"), "revision one")
	if err := sevenzip.Add(archive, filepath.Join(dir, "*.txt"), false); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dir)

	if _, err := Wiki(wikiDir, Options{}); err != nil {
		t.Fatal(err)
	}
	if got, want := members(t, archive), []string{"0.txt", "1.txt"}; !equalStrings(got, want) {
		t.Fatalf("members = %v, want %v", got, want)
	}
	if got := contentOf(t, archive, "0.txt"); got != "current copy" {
		t.Errorf("0.txt = %q, want the bare (current) copy to win", got)
	}
}

func TestWikiMissingDirectory(t *testing.T) {
	if _, err := Wiki(filepath.Join(t.TempDir(), "nope"), Options{}); err == nil {
		t.Fatal("expected an error for a missing wiki directory")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
