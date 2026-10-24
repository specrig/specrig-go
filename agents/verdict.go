package agents

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	heading = "## Rule #11 — specification before code"
	token   = "@specrig.md"
)

var (
	fence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	ticks = regexp.MustCompile("`+")
)

// Verdict is agents/1's on a service's AGENTS.md, nil where there is none: absent,
// stale, unreferenced or ok.
func Verdict(agents *string) string {
	if agents == nil {
		return "absent"
	}
	stale, referenced := scan(*agents)
	switch {
	case stale:
		return "stale"
	case referenced:
		return "ok"
	}
	return "unreferenced"
}

func scan(text string) (stale, referenced bool) {
	var char byte
	length := 0
	for line := range strings.SplitSeq(text, "\n") {
		if m := fence.FindStringSubmatch(line); m != nil {
			run := m[1]
			if length == 0 {
				char, length = run[0], len(run)
			} else if run[0] == char && len(run) >= length {
				length = 0
			}
			continue
		}
		if length > 0 {
			continue
		}
		stale = stale || strings.TrimRightFunc(line, unicode.IsSpace) == heading
		referenced = referenced || standsAlone(line)
	}
	return stale, referenced
}

func standsAlone(line string) bool {
	code := codeSpans(line)
	for from := 0; ; {
		i := strings.Index(line[from:], token)
		if i < 0 {
			return false
		}
		at, end := from+i, from+i+len(token)
		before, _ := utf8.DecodeLastRuneInString(line[:at])
		after, _ := utf8.DecodeRuneInString(line[end:])
		inCode := false
		for _, span := range code {
			inCode = inCode || (at >= span[0] && at < span[1])
		}
		if (at == 0 || unicode.IsSpace(before)) && (end == len(line) || unicode.IsSpace(after)) && !inCode {
			return true
		}
		from = at + 1
	}
}

func codeSpans(line string) [][2]int {
	runs := ticks.FindAllStringIndex(line, -1)
	var spans [][2]int
	for i := 0; i < len(runs); i++ {
		for j := i + 1; j < len(runs); j++ {
			if runs[j][1]-runs[j][0] == runs[i][1]-runs[i][0] {
				spans = append(spans, [2]int{runs[i][0], runs[j][1]})
				i = j
				break
			}
		}
	}
	return spans
}
