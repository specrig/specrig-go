package flows

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// Run writes what Assemble builds that is not on disk already, removes a page it
// wrote before and would not now, and says what it did on out. It fails on prose
// that lost its flow and on what the services' records leave unclosed.
func Run(root, path string, out io.Writer) error {
	files, foreign, err := Assemble(root, path)
	if err != nil {
		return err
	}
	say := func(mark, text string) { _, _ = fmt.Fprintf(out, "  %s %s\n", mark, text) }
	for _, f := range foreign {
		say("!", "skipped "+f+": it is not a specrig record")
	}
	if current, err := os.ReadFile(path); err == nil {
		for _, change := range LinkChanges(current, files[path]) {
			say("!", change)
		}
	}
	stale := stalePages(filepath.Dir(path), files)

	written := 0
	for _, p := range slices.Sorted(maps.Keys(files)) {
		changed, err := writeIfChanged(p, files[p])
		if err != nil {
			return err
		}
		if changed {
			written++
		}
	}
	for _, p := range stale {
		if err := os.Remove(p); err != nil {
			return err
		}
		say("+", "removed "+p)
	}
	if summary := fmt.Sprintf("%s and %d pages", path, len(files)-1); written == 0 {
		say("=", summary+" — up to date")
	} else {
		say("+", fmt.Sprintf("wrote %d of %s", written, summary))
	}

	var index Index
	if err := yaml.Unmarshal(files[path], &index); err != nil {
		return err
	}
	if len(index.Orphaned) > 0 {
		return fmt.Errorf("%s: some prose lost its flow — see orphaned", path)
	}
	if index.System != nil && index.System.Failed() {
		// What the records only tell is in the index, not in the failure.
		failing := *index.System
		failing.PatternOnly, failing.Loops, failing.Outside = nil, nil, nil
		return fmt.Errorf("the services' records do not close — see system in %s:\n%s", path, failing.String())
	}
	return nil
}

// A page this command wrote before and would not write now.
func stalePages(dir string, files map[string][]byte) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() || filepath.Ext(path) != ".md" || files[path] != nil {
			continue
		}
		if body, err := os.ReadFile(path); err == nil && Generated(body) {
			out = append(out, path)
		}
	}
	return out
}

// A file already holding body is not touched, so a run that changes nothing
// leaves no trace on disk.
func writeIfChanged(path string, body []byte) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, body) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, body, 0o644)
}
