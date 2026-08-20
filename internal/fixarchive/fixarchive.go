// Package fixarchive repairs .7z archives written by older wikit builds, whose
// members carried the whole relative work-directory prefix
// ("wikit_data/<wiki>/pages/<name>/0.txt") instead of the bare names the
// WikiComma format uses ("0.txt", and "<post>/<rev>.html" for forum threads).
//
// Besides being awkward to browse, the stray prefix breaks the incremental
// scan: it parses revision numbers straight out of the member names, so a
// prefixed archive looks empty and every revision in it gets fetched again.
//
// Repair unpacks an archive, collects the members under the names they should
// have had, and rebuilds it. The rebuild happens in a sibling temp directory
// and is moved into place only once complete, so an interrupted run leaves the
// original archive untouched.
package fixarchive

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"wikit/internal/sevenzip"
)

// Logger receives progress lines. level is "INFO", "WARN" or "ERROR".
type Logger func(level, msg string)

// Result counts what a repair pass did.
type Result struct {
	Scanned int // archives examined
	Fixed   int // archives rebuilt (or that would be, in a dry run)
	OK      int // archives already in the correct layout
	Failed  int // archives that could not be repaired
}

// Options tunes a repair pass.
type Options struct {
	DryRun bool // report what would change without touching anything
	Log    Logger
}

func (o Options) logf(level, format string, a ...any) {
	if o.Log != nil {
		o.Log(level, fmt.Sprintf(format, a...))
	}
}

// Wiki repairs every page and forum archive under one wiki directory, i.e.
// <base_directory>/<wiki>. A missing pages/ or forum/ subdirectory is not an
// error: the wiki may simply not have been backed up that far.
func Wiki(wikiDir string, opts Options) (Result, error) {
	if st, err := os.Stat(wikiDir); err != nil || !st.IsDir() {
		return Result{}, fmt.Errorf("no wiki directory at %s", wikiDir)
	}

	pages, err := pageArchives(filepath.Join(wikiDir, "pages"))
	if err != nil {
		return Result{}, err
	}
	forums, err := forumArchives(filepath.Join(wikiDir, "forum"))
	if err != nil {
		return Result{}, err
	}

	var total Result
	for _, a := range pages {
		total.Add(repair(a, wikiDir, pageMember, opts))
	}
	for _, a := range forums {
		total.Add(repair(a, wikiDir, forumMember, opts))
	}
	return total, nil
}

// Add accumulates another pass's counts into r.
func (r *Result) Add(other Result) {
	r.Scanned += other.Scanned
	r.Fixed += other.Fixed
	r.OK += other.OK
	r.Failed += other.Failed
}

