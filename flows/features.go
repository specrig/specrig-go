package flows

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	walkFeatures(dir, func(rel string, cases []parsedCase) {
		for _, c := range cases {
			title := spelled(c.title)
			if _, seen := f.comments[caseKey{rel, title}]; !seen {
				f.files[title] = append(f.files[title], rel)
			}
			f.comments[caseKey{rel, title}] = c.comment
		}
	})
	return f
}

// The comment above every case below dir, by its title as written and by each
// tail of its file's path: a stack's suite names the file from its own directory.
func readComments(dir string) map[caseKey]string {
	out := map[caseKey]string{}
	walkFeatures(dir, func(rel string, cases []parsedCase) {
		parts := strings.Split(rel, "/")
		for i := range parts {
			for _, c := range cases {
				// The Go writer reads a title back from its go test name, where _ stood for a space.
				for _, title := range []string{c.title, strings.ReplaceAll(c.title, "_", " ")} {
					out[caseKey{strings.Join(parts[i:], "/"), title}] = c.comment
				}
			}
		}
	})
	return out
}

func walkFeatures(dir string, each func(rel string, cases []parsedCase)) {
	_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".feature") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		each(filepath.ToSlash(rel), parseFeature(string(body)))
		return nil
	})
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
	var pending, header []string
	var outline parsedCase
	feature, examples := false, false
	for raw := range strings.SplitSeq(body, "\n") {
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
			outline = parsedCase{title: scenario(line), comment: prose(pending)}
			cases = append(cases, outline)
			pending, examples = nil, false
		case strings.HasPrefix(line, "Examples:"), strings.HasPrefix(line, "Scenarios:"):
			pending, header, examples = nil, nil, true
		case examples && strings.HasPrefix(line, "|"):
			if row := cells(line); header == nil {
				header = row
			} else {
				cases = append(cases, outline.filled(header, row))
			}
			pending = nil
		default:
			pending = nil
		}
	}
	return cases
}

// The case an outline runs for one Examples row, titled as gherkin names it.
func (c parsedCase) filled(header, row []string) parsedCase {
	for i := range min(len(header), len(row)) {
		c.title = strings.ReplaceAll(c.title, "<"+header[i]+">", row[i])
	}
	return c
}

var (
	cell = regexp.MustCompile(`(?:[^|\\]|\\.)*\|`)
	// gherkin's escapes in a table cell.
	unescape = strings.NewReplacer(`\|`, "|", `\\`, `\`, `\n`, "\n")
)

func cells(row string) []string {
	out := cell.FindAllString(strings.TrimPrefix(row, "|"), -1)
	for i, c := range out {
		out[i] = strings.TrimSpace(unescape.Replace(strings.TrimSuffix(c, "|")))
	}
	return out
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
	return spelled(subtest)
}

// A title as normal() reads it back from its go test name; a / in it is its own.
func spelled(title string) string {
	if i := strings.LastIndex(title, "#"); i > 0 && strings.Trim(title[i+1:], "0123456789") == "" {
		title = title[:i]
	}
	return strings.ReplaceAll(title, "_", " ")
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
