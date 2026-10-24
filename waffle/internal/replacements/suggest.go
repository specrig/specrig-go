package replacements

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	check "github.com/specrig/specrig-go/waffle/replacements"
	waffle "gitlab.com/gorib/waffle/container"
)

type tier int

const (
	tierExternal tier = iota
	tierAPI
	tierPkg
	tierDomain
	tierStdlib
)

var tierOrder = []tier{tierExternal, tierAPI, tierPkg, tierDomain, tierStdlib}

var tierHeading = map[tier]string{
	tierExternal: "outside this module",
	tierAPI:      "api/ — generated clients of other services",
	tierPkg:      "pkg/ — this service's own infrastructure",
	tierDomain:   "ports of this service",
	tierStdlib:   "standard library",
}

// Tiers order the report and nothing else. Which of these a test may replace is
// the allow list's answer, so nothing here is demoted.
func tierOf(path, module string) tier {
	rel, inside := strings.CutPrefix(path, module+"/")
	first, _, _ := strings.Cut(path, "/")
	switch {
	// A standard library import path has no dot in its first element, which is
	// the rule the go tool goes by too.
	case !inside && !strings.Contains(first, "."):
		return tierStdlib
	case !inside:
		return tierExternal
	case strings.HasPrefix(rel, "api/"):
		return tierAPI
	case strings.HasPrefix(rel, "pkg/"):
		return tierPkg
	default:
		return tierDomain
	}
}

type candidate struct {
	Path    string
	Name    string
	Leaf    bool
	Allowed bool
}

func candidates(seams []waffle.Seam, cfg check.Config) []candidate {
	var out []candidate
	seen := map[string]bool{}
	for _, seam := range seams {
		for _, ref := range seam.Types {
			if seen[ref.String()] {
				continue
			}
			seen[ref.String()] = true
			out = append(out, candidate{
				Path:    ref.Path,
				Name:    ref.Name,
				Leaf:    seam.Leaf,
				Allowed: cfg.Allowed(ref.Path, ref.Name),
			})
		}
	}
	return out
}

func composites(seams []waffle.Seam) []waffle.Seam {
	var out []waffle.Seam
	for _, seam := range seams {
		if seam.Composite() {
			out = append(out, seam)
		}
	}
	return out
}

// Mapping lines only, in the shape replacements.allow takes, so granting one is
// a copied line.
func entries(cands []candidate, module, indent string) []string {
	byPath := map[string][]candidate{}
	byTier := map[tier][]string{}
	for _, c := range cands {
		if _, known := byPath[c.Path]; !known {
			byTier[tierOf(c.Path, module)] = append(byTier[tierOf(c.Path, module)], c.Path)
		}
		byPath[c.Path] = append(byPath[c.Path], c)
	}

	var out []string
	for _, t := range tierOrder {
		paths := byTier[t]
		if len(paths) == 0 {
			continue
		}
		sort.Strings(paths)
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, indent+"# "+tierHeading[t])
		for _, path := range paths {
			group := byPath[path]
			sort.Slice(group, func(i, j int) bool { return group[i].Name < group[j].Name })

			// One interface stays inline — there is nothing to tell apart, and a
			// block would double the height of a report that is mostly singles.
			if len(group) == 1 {
				out = append(out, indent+fmt.Sprintf("%s: [%s]%s", path, group[0].Name, note(group[0])))
				continue
			}
			// Several go one per line, so a marker sits on the interface it
			// describes rather than naming it from the end of a shared line.
			out = append(out, indent+path+":")
			for _, c := range group {
				out = append(out, indent+"  - "+c.Name+note(c))
			}
		}
	}
	return out
}

func note(c candidate) string {
	var marks []string
	if c.Leaf {
		marks = append(marks, "leaf")
	}
	if c.Allowed {
		marks = append(marks, "allowed")
	}
	if len(marks) == 0 {
		return ""
	}
	return "  # " + strings.Join(marks, ", ")
}

func suggestion(seams []waffle.Seam, cfg check.Config, module string) string {
	var b strings.Builder
	b.WriteString("# Everything the containers expose. This is a menu, not a proposal: what\n")
	b.WriteString("# replacements.allow does not name, a test may not supply.\n\n")
	for _, line := range entries(candidates(seams, cfg), module, "    ") {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n# leaf — the default constructor takes nothing else out of the container, so it\n")
	b.WriteString("# is a boundary of the process. Granting a leaf leaves the whole service under\n")
	b.WriteString("# test; granting an interior seam cuts out everything below it.\n")

	if merged := composites(seams); len(merged) > 0 {
		b.WriteString("\n# Composite seams — one field diogen merged, serving several declared\n")
		b.WriteString("# contracts. Granting one means granting every constituent, and whatever a\n")
		b.WriteString("# test supplies has to implement all of them.\n")
		for _, seam := range merged {
			// Shortened for reading, but only as far as it stays unambiguous:
			// two constituents can sit in packages that share a last segment.
			paths := make([]string, 0, len(seam.Types))
			for _, ref := range seam.Types {
				paths = append(paths, ref.Path)
			}
			short := distinctSuffixes(paths, "/")

			names := make([]string, 0, len(seam.Types))
			for _, ref := range seam.Types {
				names = append(names, short[ref.Path]+"."+ref.Name)
			}
			_, _ = fmt.Fprintf(&b, "#   %s — %s\n", seam.Field, strings.Join(names, " + "))
		}
	}
	return b.String()
}

// The last segment of each path, lengthened one segment at a time until no two
// are the same. Repeats are collapsed first: one composite seam names the same
// package several times, and no suffix can ever tell a path from itself.
func distinctSuffixes(paths []string, sep string) map[string]string {
	unique := make([]string, 0, len(paths))
	for _, p := range paths {
		if !slices.Contains(unique, p) {
			unique = append(unique, p)
		}
	}

	out := make(map[string]string, len(unique))
	for k := 1; k <= 16; k++ {
		clear(out)
		seen := make(map[string]bool, len(unique))
		clash := false
		for _, p := range unique {
			segments := strings.Split(p, "/")
			if k < len(segments) {
				segments = segments[len(segments)-k:]
			}
			joined := strings.Join(segments, sep)
			if seen[joined] {
				clash = true
				break
			}
			seen[joined] = true
			out[p] = joined
		}
		if !clash {
			break
		}
	}
	return out
}
