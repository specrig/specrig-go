package records

import (
	"context"

	"github.com/specrig/specrig-go/surface"
	"github.com/specrig/specrig-go/waffle/extract"
	"github.com/specrig/specrig-go/waffle/internal/command"
)

func NewSurface() *surfaceCmd {
	return &surfaceCmd{command.New("surface",
		"Write tests/surface.yaml from this module's code: what the service publishes, consumes, serves and calls")}
}

type surfaceCmd struct {
	*command.Command
}

func (c *surfaceCmd) Run(_ context.Context, args ...string) error {
	if err := c.Operands(args, 0, 0); err != nil {
		return err
	}
	p, err := command.OpenProject()
	if err != nil {
		return err
	}
	s, err := extract.Extract(p.Root())
	if err != nil {
		return err
	}
	if changed, err := command.WriteIfChanged(p.Path(surface.File), s.Encode()); err != nil || !changed {
		return err
	}
	command.ReportAction("wrote " + surface.File)
	return nil
}
