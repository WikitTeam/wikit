package sevenzip

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestAddPageRevisions verifies the page-revision invocation yields bare
// <rev>.txt members (matching the reference archives' layout).
func TestAddPageRevisions(t *testing.T) {
	if _, err := Bin(); err != nil {
		t.Skipf("no 7z available: %v", err)
	}
	base := t.TempDir()
	dir := filepath.Join(base, "pages", "somepage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"1.txt", "2.txt", "43.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("content "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(base, "pages", "somepage.7z")
	if err := Add(archive, filepath.Join(dir, "*.txt"), false); err != nil {
		t.Fatal(err)
	}
	got, err := List(archive)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"1.txt", "2.txt", "43.txt"}
	if !equal(got, want) {
		t.Fatalf("page members = %v, want %v", got, want)
	}
}

// TestAddForumThread verifies the forum invocation yields <post>/<rev>.html
// members.
func TestAddForumThread(t *testing.T) {
	if _, err := Bin(); err != nil {
		t.Skipf("no 7z available: %v", err)
	}
	base := t.TempDir()
	thread := filepath.Join(base, "forum", "123", "456")
	for _, p := range []string{"100/latest.html", "100/9189259.html", "200/latest.html"} {
		full := filepath.Join(thread, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(base, "forum", "123", "456.7z")
	if err := Add(archive, filepath.Join(thread, "*.*"), true); err != nil {
		t.Fatal(err)
	}
	got, err := List(archive)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"100/9189259.html", "100/latest.html", "200/latest.html"}
	if !equal(got, want) {
		t.Fatalf("forum members = %v, want %v", got, want)
	}
}

// TestAddRelativeWorkDir covers the shape a relative --work-dir produces: 7z
// keeps the whole prefix of a relative spec, which used to bake
// "wikit_data/<wiki>/pages/<name>/" into every member and break the incremental
// scan that parses member names back into revision numbers.
func TestAddRelativeWorkDir(t *testing.T) {
	if _, err := Bin(); err != nil {
		t.Skipf("no 7z available: %v", err)
	}
	base := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	// Exactly what wiki.WikiDot builds with a relative workDir.
	dir := filepath.Join("wikit_data", "some-wiki", "pages", "somepage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"0.txt", "1.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("content "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join("wikit_data", "some-wiki", "pages", "somepage.7z")
	if err := Add(archive, filepath.Join(dir, "*.txt"), false); err != nil {
		t.Fatal(err)
	}
	got, err := List(archive)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"0.txt", "1.txt"}
	if !equal(got, want) {
		t.Fatalf("members = %v, want %v (relative workDir must not leak into the archive)", got, want)
	}
}

func equal(a, b []string) bool {
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
