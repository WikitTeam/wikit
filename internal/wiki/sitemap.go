package wiki

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	reMetaDomain   = regexp.MustCompile(`WIKIREQUEST\.info\.domain = "([^"]+)"`)
	reMetaSiteID   = regexp.MustCompile(`WIKIREQUEST\.info\.siteId = (\d+)`)
	reMetaSlug     = regexp.MustCompile(`WIKIREQUEST\.info\.siteUnixName = "([^"]+)"`)
	reMetaHomePage = regexp.MustCompile(`WIKIREQUEST\.info\.pageUnixName = "([^"]+)"`)
	reMetaLang     = regexp.MustCompile(`WIKIREQUEST\.info\.lang = '([^']+)'`)
	rePageXML      = regexp.MustCompile(`_page_([0-9]+)\.xml$`)
)

func (w *WikiDot) fetchSiteMetadata() (SiteMeta, error) {
	body, err := w.get(w.url, nil)
	if err != nil {
		return SiteMeta{}, err
	}
	html := string(body)
	ex := func(re *regexp.Regexp) (string, error) {
		m := re.FindStringSubmatch(html)
		if m == nil {
			return "", fmt.Errorf("regex %v failed on %s for site metadata", re, w.url)
		}
		return m[1], nil
	}
	domain, err := ex(reMetaDomain)
	if err != nil {
		return SiteMeta{}, err
	}
	siteID, err := ex(reMetaSiteID)
	if err != nil {
		return SiteMeta{}, err
	}
	slug, err := ex(reMetaSlug)
	if err != nil {
		return SiteMeta{}, err
	}
	home, err := ex(reMetaHomePage)
	if err != nil {
		return SiteMeta{}, err
	}
	lang, err := ex(reMetaLang)
	if err != nil {
		return SiteMeta{}, err
	}
	return SiteMeta{Domain: domain, SiteID: mustI64(siteID), Slug: slug, HomePage: home, Language: lang}, nil
}

// fetchSiteMap recursively walks sitemap.xml, appending page entries (in document
// order, which the written sitemap.json must preserve).
func (w *WikiDot) fetchSiteMap(sitemapURL string, entries *[]sitemapEntry) error {
	var body []byte
	var lastErr error
	for i := 0; i < 40; i++ {
		w.logf("Fetching %s", sitemapURL)
		b, err := w.get(sitemapURL, nil)
		if err == nil {
			body = b
			break
		}
		w.logf("Exception while fetching %s: %v (tries left: %d)", sitemapURL, err, 40-i-1)
		w.client.ChangeFingerprint()
		time.Sleep(4 * time.Second)
		lastErr = err
	}
	if body == nil {
		return lastErr
	}

	dec := xml.NewDecoder(strings.NewReader(string(body)))
	var inURL, inSitemap bool
	var curLoc, curLastmod string
	var field string

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "url":
				inURL, curLoc, curLastmod = true, "", ""
			case "sitemap":
				inSitemap, curLoc = true, ""
			case "loc":
				field = "loc"
			case "lastmod":
				field = "lastmod"
			}
		case xml.CharData:
			switch field {
			case "loc":
				curLoc += string(t)
			case "lastmod":
				curLastmod += string(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "loc", "lastmod":
				field = ""
			case "sitemap":
				if inSitemap && rePageXML.MatchString(curLoc) {
					if err := w.fetchSiteMap(strings.TrimSpace(curLoc), entries); err != nil {
						return err
					}
				}
				inSitemap = false
			case "url":
				if inURL {
					w.appendSitemapEntry(strings.TrimSpace(curLoc), strings.TrimSpace(curLastmod), entries)
				}
				inURL = false
			}
		}
	}
	return nil
}

func (w *WikiDot) appendSitemapEntry(loc, lastmod string, entries *[]sitemapEntry) {
	if loc == "" {
		return
	}
	if strings.HasPrefix(loc, w.url) {
		loc = loc[len(w.url):]
	} else if strings.HasPrefix(loc, "http") {
		// custom domain: take the path
		if i := strings.Index(loc, "//"); i != -1 {
			rest := loc[i+2:]
			if s := strings.IndexByte(rest, '/'); s != -1 {
				loc = rest[s+1:]
			} else {
				loc = ""
			}
		}
	}
	if loc == "" || loc == "/" {
		return
	}
	if strings.HasPrefix(loc, "/forum/") || strings.HasPrefix(loc, "forum/") {
		return
	}
	loc = strings.TrimPrefix(loc, "/")

	var ms *int64
	if lastmod != "" {
		if t, err := parseLastmod(lastmod); err == nil {
			v := t.UnixMilli()
			ms = &v
		}
	}
	*entries = append(*entries, sitemapEntry{Name: loc, Update: ms})
}