// pageArchives lists pages/<name>.7z.
func pageArchives(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".7z") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// forumArchives lists forum/<category>/<thread>.7z.
func forumArchives(dir string) ([]string, error) {
	cats, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, c := range cats {
		if !c.IsDir() {
			continue
		}
		threads, err := os.ReadDir(filepath.Join(dir, c.Name()))
		if err != nil {
			continue
		}
		for _, t := range threads {
			if !t.IsDir() && strings.HasSuffix(t.Name(), ".7z") {
				out = append(out, filepath.Join(dir, c.Name(), t.Name()))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// memberFunc maps a member path to the name it should have, and reports whether
// the member belongs in this kind of archive at all.
type memberFunc func(memberPath string) (want string, ok bool)

// pageMember keeps files named "<revision>.txt", flattened.
func pageMember(m string) (string, bool) {
	base := lastComponent(m)
	if !strings.HasSuffix(base, ".txt") {
		return "", false
	}
	if _, err := strconv.ParseInt(strings.TrimSuffix(base, ".txt"), 10, 64); err != nil {
		return "", false
	}
	return base, true
}

// forumMember keeps "<post id>/<revision>.html": the last two components, where
// the post id must be numeric.
func forumMember(m string) (string, bool) {
	base := lastComponent(m)
	if !strings.HasSuffix(base, ".html") {
		return "", false
	}
	parts := strings.Split(toSlash(m), "/")
	if len(parts) < 2 {
		return "", false
	}
	post := parts[len(parts)-2]
	if _, err := strconv.ParseInt(post, 10, 64); err != nil {
		return "", false
	}
	return post + "/" + base, true
}

func toSlash(m string) string { return strings.ReplaceAll(m, "\\", "/") }

// lastComponent returns the file name of a member path, tolerating either
// separator (7z reports backslashes on Windows).
func lastComponent(m string) string {
	m = toSlash(m)
	if i := strings.LastIndexByte(m, '/'); i >= 0 {
		return m[i+1:]
	}
	return m
}

// repair inspects one archive and rebuilds it when its members are misnamed.
func repair(archive, wikiDir string, member memberFunc, opts Options) Result {
	res := Result{Scanned: 1}
	rel := displayPath(archive, wikiDir)

	members, err := sevenzip.List(archive)
	if err != nil {
		opts.logf("ERROR", "%s: cannot read archive: %v", rel, err)
		res.Failed = 1
		return res
	}
	if len(members) == 0 {
		opts.logf("WARN", "%s: archive is empty, leaving it alone", rel)
		res.OK = 1
		return res
	}

	needsFix := false
	for _, m := range members {
		want, ok := member(m)
		if !ok {
			// Something this repair does not understand: never rewrite it,
			// because rebuilding would drop the member.
			opts.logf("WARN", "%s: unexpected member %q, leaving this archive alone", rel, m)
			res.OK = 1
			return res
		}
		if want != toSlash(m) {
			needsFix = true
		}
	}
	if !needsFix {
		res.OK = 1
		return res
	}
	if opts.DryRun {
		opts.logf("INFO", "%s: would rebuild (%d members)", rel, len(members))
		res.Fixed = 1
		return res
	}

	n, err := rebuild(archive, member)
	if err != nil {
		opts.logf("ERROR", "%s: repair failed: %v", rel, err)
		res.Failed = 1
		return res
	}
	opts.logf("INFO", "%s: rebuilt (%d members)", rel, n)
	res.Fixed = 1
	return res
}

// displayPath shortens an archive path to something wiki-relative for logs.
func displayPath(archive, wikiDir string) string {
	if rel, err := filepath.Rel(wikiDir, archive); err == nil {
		return filepath.ToSlash(rel)
	}
	return archive
}

// rebuild unpacks archive, re-lays the files out under their intended names and
// writes a fresh archive over the original, returning the member count. The
// original is replaced only after the new archive has been written in full.
func rebuild(archive string, member memberFunc) (int, error) {
	work, err := os.MkdirTemp(filepath.Dir(archive), ".wikit-fix-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(work)

	unpacked := filepath.Join(work, "unpacked")
	staged := filepath.Join(work, "staged")
	if err := sevenzip.Extract(archive, unpacked); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(staged, 0o755); err != nil {
		return 0, err
	}

	// Collect the unpacked files under the names they should have. A name that
	// occurs at several depths (an archive appended to both before and after
	// the naming fix) is taken from the shallowest path, which is the copy the
	// current code wrote.
	type candidate struct {
		src   string
		depth int
	}
	best := map[string]candidate{}
	err = filepath.WalkDir(unpacked, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(unpacked, p)
		if err != nil {
			return err
		}
		slashRel := filepath.ToSlash(rel)
		want, ok := member(slashRel)
		if !ok {
			return nil
		}
		depth := strings.Count(slashRel, "/")
		if cur, seen := best[want]; !seen || depth < cur.depth {
			best[want] = candidate{src: p, depth: depth}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(best) == 0 {
		return 0, fmt.Errorf("no usable members found after unpacking")
	}

	nested := false
	for want, c := range best {
		if strings.Contains(want, "/") {
			nested = true
		}
		dst := filepath.Join(staged, filepath.FromSlash(want))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return 0, err
		}
		if err := os.Rename(c.src, dst); err != nil {
			return 0, err
		}
	}

	// Pack from the staging directory with the same invocation the backup uses:
	// flat "*.txt" for page revisions, recursive "*.*" for forum threads.
	spec, recursive := filepath.Join(staged, "*.txt"), false
	if nested {
		spec, recursive = filepath.Join(staged, "*.*"), true
	}
	rebuilt := filepath.Join(work, filepath.Base(archive))
	if err := sevenzip.Add(rebuilt, spec, recursive); err != nil {
		return 0, err
	}
	if err := os.Remove(archive); err != nil {
		return 0, err
	}
	if err := os.Rename(rebuilt, archive); err != nil {
		return 0, err
	}
	return len(best), nil
}
