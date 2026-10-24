// Package cli is the core of a Go stack's tool: the commands every stack answers,
// with what differs by framework in a Preset.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/specrig/specrig-go/agents"
	"github.com/specrig/specrig-go/rules"
	"gopkg.in/yaml.v3"
)

// Preset is what a framework's adapter hands the core.
type Preset struct {
	// Tool is how a service runs the adapter: `go tool specrig`.
	Tool string
	// Version is the adapter with its version, as specrig.md names what wrote it.
	Version string
	Layout  rules.Layout
	// Areas are the stack's defaults for the service in dir.
	Areas func(dir string) ([]rules.Area, error)
	// Slots are agents/1's, but for those the core fills.
	Slots map[string]string
	// Own are the sections of specrig.md after Rule #11.
	Own []string
	// Commands are the adapter's own, beside init, rules and agents.
	Commands []Command
	// Init lays out what the stack's suite needs, after init's own files, where
	// the process works.
	Init func() error
}

// Command is one of an adapter's own. It works where the process does: Run's
// dir is for rules and agents.
type Command struct {
	Name string
	// Operands follow the name in the usage.
	Operands    string
	Description string
	Run         func(args []string) error
}

const marker = "specrig.yaml"

var core = []string{"writer", "version", "modes", "areas", "platform-files", "config"}

func usage(p Preset) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%[1]s init\n%[1]s agents\n%[1]s rules\n", p.Tool)
	for _, c := range p.Commands {
		fmt.Fprintln(&b, strings.TrimSpace(p.Tool+" "+c.Name+" "+c.Operands))
	}
	fmt.Fprintf(&b, `
init lays the service out for specrig: %[1]s naming this tool as its guard,
an AGENTS.md that brings specrig.md in, specrig.md, CLAUDE.md as a link to
AGENTS.md, and what the suite needs. No file that exists is rewritten but
specrig.md, and an AGENTS.md lacking the line, which gets it at its end.

agents writes specrig.md, Rule #11 for this service with the areas %[1]s
declares, and fails while AGENTS.md does not bring it in. rules prints the Rule #11
modes, this service's layout and its areas as JSON, for the plugin that enforces
the modes. The areas are the defaults and those %[1]s declares under
agents.areas.`, marker)
	for _, c := range p.Commands {
		fmt.Fprintf(&b, "\n\n%s: %s", c.Name, c.Description)
	}
	return b.String()
}

// Main runs the command args name in the working directory, and is the exit code.
func Main(args []string, p Preset) int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return Run(dir, args, p, os.Stdout, os.Stderr)
}

// Run is Main for the service in dir, writing to stdout and stderr.
func Run(dir string, args []string, p Preset, stdout, stderr io.Writer) int {
	commands := map[string]func(string, Preset, io.Writer) error{"init": initService, "agents": writeAgents, "rules": printRules}
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage(p))
		return 1
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, usage(p))
		return 0
	}
	command, ok := commands[args[0]]
	if own := slices.IndexFunc(p.Commands, func(c Command) bool { return c.Name == args[0] }); !ok && own >= 0 {
		if err := p.Commands[own].Run(args[1:]); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}
	if !ok {
		fmt.Fprintf(stderr, "Unknown command %q\n\n%s\n", args[0], usage(p))
		return 1
	}
	if len(args) > 1 {
		fmt.Fprintf(stderr, "Error: %s takes no arguments\n\n%s\n", args[0], usage(p))
		return 1
	}
	if err := command(dir, p, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func areas(dir string, p Preset) ([]rules.Area, error) {
	defaults, err := p.Areas(dir)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(filepath.Join(dir, marker))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	merged, err := rules.Areas(defaults, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", marker, err)
	}
	return merged, nil
}

func printRules(dir string, p Preset, out io.Writer) error {
	merged, err := areas(dir, p)
	if err != nil {
		return err
	}
	doc, err := rules.Of(p.Layout, merged)
	if err != nil {
		return err
	}
	line, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s\n", line)
	return err
}

func writeAgents(dir string, p Preset, out io.Writer) error {
	reference, err := agents.Reference(p.Slots["what"])
	if err != nil {
		return err
	}
	text, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("there is no AGENTS.md here: add one with the line\n\n%s\n\nthat brings specrig.md in", reference)
	}
	if err != nil {
		return err
	}
	written, err := writeSpecrigMd(dir, p)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, written)
	agentsMd := string(text)
	switch agents.Verdict(&agentsMd) {
	case "stale":
		return fmt.Errorf("AGENTS.md still carries Rule #11, which specrig.md holds now: keep the %s's own sections in it and delete the rest", p.Slots["what"])
	case "unreferenced":
		return fmt.Errorf("AGENTS.md does not bring specrig.md in: add the line\n\n%s\n\nwhere an agent reads it before the first edit", reference)
	}
	return nil
}

