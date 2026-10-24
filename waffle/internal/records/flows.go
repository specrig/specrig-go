package records

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/specrig/specrig-go/flows"
	"github.com/specrig/specrig-go/waffle/internal/command"
)

func NewFlows() *flowsCmd {
	return &flowsCmd{base: command.New("flows",
		"Assemble a project's flows from its services' records and check the services against each other").
		WithUsage("<root> <index.yaml>\n" +
			"  root is searched for services the way ./... is, and taken as the whole system; index.yaml is\n" +
			"  the project's index, and the pages are written beside it. Fails on prose that lost its flow and on\n" +
			"  what the services' records leave unclosed")}
}

type flowsCmd struct {
	base *command.Command
}

func (c *flowsCmd) Code() string        { return c.base.Code() }
func (c *flowsCmd) Description() string { return c.base.Description() }
func (c *flowsCmd) Usage() string       { return c.base.Usage() }
func (c *flowsCmd) Synopsis() string    { return c.base.Synopsis() }

func (c *flowsCmd) Run(_ context.Context, args ...string) error {
	if err := c.base.Operands(args, 2, 2); err != nil {
		return err
	}
	root, index := strings.TrimSuffix(args[0], "..."), args[1]
	files, foreign, err := flows.Assemble(root, index)
	if err != nil {
		return err
	}
	for _, path := range foreign {
		command.ReportWarning("skipped " + path + ": it is not a specrig record")
	}
	stale := stalePages(filepath.Dir(index), files)
	unclosed := unclosed(files[index])
	if current, err := os.ReadFile(index); err == nil {
		for _, change := range flows.LinkChanges(current, files[index]) {
			command.ReportWarning(change)
		}
	}

	written := 0
	for _, path := range slices.Sorted(maps.Keys(files)) {
		changed, err := command.WriteIfChanged(path, files[path])
		if err != nil {
			return err
		}
		if changed {
			written++
		}
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil {
			return err
		}
		command.ReportAction("removed " + path)
	}
	if summary := fmt.Sprintf("%s and %d pages", index, len(files)-1); written == 0 {
		command.ReportUnchanged(summary)
	} else {
		command.ReportAction(fmt.Sprintf("wrote %d of %s", written, summary))
	}
	if orphaned(files[index]) {
		return fmt.Errorf("%s: some prose lost its flow — see orphaned", index)
	}
	if unclosed != "" {
		return fmt.Errorf("the services' records do not close — see system in %s:%s", index, unclosed)
	}
	return nil
}

// What in the index's system section fails: the records' findings, not what
// they only tell.
func unclosed(index []byte) string {
	var i flows.Index
	if yaml.Unmarshal(index, &i) != nil || i.System == nil || !i.System.Failed() {
		return ""
	}
	failing := *i.System
	failing.PatternOnly, failing.Loops, failing.Outside = nil, nil, nil
	return "\n" + failing.String()
}

// A page this command wrote before and would not write now.
func stalePages(dir string, files map[string][]byte) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() || filepath.Ext(path) != ".md" || files[path] != nil {
			continue
		}
		if body, err := os.ReadFile(path); err == nil && flows.Generated(body) {
			out = append(out, path)
		}
	}
	return out
}

func orphaned(index []byte) bool { return bytes.Contains(index, []byte("\norphaned:")) }
