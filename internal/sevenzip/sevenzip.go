// Package sevenzip wraps a 7-Zip command-line binary to create and list the .7z
// archives WikiComma uses for page revisions and forum threads. The 7z engine is
// bundled (embedded) so wikit needs nothing installed; if no embedded binary is
// available for the platform it falls back to a 7z/7za/7zz found on PATH.
//
// Archive container bytes are not reproducible across tools/runs (7z stores file
// timestamps), but the members and their contents are identical, which is what
// matters for compatibility.
package sevenzip

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	resolveOnce sync.Once
	resolvedBin string
	resolveErr  error
)

// Bin returns the path to a usable 7z binary, extracting the embedded one on
// first use.
func Bin() (string, error) {
	resolveOnce.Do(func() {
		if p, err := extractEmbedded(); err == nil && p != "" {
			resolvedBin = p
			return
		}
		for _, name := range []string{"7z", "7za", "7zz", "7zr"} {
			if p, err := exec.LookPath(name); err == nil {
				resolvedBin = p
				return
			}
		}
		resolveErr = fmt.Errorf("no 7z binary found (not embedded for this platform and not on PATH)")
	})
	return resolvedBin, resolveErr
}

// Add adds files matching fileSpec (a path possibly containing 7z wildcards) to
// the archive at archivePath, creating or updating it. When recursive is true,
// subdirectories are included (-r). This mirrors the original's two call sites:
// page revisions use "<dir>/*.txt" (non-recursive) and forum threads use
// "<dir>/*.*" (recursive), and members must come out as bare "<rev>.txt" and
// "<post>/<rev>.html" respectively -- that is both the reference layout and what
// the incremental scan parses back out of existing archives.
//
// 7z only strips the wildcard's directory prefix when the spec is absolute; with
// a relative spec (which is what a relative workDir produces) it stores the whole
// prefix, e.g. "wikit_data/<wiki>/pages/<name>/0.txt". So instead of trusting the
// spec's shape, run 7z inside the directory being packed and pass a bare pattern.
func Add(archivePath, fileSpec string, recursive bool) error {
	bin, err := Bin()
	if err != nil {
		return err
	}
	// The archive and the binary are resolved absolutely because cmd.Dir moves
	// the process's working directory to the packed directory.
	absArchive, err := filepath.Abs(archivePath)
	if err != nil {
		return err
	}
	if strings.ContainsAny(bin, `/\`) {
		if absBin, err := filepath.Abs(bin); err == nil {
			bin = absBin
		}
	}
	baseDir, pattern := filepath.Split(fileSpec)

	args := []string{"a", absArchive, pattern, "-y", "-bso0", "-bsp0"}
	if recursive {
		args = append(args, "-r")
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = baseDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("7z add failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Extract unpacks every member of archivePath into destDir, keeping the member
// paths (7z "x"). destDir is created by 7z if missing. Used by the archive
// repair pass, which unpacks a mislaid archive before rebuilding it.
func Extract(archivePath, destDir string) error {
	bin, err := Bin()
	if err != nil {
		return err
	}
	absArchive, err := filepath.Abs(archivePath)
	if err != nil {
		return err
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "x", absArchive, "-o"+absDest, "-y", "-bso0", "-bsp0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("7z extract failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// List returns the member paths inside an archive (using forward slashes),
// excluding directories. Used by the incremental scan to learn which revisions
// and forum posts are already archived.
func List(archivePath string) ([]string, error) {
	bin, err := Bin()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "l", "-slt", "-ba", archivePath)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("7z list failed: %v", err)
	}

	var files []string
	var curPath string
	var isDir bool
	flush := func() {
		if curPath != "" && !isDir {
			files = append(files, strings.ReplaceAll(curPath, "\\", "/"))
		}
		curPath = ""
		isDir = false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		if v, ok := strings.CutPrefix(line, "Path = "); ok {
			flush()
			curPath = v
		} else if v, ok := strings.CutPrefix(line, "Attributes = "); ok {
			isDir = strings.HasPrefix(v, "D")
		}
	}
	flush()
	return files, nil
}