func initService(dir string, p Preset, out io.Writer) error {
	reference, err := agents.Reference(p.Slots["what"])
	if err != nil {
		return err
	}
	for _, step := range []func() (string, error){
		func() (string, error) { return writeMarker(dir, p) },
		func() (string, error) { return bringIn(dir, p, reference) },
		func() (string, error) { return writeSpecrigMd(dir, p) },
		func() (string, error) { return linkClaudeMd(dir) },
	} {
		line, err := step()
		if err != nil {
			return err
		}
		if line != "" {
			fmt.Fprintln(out, line)
		}
	}
	if p.Init == nil {
		return nil
	}
	return p.Init()
}

func writeMarker(dir string, p Preset) (string, error) {
	guard := "guard: [" + strings.Join(strings.Fields(p.Tool), ", ") + "]"
	file := filepath.Join(dir, marker)
	body, err := os.ReadFile(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "+ " + marker, os.WriteFile(file, []byte(guard+"\n"), 0o644)
	case err != nil:
		return "", err
	}
	var named struct {
		Guard any `yaml:"guard"`
	}
	if err := yaml.Unmarshal(body, &named); err != nil {
		return "", fmt.Errorf("%s: %w", marker, err)
	}
	if named.Guard == nil {
		return fmt.Sprintf("= %s — kept; it names no guard: add `%s` for the plugin to ask this tool", marker, guard), nil
	}
	return "= " + marker + " — kept", nil
}

func bringIn(dir string, p Preset, reference string) (string, error) {
	file := filepath.Join(dir, "AGENTS.md")
	text, err := os.ReadFile(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		_, parent := os.Stat(filepath.Join(dir, "..", "AGENTS.md"))
		return "+ AGENTS.md", os.WriteFile(file, []byte(stub(filepath.Base(dir), p.Slots["what"], reference, parent == nil)), 0o644)
	case err != nil:
		return "", err
	}
	agentsMd := string(text)
	left := ""
	if agents.Verdict(&agentsMd) == "stale" {
		left = fmt.Sprintf("; it still carries Rule #11, which specrig.md holds now: keep the %s's own sections in it and delete the rest", p.Slots["what"])
	}
	if agents.References(agentsMd) {
		return "= AGENTS.md — kept" + left, nil
	}
	gap := "\n\n"
	switch {
	case agentsMd == "":
		gap = ""
	case strings.HasSuffix(agentsMd, "\n"):
		gap = "\n"
	}
	return "~ AGENTS.md: the line that brings specrig.md in, at its end" + left,
		os.WriteFile(file, []byte(agentsMd+gap+reference+"\n"), 0o644)
}

func stub(name, what, reference string, parent bool) string {
	sections := []string{"# AGENTS.md", "Instructions for AI coding agents working in " + name + ".", reference}
	if parent {
		sections = append(sections, "The repository's rules in `../AGENTS.md` hold here too — ownership, the\n"+
			"notation for specifications, a neighbour's rules are not reproduced.")
	}
	sections = append(sections, "## This "+what, "<!-- What "+name+" is for, who calls it, which neighbours it calls and what it\n"+
		"publishes. Written by the person who owns the platform. -->")
	return strings.Join(sections, "\n\n") + "\n"
}

func linkClaudeMd(dir string) (string, error) {
	link := filepath.Join(dir, "CLAUDE.md")
	if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return "+ CLAUDE.md → AGENTS.md", os.Symlink("AGENTS.md", link)
}

func writeSpecrigMd(dir string, p Preset) (string, error) {
	for _, name := range core {
		if _, ok := p.Slots[name]; ok {
			return "", fmt.Errorf("the preset fills the slot %s, which the core fills", name)
		}
	}
	merged, err := areas(dir, p)
	if err != nil {
		return "", err
	}
	slots := map[string]string{
		"writer":         "`" + p.Tool + " agents`",
		"version":        p.Version,
		"modes":          agents.ModeRows(p.Layout["cases"]),
		"areas":          agents.AreaRows(merged),
		"platform-files": listing(p.Layout["platform"]),
		"config":         "`" + marker + "`",
	}
	maps.Copy(slots, p.Slots)
	body, err := agents.SpecrigMd(slots, p.Own)
	if err != nil {
		return "", err
	}
	file := filepath.Join(dir, "specrig.md")
	before, err := os.ReadFile(file)
	existed := err == nil
	switch {
	case existed && string(before) == body:
		return "= specrig.md", nil
	case !existed && !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		return "", err
	}
	if existed {
		return "~ specrig.md", nil
	}
	return "+ specrig.md", nil
}

func listing(paths []string) string {
	ticked := make([]string, 0, len(paths))
	for _, p := range paths {
		ticked = append(ticked, "`"+p+"`")
	}
	if len(ticked) < 2 {
		return strings.Join(ticked, "")
	}
	return strings.Join(ticked[:len(ticked)-1], ", ") + " and " + ticked[len(ticked)-1]
}

// Version is the version the running binary was built with of the module at
// path, "" where it holds none.
func Version(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, m := range slices.Concat([]*debug.Module{&info.Main}, info.Deps) {
		if m.Path == path {
			if m.Replace != nil {
				return m.Replace.Version
			}
			return m.Version
		}
	}
	return ""
}
