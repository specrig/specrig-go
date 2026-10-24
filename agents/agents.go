// Package agents writes specrig.md from the parts every stack shares, agents/1,
// with a stack's slots filled, and judges whether AGENTS.md brings it in.
package agents

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/specrig/specrig-go/internal/protocol"
)

var parts = []string{"header", "contributing", "short", "rule11", "reference"}

var slot = regexp.MustCompile(`\{\{([a-z-]+)\}\}`)

// Render fills every slot of the part; a slot left unfilled, or a value for a slot
// no part has, is an error.
func Render(part string, slots map[string]string) (string, error) {
	if !slices.Contains(parts, part) {
		return "", fmt.Errorf("no part %q (have: %s)", part, strings.Join(parts, ", "))
	}
	if unknown := Unknown(slots); len(unknown) > 0 {
		return "", fmt.Errorf("no part has the slots %s", strings.Join(unknown, ", "))
	}
	body, err := protocol.Files.ReadFile("agents/" + part + ".md")
	if err != nil {
		return "", err
	}
	var missing []string
	lines := strings.Split(string(body), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if m := slot.FindStringSubmatch(line); m != nil && m[0] == line && slots[m[1]] == "" {
			if _, ok := slots[m[1]]; !ok {
				missing = append(missing, m[1])
			}
			continue
		}
		out = append(out, slot.ReplaceAllStringFunc(line, func(s string) string {
			name := s[2 : len(s)-2]
			value, ok := slots[name]
			if !ok {
				missing = append(missing, name)
			}
			return value
		}))
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return "", fmt.Errorf("%s leaves %s unfilled", part, strings.Join(slices.Compact(missing), ", "))
	}
	text := strings.Join(out, "\n")
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return text, nil
}

// Unknown are the slots given that no part has.
func Unknown(slots map[string]string) []string {
	known := map[string]bool{}
	for _, part := range parts {
		body, _ := protocol.Files.ReadFile("agents/" + part + ".md")
		for _, m := range slot.FindAllStringSubmatch(string(body), -1) {
			known[m[1]] = true
		}
	}
	var unknown []string
	for name := range slots {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	return unknown
}

// ModeRows are the mode table's rows, the cases' patterns in spec's.
func ModeRows(cases []string) string {
	rows := make([]string, 0, len(modes))
	for _, m := range modes {
		allows := strings.ReplaceAll(m.Allows, "{{cases}}", ticked(cases))
		rows = append(rows, fmt.Sprintf("| %s | %s |", m.Name, allows))
	}
	return strings.Join(rows, "\n")
}

// AreaRows are the area table's rows, by the paths each area decides.
func AreaRows(areas []Area) string {
	var rows []string
	for _, a := range areas {
		paths := claimed(areas, a)
		if len(paths) == 0 {
			continue
		}
		open := "as the table says"
		if a.Modes != nil {
			open = "no mode"
			if len(a.Modes) > 0 {
				open = strings.Join(a.Modes, ", ")
			}
		}
		granted := "—"
		if len(a.Grant) > 0 {
			granted = strings.Join(a.Grant, ", ")
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s |", a.Name, ticked(paths), open, granted))
	}
	return strings.Join(rows, "\n")
}

func ticked(paths []string) string {
	return "`" + strings.Join(paths, "`, `") + "`"
}
