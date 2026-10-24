package replacements

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/specrig/specrig-go/waffle/internal/command"
	check "github.com/specrig/specrig-go/waffle/replacements"
	waffle "gitlab.com/gorib/waffle/container"
)

func New() *cmd {
	return &cmd{command.New("replacements",
		"Report every seam the containers expose and what specrig.yaml allows; check refuses the rest").
		WithUsage("[check] [--container=<path>]\n" +
			"  check        fail if a test replaces a seam replacements.allow does not grant\n" +
			"  --container  read this generated container instead of the ones //go:generate names")}
}

type cmd struct {
	*command.Command
}

func (c *cmd) Run(_ context.Context, args ...string) error {
	containers, args, err := command.TakeValues(args, "--container", "a generated container's path")
	if err != nil {
		return err
	}

	if err := c.Operands(args, 0, 1); err != nil {
		return err
	}
	// The comparison also runs inside `go test`, so the CLI's own default is the
	// report a person came for, not a second copy of the pass/fail.
	verify := len(args) == 1
	if verify && args[0] != "check" {
		return fmt.Errorf("unknown subcommand %q\n%s", args[0], c.Usage())
	}

	p, err := command.OpenProject()
	if err != nil {
		return err
	}
	cfg, err := check.LoadConfig(p.Root())
	if err != nil {
		return err
	}
	read, err := targets(p.Root(), containers)
	if err != nil {
		return err
	}
	found, err := check.Inspect(p.Root(), read)
	if err != nil {
		return err
	}

	if !verify {
		fmt.Print(suggestion(found.Seams, cfg, p.Module()))
		return nil
	}
	return report(check.Verify(p.Root(), found, cfg))
}

// A named container is taken at its word — that is what the flag is for.
func targets(root string, overrides []string) ([]waffle.Container, error) {
	if len(overrides) == 0 {
		return waffle.DiscoverContainers(root)
	}
	out := make([]waffle.Container, 0, len(overrides))
	for _, file := range overrides {
		// A path a person typed is relative to where they typed it; the check
		// works from the module root, which is not always the same place.
		if !filepath.IsAbs(file) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("failed to get current working directory: %w", err)
			}
			file = filepath.Join(cwd, file)
		}
		dir := filepath.Dir(file)
		out = append(out, waffle.Container{File: file, Dir: dir, Package: filepath.Base(dir)})
	}
	return out, nil
}
