package agents

import (
	"path"
	"slices"
	"strings"

	"github.com/specrig/specrig-go/rules"
)

func claimed(areas []rules.Area, a rules.Area) []string {
	var paths []string
	for _, p := range a.Paths {
		literal, glob := atWildcard(p)
		if !slices.ContainsFunc(areas, func(o rules.Area) bool {
			return o.Name < a.Name && slices.ContainsFunc(o.Paths, func(q string) bool {
				l, g := atWildcard(q)
				return slices.Equal(l, literal) && slices.Equal(g, glob)
			})
		}) {
			paths = append(paths, p)
		}
	}
	return paths
}

func atWildcard(p string) ([]string, []string) {
	names := strings.Split(path.Clean(p), "/")
	i := slices.IndexFunc(names, func(name string) bool { return strings.ContainsAny(name, `*?[\`) })
	if i < 0 {
		i = len(names)
	}
	return names[:i], names[i:]
}
