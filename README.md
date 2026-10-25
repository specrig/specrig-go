# specrig-go

The Go side of [specrig](https://github.com/specrig/protocol): what a Go stack
needs to agree with the protocol, and the adapter that brings it to a service built
on waffle.

| | what |
|---|---|
| [agents](agents) | `specrig.md`, agents/1: the parts every stack shares with a stack's slots filled, the rows of its mode and area tables, and the verdict on whether `AGENTS.md` brings it in |
| [rules](rules) | rules/1: the modes and classes of the protocol's table, a service's areas with its marker's merged in and checked, and the document a stack's tool prints for the plugin |
| [cli](cli) | the commands every stack's tool answers — `init`, which lays a service out for specrig; `rules`; and `agents`, which writes `specrig.md` and fails while `AGENTS.md` does not bring it in — with what differs by framework in a `Preset`, an adapter's own commands among it |
| [surface](surface) | the `surface/1` record, and the check of a set of them against each other |
| [trace](trace) | what crossed a service's edge during a run, the records `flows/1` is written from |
| [flows](flows) | `flows/1`: a suite's record written from its trace, and `index/1`: a project's flows assembled from its services' records |
| [cmd/specrig-flows](cmd/specrig-flows) | the command that assembles them, for a project on any stack |
| [resource](resource) | the seam between a component test and the external systems it runs against for real |
| [waffle](waffle) | the adapter for a service built on waffle, the Go framework, in a module of its own: the layout, the default areas and the text of `specrig.md`, the component harness generated from the container, what a test may replace, the surface read off the code, and the trace over gorib's transports |

A framework's adapter hands `cli.Main` its `Preset`: the layout, the default areas,
the slots and sections of `specrig.md` that are its own, and its own commands. The
core fills the rest and reads the service's `specrig.yaml`.

A waffle service takes the adapter as a tool, once:

```bash
go get -tool github.com/specrig/specrig-go/waffle/cmd/specrig
go tool specrig init
go mod tidy
```

`init` writes `specrig.yaml` naming the tool as its guard, brings `specrig.md`
into `AGENTS.md`, and scaffolds the harness. After that the service runs
`go tool specrig agents`, `rules`, `harness`, `replacements` and `surface`. What each writes and why is under [waffle/docs](waffle/docs):
[the harness](waffle/docs/harness.md), [the first component test](waffle/docs/first-component-test.md),
[the commands](waffle/docs/commands.md), [flows](waffle/docs/flows.md) and
[the approach](waffle/docs/ai-native.md).

The Claude Code plugin that enforces the modes for every stack is
[specrig](https://github.com/specrig/specrig).

## A project's flows

```bash
specrig-flows <root> <index.yaml>
```

reads the records of every service below `root` — Go, PHP, .NET or a web app — as
one system, and writes the index of its flows and a page per flow beside it, as the
protocol's [system.md](https://github.com/specrig/protocol/blob/main/system.md)
says. Each [release](https://github.com/specrig/specrig-go/releases) carries it for
macOS, Linux and Windows on amd64 and arm64, with `checksums.txt`:

```bash
curl -fsSLO https://github.com/specrig/specrig-go/releases/latest/download/specrig-flows-linux-amd64
```

```bash
curl -fsSLO https://github.com/specrig/specrig-go/releases/latest/download/checksums.txt
```

```bash
shasum -a 256 --check --ignore-missing checksums.txt
```

A Go module takes it as a tool instead:

```bash
go get -tool github.com/specrig/specrig-go/cmd/specrig-flows
```

and runs `go tool specrig-flows`.

## Development

```bash
make test
```

runs the packages, and the adapter's, against the copy of the protocol in
`internal/protocol` — its data embedded, its agents, markers, records and flows
fixtures read by the tests.

```bash
make release
```

builds `specrig-flows` for every platform into `dist/`, with `checksums.txt`; a tag
`v<version>` pushed does it in CI and attaches them to the release.

```bash
make protocol
```

refreshes that copy from a checkout of the protocol at `PROTOCOL`, `../protocol` by
default.
