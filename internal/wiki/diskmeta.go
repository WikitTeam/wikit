package wiki

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"wikit/internal/jsonx"
)

// fileMapEntry is one entry of meta/file_map.json: file_id -> {url, path}.
type fileMapEntry struct {
	URL  string
	Path string
}

// state holds the incremental bookkeeping the original keeps in meta/*.json:
// pending queues plus the file and page-id maps. It is persisted in the exact
// JSON shapes the original wrote.
type state struct {
	mu sync.Mutex

	dir string // <workdir>/meta

	pendingFiles     []int64
	pendingPages     []string
	pendingRevisions map[int64]int64 // global_revision -> page_id
	fileMap          map[int64]fileMapEntry
	pageIDMap        map[int64]string

	dirtyPendingFiles bool
	dirtyPendingPages bool
	dirtyPendingRevs  bool
	dirtyFileMap      bool
	dirtyPageIDMap    bool
}

func newState(metaDir string) *state {
	return &state{
		dir:              metaDir,
		pendingRevisions: map[int64]int64{},
		fileMap:          map[int64]fileMapEntry{},
		pageIDMap:        map[int64]string{},
	}
}

func (s *state) load() {
	s.pendingFiles = loadInt64Array(filepath.Join(s.dir, "pending_files.json"))
	s.pendingPages = loadStringArray(filepath.Join(s.dir, "pending_pages.json"))
	s.pendingRevisions = loadIntKeyedInt(filepath.Join(s.dir, "pending_revisions.json"))
	s.fileMap = loadFileMap(filepath.Join(s.dir, "file_map.json"))
	s.pageIDMap = loadIntKeyedString(filepath.Join(s.dir, "page_id_map.json"))
}

// ---- mutation helpers (mirror pushToSet/removeFromSet semantics) ----

func (s *state) pushPendingFile(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.pendingFiles {
		if v == id {
			return
		}
	}
	s.pendingFiles = append(s.pendingFiles, id)
	s.dirtyPendingFiles = true
}

func (s *state) removePendingFile(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.pendingFiles {
		if v == id {
			s.pendingFiles = append(s.pendingFiles[:i], s.pendingFiles[i+1:]...)
			s.dirtyPendingFiles = true
			return
		}
	}
}

func (s *state) pushPendingPage(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.pendingPages {
		if v == name {
			return
		}
	}
	s.pendingPages = append(s.pendingPages, name)
	s.dirtyPendingPages = true
}

func (s *state) removePendingPage(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.pendingPages {
		if v == name {
			s.pendingPages = append(s.pendingPages[:i], s.pendingPages[i+1:]...)
			s.dirtyPendingPages = true
			return
		}
	}
}

func (s *state) setFileMap(id int64, e fileMapEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileMap[id] = e
	s.dirtyFileMap = true
}

func (s *state) setPageID(id int64, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pageIDMap[id] != name {
		s.pageIDMap[id] = name
		s.dirtyPageIDMap = true
	}
}

func (s *state) deletePageID(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pageIDMap[id]; ok {
		delete(s.pageIDMap, id)
		s.dirtyPageIDMap = true
	}
}

func (s *state) addPendingRevision(globalRev, pageID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingRevisions[globalRev] = pageID
	s.dirtyPendingRevs = true
}

func (s *state) deletePendingRevision(globalRev int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pendingRevisions[globalRev]; ok {
		delete(s.pendingRevisions, globalRev)
		s.dirtyPendingRevs = true
	}
}

// flush writes any dirty state files. Each file is written only when changed,
// keeping unchanged backups byte-identical.
func (s *state) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if s.dirtyPendingFiles {
		if err := writeJSON(filepath.Join(s.dir, "pending_files.json"), int64Array(s.pendingFiles)); err != nil {
			return err
		}
		s.dirtyPendingFiles = false
	}
	if s.dirtyPendingPages {
		if err := writeJSON(filepath.Join(s.dir, "pending_pages.json"), stringSliceArray(s.pendingPages)); err != nil {
			return err
		}
		s.dirtyPendingPages = false
	}
	if s.dirtyPendingRevs {
		if err := writeJSON(filepath.Join(s.dir, "pending_revisions.json"), intKeyedIntObject(s.pendingRevisions)); err != nil {
			return err
		}
		s.dirtyPendingRevs = false
	}
	if s.dirtyFileMap {
		if err := writeJSON(filepath.Join(s.dir, "file_map.json"), fileMapObject(s.fileMap)); err != nil {
			return err
		}
		s.dirtyFileMap = false
	}
	if s.dirtyPageIDMap {
		if err := writeJSON(filepath.Join(s.dir, "page_id_map.json"), intKeyedStringObject(s.pageIDMap)); err != nil {
			return err
		}
		s.dirtyPageIDMap = false
	}
	return nil
}

