// Package command is what the adapter's own commands share: the module they run
// in, the templates they write from, and how they say what they did.
package command

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/specrig/specrig-go/waffle/internal/gomod"
)

func New(name, description string) *Command {
	return &Command{name: name, description: description}
}

type Command struct {
	name        string
	description string
	operands    string
}

// One string, so a bad invocation and --help cannot disagree.
func (c *Command) WithUsage(operands string) *Command {
	c.operands = operands
	return c
}

func (c *Command) Code() string { return c.name }

func (c *Command) Description() string { return c.description }

// Synopsis is the operands' first line, as a list of commands shows them.
func (c *Command) Synopsis() string {
	first, _, _ := strings.Cut(c.operands, "\n")
	return first
}

func (c *Command) Usage() string {
	if c.operands == "" {
		return "usage: go tool specrig " + c.name
	}
	return "usage: go tool specrig " + c.name + " " + c.operands
}

// An extra operand is refused rather than dropped, and so is a flag.
func (c *Command) Operands(args []string, min, max int) error {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag %q\n%s", a, c.Usage())
		}
	}
	if len(args) < min {
		return errors.New(c.Usage())
	}
	if len(args) > max {
		return fmt.Errorf("unexpected operand %q\n%s", args[max], c.Usage())
	}
	return nil
}

// Called before Operands, which would count a flag and its value as operands.
func TakeValues(args []string, flag, takes string) (values, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		var value string
		switch joined, found := strings.CutPrefix(args[i], flag+"="); {
		case found:
			value = joined
		case args[i] != flag:
			rest = append(rest, args[i])
			continue
		case i+1 < len(args):
			i++
			value = args[i]
		}
		if value == "" {
			return nil, nil, fmt.Errorf("%s takes %s", flag, takes)
		}
		values = append(values, value)
	}
	return values, rest, nil
}

// Every path a Project takes or reports is relative to the module root, not to
// the working directory.
type Project struct {
	root   string
	module string
}

func OpenProject() (*Project, error) {
	root, err := gomod.Root()
	if err != nil {
		return nil, err
	}
	module, err := gomod.Path(root)
	if err != nil {
		return nil, err
	}
	return &Project{root: root, module: module}, nil
}

func (p *Project) Root() string { return p.root }

func (p *Project) Module() string { return p.module }

func (p *Project) Path(elem ...string) string {
	return filepath.Join(append([]string{p.root}, elem...)...)
}

type Templates struct{ files embed.FS }

func NewTemplates(files embed.FS) Templates { return Templates{files: files} }

func (t Templates) Render(source string, params any) ([]byte, error) {
	tpl, err := template.New(filepath.Base(source)).ParseFS(t.files, source)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", source, err)
	}
	var out bytes.Buffer
	if err = tpl.Execute(&out, params); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Write scaffolds: a target that exists is left alone, and said to be.
func (p *Project) Write(templates Templates, sources map[string]string, params any) error {
	type pair struct{ source, target string }

	pairs := make([]pair, 0, len(sources))
	for source, target := range sources {
		pairs = append(pairs, pair{source, target})
	}
	// Map order would shuffle the report between runs.
	slices.SortFunc(pairs, func(a, b pair) int { return strings.Compare(a.target, b.target) })

	for _, pair := range pairs {
		wrote, err := p.write(templates, pair.source, pair.target, params)
		if err != nil {
			return err
		}
		if wrote {
			report("+", pair.target)
		} else {
			report("=", pair.target+" — exists, left alone")
		}
	}
	return nil
}

func (p *Project) write(templates Templates, source, target string, params any) (created bool, err error) {
	path := p.Path(target)
	switch _, err := os.Stat(path); {
	case err == nil:
		return false, nil
	case !errors.Is(err, os.ErrNotExist):
		return false, fmt.Errorf("failed to stat %s: %w", target, err)
	}

	out, err := templates.Render(source, params)
	if err != nil {
		return false, err
	}
	if filepath.Ext(target) == ".go" {
		if out, err = format.Source(out); err != nil {
			return false, fmt.Errorf("%s renders invalid Go: %w", source, err)
		}
	}
	if err = os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return false, fmt.Errorf("failed to create directory %s: %w", filepath.Dir(target), err)
	}
	if err = os.WriteFile(path, out, 0o644); err != nil {
		return false, fmt.Errorf("failed to create %s: %w", target, err)
	}
	return true, nil
}

// For a file that is regenerated rather than scaffolded: one already holding body
// is not touched, so a run that changes nothing leaves no trace on disk.
func WriteIfChanged(path string, body []byte) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, body) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

var output io.Writer = os.Stderr

func report(mark, text string) { _, _ = fmt.Fprintf(output, "  %s %s\n", mark, text) }

func ReportAction(text string) { report("+", text) }

// For a file that is regenerated rather than scaffolded: compared and found current.
func ReportUnchanged(text string) { report("=", text+" — up to date") }

// Not an error: the run succeeded, and this is about something it deliberately
// did not touch.
func ReportWarning(text string) { report("!", text) }
