package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wikit/internal/jsonx"
)

func testWiki(t *testing.T) *WikiDot {
	t.Helper()
	dir := t.TempDir()
	return &WikiDot{
		name:       "test",
		workDir:    dir,
		state:      newState(filepath.Join(dir, "meta")),
		checkpoint: DefaultCheckpointPolicy(),
	}
}

func stamp(v int64) *int64 { return &v }

func entriesOf(pairs ...any) []sitemapEntry {
	var out []sitemapEntry
	for i := 0; i < len(pairs); i += 2 {
		name := pairs[i].(string)
		var s *int64
		if pairs[i+1] != nil {
			s = stamp(int64(pairs[i+1].(int)))
		}
		out = append(out, sitemapEntry{Name: name, Update: s})
	}
	return out
}

func readSitemapKeys(t *testing.T, w *WikiDot) ([]string, map[string]*int64) {
	t.Helper()
	data, err := os.ReadFile(w.sitemapPath())
	if err != nil {
		t.Fatalf("read sitemap: %v", err)
	}
	v, err := jsonx.Decode(data)
	if err != nil {
		t.Fatalf("decode sitemap: %v", err)
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		t.Fatalf("sitemap is not an object")
	}
	vals := map[string]*int64{}
	for _, k := range o.Keys() {
		raw, _ := o.Get(k)
		if raw == nil {
			vals[k] = nil
			continue
		}
		n := asInt64(raw)
		vals[k] = &n
	}
	return o.Keys(), vals
}

func eqKeys(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestSitemapProgressSeedsFromOldMap(t *testing.T) {
	w := testWiki(t)
	entries := entriesOf("a", 1, "b", 2, "c", 3)
	old := map[string]*int64{"a": stamp(1), "b": stamp(1)}

	p := newSitemapProgress(w, entries, old)
	p.markDone("b", stamp(2)) // updated page finished this run
	p.checkpoint(true)

	keys, vals := readSitemapKeys(t, w)
	if !eqKeys(keys, []string{"a", "b"}) {
		t.Fatalf("keys = %v, want [a b] (c was never reached)", keys)
	}
	if *vals["a"] != 1 {
		t.Errorf("a = %d, want carried-over 1", *vals["a"])
	}
	if *vals["b"] != 2 {
		t.Errorf("b = %d, want updated 2", *vals["b"])
	}
}

func TestSitemapProgressDropsRemovedPages(t *testing.T) {
	w := testWiki(t)
	entries := entriesOf("a", 2)
	old := map[string]*int64{"a": stamp(1), "gone": stamp(9)}

	p := newSitemapProgress(w, entries, old)
	p.markDone("a", stamp(2))
	p.checkpoint(true)

	keys, _ := readSitemapKeys(t, w)
	if !eqKeys(keys, []string{"a"}) {
		t.Fatalf("keys = %v, want [a]", keys)
	}
}

func TestSitemapProgressKeepsDocumentOrder(t *testing.T) {
	w := testWiki(t)
	entries := entriesOf("z", 1, "m", nil, "a", 3)

	p := newSitemapProgress(w, entries, nil)
	p.markDone("a", stamp(3))
	p.markDone("z", stamp(1))
	p.markDone("m", nil)
	p.checkpoint(true)

	keys, vals := readSitemapKeys(t, w)
	if !eqKeys(keys, []string{"z", "m", "a"}) {
		t.Fatalf("keys = %v, want [z m a]", keys)
	}
	if vals["m"] != nil {
		t.Errorf("m = %v, want null", *vals["m"])
	}
}

func TestSitemapProgressNoWriteWhenClean(t *testing.T) {
	w := testWiki(t)
	entries := entriesOf("a", 1)
	p := newSitemapProgress(w, entries, map[string]*int64{"a": stamp(1)})

	p.markDone("a", stamp(1)) // same stamp: not dirty
	p.checkpoint(true)

	if _, err := os.Stat(w.sitemapPath()); !os.IsNotExist(err) {
		t.Fatalf("sitemap was written for a no-op run (err = %v)", err)
	}
}

func TestSitemapProgressThrottles(t *testing.T) {
	w := testWiki(t)
	policy := w.checkpoint
	var entries []sitemapEntry
	for i := 0; i < policy.Pages+1; i++ {
		entries = append(entries, sitemapEntry{Name: fmt.Sprintf("page-%d", i), Update: stamp(int64(i))})
	}
	p := newSitemapProgress(w, entries, nil)

	for _, e := range entries {
		p.markDone(e.Name, e.Update)
	}
	p.checkpoint(false)
	if _, err := os.Stat(w.sitemapPath()); !os.IsNotExist(err) {
		t.Fatalf("checkpoint wrote before the interval elapsed (err = %v)", err)
	}

	p2 := newSitemapProgress(w, entries, nil)
	p2.last = time.Now().Add(-2 * policy.minElapsed())
	p2.markDone(entries[0].Name, entries[0].Update)
	p2.checkpoint(false)
	if _, err := os.Stat(w.sitemapPath()); !os.IsNotExist(err) {
		t.Fatalf("checkpoint wrote for a single page (err = %v)", err)
	}

	p.last = time.Now().Add(-2 * policy.minElapsed())
	p.checkpoint(false)
	if _, err := os.Stat(w.sitemapPath()); err != nil {
		t.Fatalf("checkpoint did not write after %d pages and a full interval: %v", len(entries), err)
	}
}

func TestCheckpointPolicyOverrides(t *testing.T) {
	// A zero page requirement checkpoints as soon as the time condition is met.
	w := testWiki(t)
	w.checkpoint = CheckpointPolicy{Pages: 0, Seconds: 0}
	p := newSitemapProgress(w, entriesOf("a", 1), nil)
	p.markDone("a", stamp(1))
	p.checkpoint(false)
	if _, err := os.Stat(w.sitemapPath()); err != nil {
		t.Fatalf("zero-threshold policy did not checkpoint: %v", err)
	}

	// A negative value disables checkpointing entirely, even when forced.
	w2 := testWiki(t)
	w2.checkpoint = CheckpointPolicy{Pages: -1, Seconds: 30}
	p2 := newSitemapProgress(w2, entriesOf("a", 1), nil)
	p2.markDone("a", stamp(1))
	p2.checkpoint(true)
	if _, err := os.Stat(w2.sitemapPath()); !os.IsNotExist(err) {
		t.Fatalf("disabled policy still wrote a checkpoint (err = %v)", err)
	}
}

func TestWriteJSONIsAtomicAndCleanable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meta", "sitemap.json")
	if err := writeJSON(path, jsonx.NewObject()); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	metaDir := filepath.Join(dir, "meta")
	left, _ := os.ReadDir(metaDir)
	if len(left) != 1 {
		t.Fatalf("expected only the target file, got %d entries", len(left))
	}

	if err := os.WriteFile(filepath.Join(metaDir, tempPrefix+"sitemap.json-123"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanTempFiles(metaDir)
	left, _ = os.ReadDir(metaDir)
	if len(left) != 1 || left[0].Name() != "sitemap.json" {
		t.Fatalf("cleanTempFiles left %v", left)
	}
}