// ---- jsonx conversions ----

func int64Array(v []int64) []any {
	arr := make([]any, len(v))
	for i, n := range v {
		arr[i] = n
	}
	return arr
}

func stringSliceArray(v []string) []any {
	arr := make([]any, len(v))
	for i, s := range v {
		arr[i] = s
	}
	return arr
}

func intKeyedIntObject(m map[int64]int64) *jsonx.Object {
	o := jsonx.NewObject()
	for _, k := range sortedInt64Keys(m) {
		o.Set(strconv.FormatInt(k, 10), m[k])
	}
	return o
}

func intKeyedStringObject(m map[int64]string) *jsonx.Object {
	o := jsonx.NewObject()
	for _, k := range sortedStringValKeys(m) {
		o.Set(strconv.FormatInt(k, 10), m[k])
	}
	return o
}

func fileMapObject(m map[int64]fileMapEntry) *jsonx.Object {
	o := jsonx.NewObject()
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		e := m[k]
		inner := jsonx.NewObject()
		inner.Set("url", e.URL)
		inner.Set("path", e.Path)
		o.Set(strconv.FormatInt(k, 10), inner)
	}
	return o
}

func sortedInt64Keys(m map[int64]int64) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func sortedStringValKeys(m map[int64]string) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// ---- loaders ----

func loadInt64Array(path string) []int64 {
	v := decodeFile(path)
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]int64, 0, len(arr))
	for _, e := range arr {
		out = append(out, asInt64(e))
	}
	return out
}

func loadStringArray(path string) []string {
	v := decodeFile(path)
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func loadIntKeyedInt(path string) map[int64]int64 {
	out := map[int64]int64{}
	v := decodeFile(path)
	o, ok := v.(*jsonx.Object)
	if !ok {
		return out
	}
	for _, k := range o.Keys() {
		ki, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue
		}
		val, _ := o.Get(k)
		out[ki] = asInt64(val)
	}
	return out
}

func loadIntKeyedString(path string) map[int64]string {
	out := map[int64]string{}
	v := decodeFile(path)
	o, ok := v.(*jsonx.Object)
	if !ok {
		return out
	}
	for _, k := range o.Keys() {
		ki, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue
		}
		val, _ := o.Get(k)
		if s, ok := val.(string); ok {
			out[ki] = s
		}
	}
	return out
}

func loadFileMap(path string) map[int64]fileMapEntry {
	out := map[int64]fileMapEntry{}
	v := decodeFile(path)
	o, ok := v.(*jsonx.Object)
	if !ok {
		return out
	}
	for _, k := range o.Keys() {
		ki, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue
		}
		val, _ := o.Get(k)
		inner, ok := val.(*jsonx.Object)
		if !ok {
			continue
		}
		out[ki] = fileMapEntry{URL: getStr(inner, "url"), Path: getStr(inner, "path")}
	}
	return out
}

func decodeFile(path string) any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	v, err := jsonx.Decode(data)
	if err != nil {
		return nil
	}
	return v
}

// tempPrefix marks the scratch files writeJSON renames into place. Kept
// distinctive so cleanTempFiles can recognise its own leftovers.
const tempPrefix = ".wikit-tmp-"

// writeJSON marshals v with the byte-exact encoder and writes it atomically:
// into a scratch file in the same directory, then rename over the target. A
// process killed mid-write therefore leaves either the old file or the new one,
// never a truncated one — which matters for sitemap.json, since a corrupt
// sitemap silently downgrades the next run to a full scan.
func writeJSON(path string, v any) error {
	data, err := jsonx.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, tempPrefix+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	// CreateTemp makes the file 0600; match the 0644 the direct write used.
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// cleanTempFiles removes scratch files a previously killed run left behind in
// dir. Non-recursive and best-effort.
func cleanTempFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), tempPrefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
