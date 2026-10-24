# specrig-go

The Go side of [specrig](https://github.com/specrig/protocol): what a Go stack
needs to agree with the protocol.

| | what |
|---|---|
| [agents](agents) | `specrig.md`, agents/1: the parts every stack shares with a stack's slots filled, the rows of its mode and area tables, and the verdict on whether `AGENTS.md` brings it in |

waffle, the Go framework, keeps its own harness, because it generates it from its
own container. Its `specrig.md` is meant to come from this package; until waffle
takes it, it keeps its own copies.

The Claude Code plugin that enforces the modes for every stack is
[specrig](https://github.com/specrig/specrig).

## Development

```bash
make test
```

runs the packages against the copy of the protocol in `internal/protocol` — its
data embedded, its agents fixtures read by the tests.

```bash
make protocol
```

refreshes that copy from a checkout of the protocol at `PROTOCOL`, `../protocol` by
default.
