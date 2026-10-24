// Package rules is guard/1's rules/1: the modes and classes of the protocol's table,
// a service's areas, and the document a stack's tool prints for the plugin.
package rules

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/specrig/specrig-go/internal/protocol"
	"gopkg.in/yaml.v3"
)

type Mode struct {
	Name     string   `yaml:"name" json:"name"`
	Opens    []string `yaml:"opens" json:"opens"`
	Includes []string `yaml:"includes" json:"includes,omitempty"`
	Offered  *bool    `yaml:"offered" json:"offered,omitempty"`
	Allows   string   `yaml:"allows" json:"allows"`
	Brief    string   `yaml:"brief" json:"brief"`
}

type Class struct {
	Name     string   `yaml:"name" json:"name"`
	What     string   `yaml:"what" json:"-"`
	Patterns []string `yaml:"patterns" json:"patterns"`
}

// Layout is the patterns a stack fills the classes with, by class.
type Layout map[string][]string

// Area is a named set of paths. Nil Modes leaves the class table in charge, so a
// grant only adds; empty Modes closes the paths in every mode.
type Area struct {
	Name  string
	Paths []string
	Modes []string
	Grant []string
}

func (a Area) MarshalJSON() ([]byte, error) {
	type set struct {
		Name  string    `json:"name"`
		Paths []string  `json:"paths"`
		Modes *[]string `json:"modes,omitempty"`
		Grant *[]string `json:"grant,omitempty"`
	}
	s := set{Name: a.Name, Paths: a.Paths}
	if a.Modes != nil {
		s.Modes = &a.Modes
	}
	if a.Grant != nil {
		s.Grant = &a.Grant
	}
	return json.Marshal(s)
}

var classes, modes = load()

func load() ([]Class, []Mode) {
	body, err := protocol.Files.ReadFile("modes.yaml")
	if err != nil {
		panic(err)
	}
	var t struct {
		Specrig string  `yaml:"specrig"`
		Classes []Class `yaml:"classes"`
		Modes   []Mode  `yaml:"modes"`
	}
	if err := yaml.Unmarshal(body, &t); err != nil {
		panic(err)
	}
	if t.Specrig != "modes/1" {
		panic("modes.yaml is " + t.Specrig + ", and this package reads modes/1")
	}
	return t.Classes, t.Modes
}

// Modes are the table's, in its order.
func Modes() []Mode { return slices.Clone(modes) }

func Lookup(name string) (Mode, error) {
	names := make([]string, 0, len(modes))
	for _, m := range modes {
		if m.Name == name {
			return m, nil
		}
		names = append(names, m.Name)
	}
	return Mode{}, fmt.Errorf("unknown mode %q (have: %s)", name, strings.Join(names, ", "))
}

// Allows is what the mode allows, with the cases' patterns in place of {{cases}}.
func Allows(m Mode, cases []string) string {
	return strings.ReplaceAll(m.Allows, "{{cases}}", "`"+strings.Join(cases, "`, `")+"`")
}

// Areas are the defaults with the marker's agents.areas merged in: an area the
// marker names replaces the default of that name.
func Areas(defaults []Area, marker []byte) ([]Area, error) {
	byName := map[string]Area{}
	for _, a := range defaults {
		byName[a.Name] = a
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(marker, &doc); err != nil {
		return nil, err
	}
	if declared := field(field(&doc, "agents"), "areas"); declared != nil && declared.Tag != "!!null" {
		if declared.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("agents.areas is not a mapping of areas by name")
		}
		for i := 0; i+1 < len(declared.Content); i += 2 {
			a, err := area(declared.Content[i].Value, resolved(declared.Content[i+1]))
			if err != nil {
				return nil, err
			}
			byName[a.Name] = a
		}
	}
	return slices.SortedFunc(maps.Values(byName), func(a, b Area) int { return strings.Compare(a.Name, b.Name) }), nil
}

func area(name string, n *yaml.Node) (Area, error) {
	if n == nil || n.Kind != yaml.MappingNode {
		return Area{}, fmt.Errorf("agents.areas.%s is not a mapping of paths, modes and grant", name)
	}
	paths := field(n, "paths")
	if paths == nil || (paths.Kind == yaml.SequenceNode && len(paths.Content) == 0) {
		return Area{}, fmt.Errorf("agents.areas.%s names no paths", name)
	}
	a := Area{Name: name}
	var ok bool
	if a.Paths, ok = strs(paths); !ok {
		return Area{}, fmt.Errorf("agents.areas.%s.paths is not a list of paths", name)
	}
	for _, list := range []struct {
		key  string
		into *[]string
	}{{"modes", &a.Modes}, {"grant", &a.Grant}} {
		if given := field(n, list.key); given != nil {
			if *list.into, ok = strs(given); !ok {
				return Area{}, fmt.Errorf("agents.areas.%s.%s is not a list of modes", name, list.key)
			}
		}
	}
	for _, m := range slices.Concat(a.Modes, a.Grant) {
		if _, err := Lookup(m); err != nil {
			return Area{}, fmt.Errorf("agents.areas.%s: %w", name, err)
		}
	}
	return a, nil
}

func resolved(n *yaml.Node) *yaml.Node {
	for n != nil && (n.Kind == yaml.DocumentNode || n.Kind == yaml.AliasNode) {
		switch {
		case n.Kind == yaml.AliasNode:
			n = n.Alias
		case len(n.Content) > 0:
			n = n.Content[0]
		default:
			return nil
		}
	}
	return n
}

func field(n *yaml.Node, key string) *yaml.Node {
	if n = resolved(n); n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return resolved(n.Content[i+1])
		}
	}
	return nil
}

func strs(n *yaml.Node) ([]string, bool) {
	if n.Kind != yaml.SequenceNode {
		return nil, false
	}
	out := []string{}
	for _, item := range n.Content {
		if item = resolved(item); item == nil || item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			return nil, false
		}
		out = append(out, item.Value)
	}
	return out, true
}

type Document struct {
	Specrig string  `json:"specrig"`
	Modes   []Mode  `json:"modes"`
	Classes []Class `json:"classes"`
	Areas   []Area  `json:"areas"`
}

// Of is rules/1 for a stack's layout and a service's areas.
func Of(layout Layout, areas []Area) (Document, error) {
	for name := range layout {
		if !slices.ContainsFunc(classes, func(c Class) bool { return c.Name == name }) {
			return Document{}, fmt.Errorf("the layout fills no class %q", name)
		}
	}
	d := Document{Specrig: "rules/1", Areas: areas}
	for _, m := range modes {
		m.Allows = Allows(m, layout["cases"])
		d.Modes = append(d.Modes, m)
	}
	for _, c := range classes {
		c.Patterns = append(append([]string{}, c.Patterns...), layout[c.Name]...)
		d.Classes = append(d.Classes, c)
	}
	return d, nil
}