func parseLastmod(s string) (time.Time, error) {
	layouts := []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02"}
	var err error
	for _, l := range layouts {
		var t time.Time
		if t, err = time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}

// ---- incremental sitemap checkpointing ----

// CheckpointPolicy controls how often the resumable sitemap checkpoint is
// written. Both thresholds must be crossed before a checkpoint lands: the whole
// sitemap is rewritten each time (megabytes on a large wiki), so a fast run must
// not rewrite it every few seconds. Losing up to this much work to a kill is
// cheap; rewriting the file thousands of times is not.
//
// A zero field drops that condition (checkpoint as soon as the other one is
// met); a negative field disables checkpointing altogether, restoring the
// pre-0.1.7 behaviour of only writing the sitemap once the run finishes.
type CheckpointPolicy struct {
	Pages   int // minimum newly-archived pages between checkpoints
	Seconds int // minimum seconds between checkpoints
}

// DefaultCheckpointPolicy is used when neither config.json nor a flag says
// otherwise.
func DefaultCheckpointPolicy() CheckpointPolicy {
	return CheckpointPolicy{Pages: 50, Seconds: 30}
}

func (c CheckpointPolicy) disabled() bool { return c.Pages < 0 || c.Seconds < 0 }

func (c CheckpointPolicy) minPages() int {
	if c.Pages < 0 {
		return 0
	}
	return c.Pages
}

func (c CheckpointPolicy) minElapsed() time.Duration {
	if c.Seconds < 0 {
		return 0
	}
	return time.Duration(c.Seconds) * time.Second
}

// sitemapProgress accumulates the pages this run has fully archived and writes
// meta/sitemap.json as it goes, so an interrupted run leaves a resumable
// sitemap behind instead of nothing (the final full write at the end of the
// work loop supersedes every checkpoint, keeping completed runs byte-identical
// to what they produced before).
//
// It is seeded from the previous sitemap, so a checkpoint is never a downgrade
// from what was already on disk: pages this run hasn't reached yet keep their
// old stamp and are re-checked next time, pages it finished get the new one.
// Only entries of the current sitemap are ever emitted, so pages that vanished
// from the site drop out exactly as the final write would drop them.
type sitemapProgress struct {
	w      *WikiDot
	order  []sitemapEntry // this run's sitemap, in document order
	policy CheckpointPolicy

	mu    sync.Mutex
	done  map[string]*int64 // page name -> stamp to record
	dirty int
	last  time.Time

	// writeMu serializes whole checkpoints (snapshot + write) so a slow write
	// can't land after a newer snapshot and regress the file.
	writeMu sync.Mutex
}

func newSitemapProgress(w *WikiDot, entries []sitemapEntry, oldMap map[string]*int64) *sitemapProgress {
	done := make(map[string]*int64, len(entries))
	for _, e := range entries {
		if stamp, ok := oldMap[e.Name]; ok {
			done[e.Name] = stamp
		}
	}
	return &sitemapProgress{w: w, order: entries, policy: w.checkpoint, done: done, last: time.Now()}
}

// markDone records that name is fully archived at the given sitemap stamp.
// Pages the fast path skipped need no call: their stamp is unchanged, so the
// seed already carries it.
func (p *sitemapProgress) markDone(name string, stamp *int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if prev, ok := p.done[name]; ok && stampEqual(prev, stamp) {
		return
	}
	p.done[name] = stamp
	p.dirty++
}

// checkpoint writes the sitemap and the pending/id state if enough has changed
// since the last one (or force is set). It is a no-op when nothing changed, so
// a run with no updates never touches the file.
func (p *sitemapProgress) checkpoint(force bool) {
	if p == nil || p.policy.disabled() {
		return
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	p.mu.Lock()
	if p.dirty == 0 || (!force && (p.dirty < p.policy.minPages() || time.Since(p.last) < p.policy.minElapsed())) {
		p.mu.Unlock()
		return
	}
	snapshot := make([]sitemapEntry, 0, len(p.done))
	for _, e := range p.order {
		if stamp, ok := p.done[e.Name]; ok {
			snapshot = append(snapshot, sitemapEntry{Name: e.Name, Update: stamp})
		}
	}
	p.dirty = 0
	p.last = time.Now()
	p.mu.Unlock()

	if err := p.w.writeSiteMap(snapshot); err != nil {
		p.w.errf("Could not checkpoint sitemap: %v", err)
	}
	if err := p.w.state.flush(); err != nil {
		p.w.errf("Could not checkpoint state: %v", err)
	}
}

func findMostRevision(revs []PageRevision) (int64, bool) {
	if len(revs) == 0 {
		return 0, false
	}
	max := revs[0].Revision
	for _, r := range revs {
		if r.Revision > max {
			max = r.Revision
		}
	}
	return max, true
}

// snapshot returns the pages recorded as fully archived, in sitemap document
// order. Callers hold no lock.
func (p *sitemapProgress) snapshot() []sitemapEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]sitemapEntry, 0, len(p.done))
	for _, e := range p.order {
		if stamp, ok := p.done[e.Name]; ok {
			out = append(out, sitemapEntry{Name: e.Name, Update: stamp})
		}
	}
	return out
}

// finish writes the definitive sitemap for a completed page phase. It is not a
// checkpoint: the throttle and the dirty counter are ignored, and a policy that
// disabled checkpointing still gets this one write.
//
// Unlike writing the raw sitemap entries, this records only pages that actually
// reached disk, so pages that failed into pending_pages keep their old stamp (or
// stay absent) and are retried on the next run instead of being skipped as
// up-to-date.
func (p *sitemapProgress) finish() error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	snapshot := p.snapshot()
	p.mu.Lock()
	p.dirty = 0
	p.last = time.Now()
	p.mu.Unlock()

	return p.w.writeSiteMap(snapshot)
}
