package flows

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type caseKey struct{ file, title string }

type features struct {
	dir      string
	comments map[caseKey]string  // title as normal() spells it
	files    map[string][]string // title -> the files holding a case of it
}

// Reads every .feature below dir; file names are kept relative to dir.
func readFeatures(dir string) features {
	f := features{dir: dir, comments: map[caseKey]string{}, files: map[string][]string{}}
	_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".feature") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		for _, c := range parseFeature(string(body)) {
			title := normal(c.title)
			if _, seen := f.comments[caseKey{rel, title}]; !seen {
				f.files[title] = append(f.files[title], rel)
			}
			f.comments[caseKey{rel, title}] = c.comment
		}
		return nil
	})
	return f
}

// The file a traced case is in: the one the suite named, or the only file with
// a case of that title. A title in several files and no name says nothing.
func (f features) file(uri, title string) string {
	if uri != "" {
		p := filepath.FromSlash(uri)
		if !filepath.IsAbs(p) {
			p = filepath.Join(home, p)
		}
		if rel, err := filepath.Rel(f.dir, p); err == nil {
			if _, ok := f.comments[caseKey{filepath.ToSlash(rel), title}]; ok {
				return filepath.ToSlash(rel)
			}
		}
	}
	if files := f.files[title]; len(files) == 1 {
		return files[0]
	}
	return ""
}

type parsedCase struct {
	title   string
	comment string
}

// Each case with the comment above it, the one place Rule #11 puts the reason.
func parseFeature(body string) (cases []parsedCase) {
	var pending []string
	feature := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "#"):
			pending = append(pending, strings.TrimSpace(strings.TrimPrefix(line, "#")))
		case line == "":
			// Before Feature: the header may span blank lines; after it a blank
			// line ends a comment that belongs to no case, such as a divider.
			if feature {
				pending = nil
			}
		case strings.HasPrefix(line, "Feature:"):
			pending, feature = nil, true
		case strings.HasPrefix(line, "@"):
		case scenario(line) != "":
			cases = append(cases, parsedCase{title: scenario(line), comment: prose(pending)})
			pending = nil
		default:
			pending = nil
		}
	}
	return cases
}

func scenario(line string) string {
	for _, keyword := range []string{"Scenario Outline:", "Scenario Template:", "Scenario:", "Example:"} {
		if title, ok := strings.CutPrefix(line, keyword); ok {
			return strings.TrimSpace(title)
		}
	}
	return ""
}

// Comment lines back into paragraphs: an empty # line is the break.
func prose(lines []string) string {
	var paragraphs, current []string
	for _, l := range lines {
		if l == "" {
			if len(current) > 0 {
				paragraphs = append(paragraphs, strings.Join(current, " "))
				current = nil
			}
			continue
		}
		current = append(current, l)
	}
	if len(current) > 0 {
		paragraphs = append(paragraphs, strings.Join(current, " "))
	}
	return strings.Join(paragraphs, "\n\n")
}

// go test names a subtest by its title with spaces turned to underscores, the
// test's own name in front and a #NN on a repeat; this takes all three back.
func normal(subtest string) string {
	if _, name, ok := strings.Cut(subtest, "/"); ok {
		subtest = name
	}
	if i := strings.LastIndex(subtest, "#"); i > 0 && strings.Trim(subtest[i+1:], "0123456789") == "" {
		subtest = subtest[:i]
	}
	return strings.ReplaceAll(subtest, "_", " ")
}

// Leaf paths, arrays folded into [], so two messages compare by shape. A null or
// an empty container carries no field: a case sends one to test a broken message.
func paths(body json.RawMessage, into map[string]bool) {
	var v any
	if len(body) == 0 || json.Unmarshal(body, &v) != nil {
		return
	}
	var walk func(v any, prefix string)
	walk = func(v any, prefix string) {
		switch t := v.(type) {
		case nil:
		case map[string]any:
			for k, child := range t {
				walk(child, join(prefix, k))
			}
		case []any:
			for _, child := range t {
				walk(child, prefix+"[]")
			}
		default:
			if prefix != "" {
				into[prefix] = true
			}
		}
	}
	walk(v, "")
}

func join(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}
