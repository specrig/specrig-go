package trace

import (
	"context"

	"gitlab.com/gorib/waffle/app"

	record "github.com/specrig/specrig-go/trace"
)

// A daemon serves inputs rather than being one, and keeps its own type.
func Command(c app.Command) app.Command {
	if _, daemon := c.(app.Daemon); daemon || c == nil {
		return c
	}
	return command{c}
}

type command struct{ app.Command }

func (c command) Run(ctx context.Context) error {
	if !record.On() {
		return c.Command.Run(ctx)
	}
	began := record.Begin()
	err := c.Command.Run(ctx)
	r := record.Record{Seq: began, Dir: "in", Kind: "command", Name: c.Name()}
	if err != nil {
		r.Error = refusal(err)
	}
	record.Write(r)
	return err
}
