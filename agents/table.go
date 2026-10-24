package agents

import (
	"path"
	"slices"
	"strings"

	"github.com/specrig/specrig-go/internal/protocol"
	"gopkg.in/yaml.v3"
)

type Mode struct {
	Name   string `yaml:"name"`
	Allows string `yaml:"allows"`
}

var modes = load()

func load() []Mode {
	body, err := protocol.Files.ReadFile("modes.yaml")
	if err != nil {
		panic(err)
	}
	var t struct {
		Specrig string `yaml:"specrig"`
		Modes   []Mode `yaml:"modes"`
	}
	if err := yaml.Unmarshal(body, &t); err != nil {
		panic(err)
	}
	if t.Specrig != "modes/1" {
		panic("modes.yaml is " + t.Specrig + ", and this package reads modes/1")
	}
	return t.Modes
}

// Modes are the table's, in its order.
func Modes() []Mode { return slices.Clone(modes) }

// Area is a named set of paths. Nil Modes leaves the class table in charge, so a
// grant only adds; empty Modes closes the paths in every mode.
type Area struct {
	Name  string   `yaml:"name"`
	Paths []string `yaml:"paths"`
	Modes []string `yaml:"modes"`
	Grant []string `yaml:"grant"`
}

func claimed(areas []Area, a Area) []string {
	var paths []string
	for _, p := range a.Paths {
		literal, glob := atWildcard(p)
		if !slices.ContainsFunc(areas, func(o Area) bool {
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
