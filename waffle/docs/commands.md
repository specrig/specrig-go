# The adapter's commands

How `go tool specrig init`, `harness` and `replacements` lay out and read a waffle
service. `harness` and `replacements` work from the module root or from anywhere
below it.

## go tool specrig init

`go tool specrig init` takes no operands and runs once, in the service's
directory, after `waffle init:service`:

```
+ specrig.yaml
~ AGENTS.md: the line that brings specrig.md in, at its end
+ specrig.md
  + config/diogen/replacements_test.go
  + tests/boot_test.go
  + tests/internal/harness/harness.go
  + tests/suite_test.go
  ! the suite imports what go.mod may not require yet: run go mod tidy
  + generated tests/internal/harness/harness_gen.go
```

`specrig.yaml` gets `guard: [go, tool, specrig]`, which is how the specrig plugin
finds the service's rules; without it the plugin checks no edit there. `AGENTS.md`
gets the line at its end, a stub where there is none, and `CLAUDE.md` a link to it
where there is none. Then it is `go tool specrig harness`'s first run. No file
that exists is rewritten but `specrig.md`, so it is safe to run again; one that
is kept is said to be, with what it lacks.

`specrig.md` and `tests/surface.yaml` go stale as the service grows: put
`go tool specrig agents` and `go tool specrig surface` beside `./waffle agents`
in whatever regenerates the service.

## go tool specrig harness

`go tool specrig harness` takes no operands. Every run rewrites
`tests/internal/harness/harness_gen.go` from the container, and the first run —
the one that finds no `harness.go` beside it — also scaffolds the suite. It
works from the module root or from anywhere below it, so the `go:generate` line
in `harness.go` runs it after diogen on every `make generate`. Which files those
are, and what goes in the one you write, is
[conventions/component-harness](harness.md#the-generated-file-and-yours).

Everything in the generated file comes from the container rather than from a
flag, because a flag would close a set the container already knows:

- **the systems.** A seam of type `gitlab.com/gorib/sql.Db` means postgres, and
  the harness gets `sqltest` plus every `domain/*/cmd/migrate` it finds, in name
  order; a `gitlab.com/gorib/redis.Db` seam means `redistest`. Without either —
  an API gateway, a proxy — the set is `resource.New()`, whose `Up`, `Reset` and
  `Down` do nothing.
- **the transports.** `NewGrpcCommand` in the container gives an in-process gRPC
  server, `NewHttpCommand` gives `waffleHttp.Serve` over the container's own mux,
  global chain and router collection, and a service that has both gets both
  behind one `Clients`. A container still passing a single `server.Router`, or
  none of the `http.Global` the chain now arrives as, is refused with the fix
  rather than read as a service that serves no HTTP.
- **the handlers, their clients and the interceptor order**, read off the
  daemon's own construction call; a handler's `Start` names the service it
  registers, and the typed client is the constructor protoc-gen-go-grpc puts
  beside it.
- **the dead connections.** Every `grpc.ClientConnInterface` seam becomes
  `grpcClient.Dead(...)`, labelled with the clients built on it and the
  variables it reads.
- **the environment.** A typed field of `Variables` for every variable read
  through `gitlab.com/gorib/env`, unless `Boot` replaces the constructor that
  reads it: a placeholder where some read has no fallback, the fallback otherwise.

A handler whose registration it cannot find is reported by name rather than
guessed at.

## go tool specrig replacements

What a test can replace are the fields of the generated `Replacements` struct, so
that struct is what this command reads. The unit is the interface, not the field:
several fields of one type collapse to a single entry, and an instantiated
generic collapses to the generic interface it instantiates.

The two halves read the module differently, and on purpose:

| | how | why |
|---|---|---|
| the menu — what the containers expose | `go/parser` on the generated file alone | it is what you ask for when nothing builds yet, and it is the only way to learn what there is to request |
| `check` — what the tests replace | `go/types` over the whole module | a `Replacements` value arrives through a helper's return, a field of the scenario struct, an element of a slice; none of those resolves by name, and all of them are how a suite is really written |

`check` therefore refuses to answer on a module that does not type-check, rather
than reporting that nothing is replaced — and a service in that state fails
`go test` anyway. Loading the module costs about 0.4s, once per run, against a
build cache `go test` has already warmed.

The comparison itself lives in `github.com/specrig/specrig-go/waffle/replacements`, so a
service runs it from its own suite through the scaffolded
`TestReplacementsStayInsideTheList`. That is where the enforcement belongs — `go
test ./...` is what people and pipelines already run — so this command is the
report layer, and the comparison is what you ask for by name.

| | |
|---|---|
| no operand | print everything the containers expose, marking `# leaf` and `# allowed` |
| `check` | fail if a test fills in a field `replacements.allow` does not grant |
| `--container=<path>` | read this generated file instead of discovering it; repeatable |

`replacements.allow` is a **permission list**, not a list of exceptions: an empty
one allows nothing, and what it does not name may not be replaced in any form —
mock, spy, hand-written fake or no-op. **Where to draw that list is not a CLI
question**, and the whole of the reasoning — what `# leaf` means and where the
marker misses, which leaves are worth granting, which stand-in each interface
gets — is [conventions/component-harness](harness.md#choosing-where-to-cut).
The rest of this section is what the command itself does.

The check reads every `.go` file in the module and reports the position of each
field it finds filled in, in either form:

```go
diogen.Replacements{SecurityAdapter: spy}   // the literal handed to Boot
cut.SecurityAdapter = spy                   // filled in field by field
```

Two things are excluded. The generated container file declares the struct and
assigns every field — but only that file, not its directory, so a component test
sitting beside `InitAppWith` is read like any other. And `tests/internal/` is the
harness: it hands in the real database and the connections a boot demands, which
is the platform owner's own decision, so measuring it against the platform
owner's own list would deny the thing the list exists to allow.

A grant is keyed by import path and interface, never by field name — the field
name is diogen's, and it changes when registrations do.

The report writes a package holding more than one interface one per line, so a
marker sits on the interface it describes — the common case, because a domain
declares the port of an external adapter next to its own repositories. A single
interface stays inline, where there is nothing to tell apart:

```yaml
replacements:
  allow:
    gitlab.com/gorib/sql: [Db]  # leaf

    example.com/platform/warehouse/domain/warehouse/service:
      - FacilityRepository
      - SecurityAdapter  # leaf, allowed
      - UnitRepository
```

`# leaf` on a line means the seam's default constructor takes nothing else out of
the container. It is read off the body of `InitAppWith`, so it is a fact about
the graph and not a recommendation — the marker misses the real process boundary
in both directions, and not every leaf is worth granting. Both cases, with what
to do instead, are in
[component-harness](harness.md#what-the-marker-cannot-know).

**Mocks are not generated by specrig**, by this command or any other. A test that
needs one carries its own `//go:generate go tool mockgen` line — see
[component-harness](harness.md#a-mock-is-a-declaration-a-spy-is-an-observation).

`go tool specrig replacements check` runs the check and prints its report. It
reads the container, so it wants a tree that already compiles. It is the fast way
to ask — the same comparison rides in `go test ./...`, which boots the whole
component suite to get there.
