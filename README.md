# specrig-go

The Go side of [specrig](https://github.com/specrig/protocol): what a Go stack
needs to agree with the protocol.

| | what |
|---|---|
| [agents](agents) | `specrig.md`, agents/1: the parts every stack shares with a stack's slots filled, the rows of its mode and area tables, and the verdict on whether `AGENTS.md` brings it in |
| [rules](rules) | rules/1: the modes and classes of the protocol's table, a service's areas with its marker's merged in and checked, and the document a stack's tool prints for the plugin |
| [cli](cli) | the commands every stack's tool answers — `rules`, and `agents`, which writes `specrig.md` and fails while `AGENTS.md` does not bring it in — with what differs by framework in a `Preset` |

A framework's adapter hands `cli.Main` its `Preset`: the layout, the default areas,
and the slots and sections of `specrig.md` that are its own. The core fills the
rest and reads the service's `specrig.yaml`. The adapter for waffle, the Go
framework, is not written yet; until it is, waffle keeps its own copies.

The Claude Code plugin that enforces the modes for every stack is
[specrig](https://github.com/specrig/specrig).

## Development

```bash
make test
```

runs the packages against the copy of the protocol in `internal/protocol` — its
data embedded, its agents and markers fixtures read by the tests.

```bash
make protocol
```

refreshes that copy from a checkout of the protocol at `PROTOCOL`, `../protocol` by
default.
