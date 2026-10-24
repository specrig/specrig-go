# Component harness

[specrig-go](../../README.md) · the waffle adapter

A component test boots the **real container** over **real infrastructure** and replaces only the leaves. It is the executable form of the specification Rule #11 asks for: a caller drives the service the way production does — a proto request over a real connection, an event through the real consumer, a cron command as the app runs it — down through the real management and the real repositories.

`go tool specrig harness` generates the harness from the container and regenerates it on every `make generate`. So this page is what the generated code cannot hold: the decisions nobody can make for you, the mechanisms behind it, and what goes wrong at run time.

## The generated file and yours

`tests/internal/harness/` holds two kinds of file, and they change for different reasons.

| file | owner | what it holds |
|---|---|---|
| `harness_gen.go` | specrig, rewritten on every run | the resources and the migrations, `Up`/`Reset`/`Down`, `Env`, `Main`, `CheckMigrations`, `App` and `Boot`, and `Clients` and `Serve` for a service with a gRPC or HTTP transport |
| `harness.go` | the service | the `go:generate` line, and the hooks — only those the service needs |
| anything else | the service | recorders of its own, `Deliver` helpers, whatever a scenario calls that the container does not describe |

The first run — the one that finds no `harness.go`, which `go tool specrig init` is — also writes the suite around it, once:

| file | what it holds |
|---|---|
| `tests/suite_test.go` | `TestMain`, which is `harness.Main(m)` |
| `tests/boot_test.go` | the boot smoke test, written before any case: `Boot`, and `Serve` where there is a transport |
| `tests/migrations_test.go` | `CheckMigrations` as an ordinary test, for a service with a database |
| `config/diogen/replacements_test.go` | `replacements.Check()` as an ordinary test |

A scaffold deleted on purpose stays deleted; `harness_gen.go` is the only file a later run touches.

Go beside Go in `tests/`, and `tests/feature/` left for the `.feature` files alone — only one of the two is a specification, and it is read by someone who does not read the other.

From a scenario the whole thing is two calls:

```go
app, err := harness.Boot(ctx, diogen.Replacements{ /* the cut */ })
clients, err := app.Serve()
```

### What is read off the container

- **The systems.** A `gitlab.com/gorib/sql.Db` seam means postgres and every `domain/*/cmd/migrate`, in name order; a `gitlab.com/gorib/redis.Db` seam means redis.
- **The recorders.** A seam of `pry.Logger`, `hilton.Provider`, `scribus.Consumer` or `amqp.Publisher` — or of the service's own interface over a publisher built by `amqp.NewPublisher` — gets the recording implementation, handed back on `App`. A `di.Tags` collection of one of these types is left alone: one recorder is not a list.
- **The trace.** Every call, event and command that crosses the service's edge is traced for [flows](flows.md#the-suites-record): the suite's calls, the typed clients a scenario replaced, the publishers, and the container's commands — single ones and `di.Tags` collections of them — behind getters `App` shadows.
- **The dead connections.** Every `grpc.ClientConnInterface` seam becomes `grpcClient.Dead(...)`, labelled with the typed clients built on it and the variables its constructor reads: `customer.AccountServiceClient, customer.CustomersServiceClient (AUSWEIS_HOST, AUSWEIS_PORT)`. Both halves are facts of the container; nothing is inferred from how a service names its variables.
- **The environment.** Every variable the service reads through `gitlab.com/gorib/env` is a field of `Variables`, named as the variable and typed as the read — unless `Boot` fills the seam above the constructor that reads it, so it never runs. The container's reads come from `di_gen.go`, the modules' included; a read anywhere else in `config/`, `domain/` or `cmd/` is a field too. A `Need*` or `Must*` read holds a placeholder that parses as its type: a string names its own key — `bladerunner-host.harness.invalid` — so a call that reaches it says which variable it came from; a number is `0`, a bool `false`, JSON `null`, and a list holds one such element: an empty one would reach the environment as `""`, which comes back as one element that no number parses. A read with a fallback holds that fallback, copied from the call and typed as Go types it — `uint16(8080)` makes the field a `uint16` — and a fallback that names a declaration of its own file is left out. Every read of a variable shares its one field: a single read without a fallback anywhere makes it a placeholder, and reads that disagree on its type stop the run, naming both call sites. `Env` always sets a placeholder, and a fallback only when the `env` hook changed it: the fallback is the container's own answer, and writing it back does not always round-trip — `[]string{}` comes back from the environment as `[""]`.
- **The transports and the clients.** `NewGrpcCommand` and `NewHttpCommand` hand over the handler, interceptor and router lists the daemon itself uses. A handler registers its service in `Start` with `proto.RegisterXServer`, and protoc-gen-go-grpc puts `NewXClient` beside it, so every handler in the module gets its typed client on `Clients`: the field is `X` without `Service`, prefixed with the package when two packages register a service of one name — `NotificationTemplate`, `DocumentTemplate`. A handler whose registration is not found is reported by name.

No `--with=postgres,elastic`, and there never will be: a flag closes the set, and elastic 7 against elastic 8 is not even a different system, just a different class for the same one.

### Hooks

What the container cannot say goes into `harness.go` as a hook. The generated file calls one only when the package declares it, and finds it by name; the compiler checks the signature against the call.

| hook | called | for |
|---|---|---|
| `func env(v *Variables)` | by `Env`, over the placeholders and fallbacks | a value with a meaning — a URL with a scheme, a key pair. A misspelt key or a wrong type does not compile, and the developer's environment still wins. `v.Custom` takes a key with no field of its own |
| `func setup() error` | by `Main`, first | what a step cannot set and the developer's environment must not override: `TZ` — Go reads it once per process, so a step that sets it comes too late |
| `func resources() []resource.Resource` | once, appended to the set | a system the container does not name — elasticsearch, a working directory the service writes into |
| `func (a *App) params(ctx context.Context) (cfg *di.Config, err error)` | by `Boot`, after `Up` and before `boot` | the container's parameters other than the context, in the order `InitAppWith` takes them — see below |
| `func (a *App) boot(ctx context.Context, cut *diogen.Replacements) error` | by `Boot`, before its own fills | a recorder of the service's own. Guard it with `if cut.X == nil` like the rest |
| `type extra struct{ … }` | embedded in `App` | the handles `boot` hands back: `app.Drivers` |

```go
func env(v *Variables) {
	// Unroutable rather than absent: an unset URL means the acquirer's production API.
	v.YOO_URL = "https://yookassa.harness.invalid/"
}
```

**`Custom` is the way out, not a way around.** It is for a key nothing reads through `gitlab.com/gorib/env` — `os.Getenv`, a library of someone else's, a variable the next change starts reading. A key that has a field is refused there at the start of the run, naming the field: two places to set one variable would disagree sooner or later, and the typed one is the one that compiles. When code starts reading a `Custom` key through `env`, the next `make generate` gives it a field and the refusal says to move it.

**Parameters are the caller's, and the harness is a caller.** A [parameter](https://gitlab.com/gorib/di/-/blob/main/README.md#parameters) of a markup function becomes one of `InitAppWith`. `Boot` passes its own `ctx` for the context; anything else is what the service fetches before the container exists, and nothing in the container says how, so `Boot` asks `params` for it. A container with such a parameter and no `params` hook stops the run, which prints the signature to declare. Its results follow `InitAppWith`, and diogen gives each parameter a type of its own, so results in the wrong order do not compile.

`Variables` is also the answer to "does anything read this": a value set for a variable no field names is set for nothing. A log level for the container's logger is one — `Boot` installs `pry.Recorder()`, so the constructor that reads `LOGLEVEL` never runs.

A hook is found in any file of the package, but keep the hooks in `harness.go` and what they are built from in a file named for its subject — `storage.go`, `keys.go`, `drivers.go`. Then `harness.go` is the list of what the service adds, and each topic is read in one place.

A service whose container says everything has a `harness.go` of one line. A hand-written harness from before the generated one declares `App`, `Boot` or `Up` itself; the run refuses it and names each file and declaration to move into a hook or delete.

## Three layers, and only two are shared

| Layer | Where | Why there |
|---|---|---|
| the seam — `Resource`, `Set` | `github.com/specrig/specrig-go/resource` | form, not a list: it imports no client, so it is tied to no version of anything |
| the implementations | `<pkg>/<pkg>test` — [`sql/sqltest`](#postgres), [`redis/redistest`](#redis), [`elastic/elastictest`](#elasticsearch) | they move with what they describe, in the same commit and the same version |
| `Boot`, `Env`, the resource list | the service, in `tests/internal/harness/` | `Boot` imports the service's generated `config/diogen`, and a library cannot depend on a service |

The third row is the reason there is no catalogue of systems in `specrig.yaml`. The generator writes what the container names; **anything else is a literal in the service's own code:**

```go
func resources() []resource.Resource { return []resource.Resource{kafkatest.New(...)} }
```

Typed, checked by the compiler, failing at build time. Adding a system is editing that line, and waffle needs no change for it. waffle knows the contract, never the list.

## The seam

```go
type Resource interface {
	Provision(ctx context.Context) error // it is there and reachable, or a loud error
	Migrate(ctx context.Context) error   // schema, index mapping or topic set up to date
	Reset(ctx context.Context) error     // empty it, between scenarios
}
```

Three methods, and two optional halves discovered by assertion: `Named` (its name is quoted in errors) and `Ephemeral` (`Down(ctx)`, for a resource with something to remove when the run ends). A third, `Fault` (`FailWith(err)`), is not for `Set`: it is all of a resource that `App` hands a step — see [A resource that goes down](#a-resource-that-goes-down).

`Set` runs them: `Up` provisions and migrates **once per run** and memoizes the failure, so a hundred scenarios report the first diagnosis instead of hammering an absent server. `Reset` calls `Up` first, so a suite that only ever calls `Reset` is still correct. `Down` tears down in reverse and does not stop at the first failure.

**`Down` ends the run.** The set stays "up" afterwards — the `sync.Once` is not reset — so a `Reset` or a `Boot` after it fails rather than quietly reprovisioning. That is why `Down` belongs in `TestMain` and nowhere else.

**The resource never names the client type**, in either direction. How it reaches the system is its constructor's business, and the choice follows one question: is management part of the client the service already has?

## Postgres

`gitlab.com/gorib/sql/sqltest` works from a DSN, because `gorib/sql` exposes neither `create database` nor migrations. The generated file writes the `sqltest.New(...)` literal and the goose call under it.

**Migration is a function you pass**, not something driven from inside. That keeps goose — or atlas, or tern, or hand-written statements — out of this module's dependency graph, which every service carries.

### Migrations of one domain do not depend on another's

The sources run in name order, and nothing lets a service change it: the daemon's own migrate command takes them from a `di.Tags` collection, which is an unordered set, so an order the harness pinned would hold only in the tests. What holds instead is that no order can matter, and `CheckMigrations` — `tests/migrations_test.go` — checks it two ways:

- **every domain applies alone to an empty database** of its own, `<name>_test_<domain>` beside the run's. A table whose trigger calls another domain's function fails here, naming the domain.
- **a version two domains ship is the same file.** The sources share one bookkeeping table, so goose applies whichever copy runs first and skips the other without a word; `waffleMigrate.CheckShared` refuses two different files under one version.

Together they are the fix as well: a domain that needs a function another domain creates ships its own byte-identical copy of that migration, under the same version. A database that already has it skips the copy; an empty one gets it from whichever domain comes first.

`Suffix(name)` is what gives the check its own database when the address is a DSN — `Database` is ignored there, and the check would otherwise land in the run's database and drop it.

### The address chain

Explicit `Dsn`, then `Env` in order, then `Docker`. The container is last so that CI and anyone with their own server never start one.

**There is no default DSN.** A default carrying a password would put a credential in the repository, so an address that resolves to nothing is a loud error naming the variable to set. Error messages report host, port and database — never the DSN, because they end up in logs and CI output.

### The container

`Docker(image)` starts the image with `POSTGRES_HOST_AUTH_METHOD=trust`, so **no password exists** — which is what makes a run with no configuration at all possible. The runtime is invoked as a binary (`docker`, else `podman`, or pin one with `Runtime`), not through a library, so nothing lands in the `sql` module's dependency graph.

The container is named and **reused**, not removed: every service in a repository shares the default name and port, two services being two databases in one instance. Measured on a laptop:

| | cost |
|---|---|
| starting the container, to accepting connections | ~2.6s |
| creating the database and applying every migration | ~0.2s — inside the run-to-run noise |

That asymmetry is the whole design. Reuse the container, throw the database away:

```
docker rm -f gorib-harness-pg   # to reset the container
```

### Ephemeral database

`Ephemeral()` makes the database this run's own: the name gets the process id appended, `Down` drops it, and `Provision` sweeps up what earlier runs left behind. A schema left by a migration that has since changed can then never be inherited, and it costs about what the noise between two runs costs.

**The suffix is what lets two processes run at once**, which is not an exotic case: `go test ./...` runs packages in parallel, so a service with two harness-using test packages would otherwise have one process drop the database out from under the other.

It also means the name in a DSN is not the name that gets used — `warehouse_test` becomes `warehouse_test_48213`. That is the trade, and it is why the suffix is tied to this option and not to the address: `Ephemeral` is the caller saying the database is theirs to create and destroy. Without it the name is taken exactly as given.

**The sweep is what keeps the suffix from leaking.** `Down` runs on a normal exit; it does not run on Ctrl+C, a test timeout, a panic in `TestMain` or a cancelled CI job — and with a per-process name every such run would leave another database behind, forever. So `Provision` drops the leftovers, and a database has to satisfy all three conditions to be taken:

| Condition | Why |
|---|---|
| the name is `<base>_<digits>` | only a name this package could have produced; matched in Go, not with `LIKE`, so a base containing underscores cannot widen it |
| its comment is this package's mark | the mark carries the creation time — `pg_database` records none — so a database somebody else documented is never a candidate |
| nothing is connected **and** the mark is over an hour old | the drop is forced, so a single condition would be enough to kill a run that is merely slow |

`Down` must be called **once per process**, from `TestMain` — the database is shared by every test in the package, and dropping it between two of them would leave the second without a schema. `harness.Main` is that call, and it is the only place `Down` belongs.

The drop uses `with (force)` — **postgres 13 or later**: our own pool is closed first, but a leaked connection would otherwise leave the schema behind.

### Reset

`Reset` truncates with `restart identity cascade`. The tables are **discovered from the schema** after migration — excluding a migration tool's own bookkeeping — so there is no list to keep in step with the migrations. `Tables(...)` pins them if you need to.

Truncation, not a database per scenario, and not a pinned transaction: the scenario talks to the service over a real connection, and context values do not cross that boundary.

## Redis

`gitlab.com/gorib/redis/redistest` works from a `host:port`, and the address chain is the postgres one: explicit `Addr`, then `Env` in order, then `Docker`, with the container last so that CI never starts one. `Reset` is `FLUSHDB`; `Migrate` does nothing, because keys carry their own shape.

**The container is this run's own, and `Down` removes it** — the opposite of postgres, from the same reasoning. Measured on a laptop:

| | postgres | redis |
|---|---|---|
| starting the container, to accepting connections | ~2.6 s | ~0.18 s |
| emptying it | ~0.2 s (create + migrate) | <1 ms (`FLUSHDB`) |

Postgres is a container that costs thirteen times what its contents cost, so you keep the container and throw the database away. Redis has no expensive contents to throw away, and the container itself is inside the noise — so you throw the container away instead. **Reuse what is expensive to build; discard what is cheap.** Copying the shape of `sqltest` here would have been copying its conclusion instead of its reasoning.

Two things follow. The runtime picks the port (`-p 127.0.0.1::6379`, read back with `docker port`) and there is no container name, because a fixed name and a fixed port are exactly how two runs would land in one instance — so `redistest` is parallel-safe by construction, and `Index` matters only for a server that was given rather than started. And `Down` runs `context.WithoutCancel`: a cancelled run is precisely when the container has to go, so teardown must not inherit the cancellation that triggered it.

**The sweep is what keeps a killed run from piling up.** `Down` does not run on Ctrl+C, a test timeout or a cancelled CI job. So `Provision` removes containers carrying the `gorib-harness=redis` label that are over an hour old and report no connections but its own probe. Unlike a postgres database, this needs no mark of its own: docker records the creation time.

## Elasticsearch

`gitlab.com/gorib/elastic/elastictest` goes over HTTP instead of through `elastic.Client`, for one concrete reason: emptying an index and making a seed visible both need `?refresh`, and no client here exposes it.

```go
var search = elastictest.New(
	elastictest.Env("CATALOG_TEST_ES"),
	elastictest.Docker("elasticsearch:8.19.7"),
	elastictest.Prefix("test_"),
	elastictest.Ephemeral(),
	elastictest.Index("product", productMappings),
)
```

Going over the wire also makes it blind to the client's major version — the management API for create, delete and refresh is the same on 7 and 8 — so a service on a back-ported v7 client uses it unchanged.

`Migrate` creates missing indices and **leaves existing ones alone**: a mapping change is a migration of its own, and guessing at one silently is worse than failing on the first query. `Reset` deletes by query with `refresh=true&conflicts=proceed`, keeping the mapping. `Refresh(ctx)` is exposed for a fixture that seeds through `Bulk` and then searches — without it that scenario flickers.

`Client()` is the service's own `elastic.New` with the resource's address and prefix, built in `Provision` — the way `redistest.Db()` is. A container that builds its own client instead has to be given `Prefix()` and `Url()`, or the resource manages indices the service never reads.

### The container

The address chain is the postgres one: explicit `Url`, then `Env` in order, then `Docker`. `Docker(image)` runs a single node with security off, so **no password exists**, published on loopback only, with a 512 MB heap and the disk watermark off — on a nearly full laptop disk elasticsearch otherwise turns the indices read-only mid-run. A missing image is pulled first, under a timeout of its own: it is over half a gigabyte.

**The container is named and reused, like postgres.** Measured on a laptop, `elasticsearch:8.19.7`:

| | time |
|---|---|
| starting the container, to a yellow cluster | ~15 s |
| reusing the running one | ~0.24 s |
| creating an index | ~0.15 s |
| emptying or deleting one | ~0.05 s |

A container that costs a hundred times its contents is kept; the indices are what a run throws away. Defaults are `gorib-harness-es` on port 59200, shared by every service in a repository; `Container`, `Port` and `Runtime` change them. Only a started container is waited for, up to two minutes; a given address fails at once.

```bash
docker rm -f gorib-harness-es   # to reset the container
```

### Ephemeral indices

`Ephemeral()` makes the indices this run's own: the process id goes into the prefix — `test_` becomes `test_48213_` — and `Down` deletes them. That is what makes two packages under `go test ./...` safe on one cluster. Without it the prefix is taken exactly as given and the indices are kept between runs; drop one by hand after a mapping change.

**The sweep keeps a killed run from leaking indices.** `Provision` deletes an index only when all of these hold:

- it is the base prefix, digits, `_` and a declared index name — another service's `test_2024_sales` is not ours to take;
- elasticsearch reports it created over an hour ago;
- no process with that id is alive.

Elasticsearch tracks no connections per index, so the process id stands in for one, and it is only comparable on this machine: the sweep runs only for a loopback address.

## A resource that goes down

A case about an outage breaks the client, not the server. Its step calls
`app.Postgres.FailWith(nil)`, and from then on every client the resource handed
out fails — the one the container already holds included — until the next
`Reset`. `nil` is a refused connection, `errors.Is(err, syscall.ECONNREFUSED)`;
pass an error of your own for anything else, a timeout say.

```go
app, err := harness.Boot(ctx, diogen.Replacements{})
...
app.Postgres.FailWith(nil)                   // Given the database is down
app.Redis.FailWith(context.DeadlineExceeded) // Given the cache times out
```

The server stays up because it is shared: every package of `go test ./...` uses
the one postgres and the one elasticsearch container, so stopping either breaks
the neighbours and costs a fifteen-second restart. A refusal in the client is
the scenario's alone.

| resource | what fails |
|---|---|
| `sqltest` | `Connect`, `Begin`, `DbSql` and every call of an open transaction. `Commit` rolls the real transaction back, so its connection goes back to the pool. A `*sql.DB` taken from `DbSql()` before the fault keeps working |
| `redistest` | every command, pipeline and dial |
| `elastictest` | every method of `elastic.Client` |

The resource's own work never fails: `Reset` heals before it empties, and empties
through a connection of its own, so its place in the set does not matter.

`Postgres` and `Redis` are on `App` where the harness filled the seam, like a
recorder; a scenario that supplied its own `SqlDb` gets no `Postgres`. They need
no grant in `replacements.allow`: the scenario replaces nothing. Elasticsearch is
not generated, so it reaches `App` through the hooks:

```go
type extra struct{ Search resource.Fault }

func (a *App) boot(ctx context.Context, cut *diogen.Replacements) error {
	if cut.ElasticClient == nil {
		cut.ElasticClient, a.Search = search.Client(), search
	}
	return nil
}
```

The publisher recorder has had the same method all along: `app.Publisher.FailWith(err)`.

## Writing a resource

For a client that is not in gorib — your own Kafka wrapper, an in-house system — the harness is three methods next to that client:

```go
package kafkatest

// Declared here, at the consumer: your admin client already satisfies it.
type Admin interface {
	TopicExists(ctx context.Context, topic string) (bool, error)
	CreateTopic(ctx context.Context, topic string, partitions int) error
	DeleteTopic(ctx context.Context, topic string) error
}

func New(admin Admin, topics ...string) *Kafka
```

Where to put it, by number of consumers — with no exception for gorib:

| Consumers | Place |
|---|---|
| one service | `tests/internal/harness/kafkatest` |
| two or more | next to the client: client in `pkg/kafka` → `pkg/kafka/kafkatest` |
| the client moved into gorib | the harness moves with it |

**To let a step fail it,** hand out a wrapped client that reads one switch, set the switch in `FailWith`, and clear it first thing in `Reset`. A resource with that method is a `resource.Fault`, and the `extra` hook puts it on `App`.

**Measure before choosing what to keep between runs.** All three existing resources start a container: postgres and elasticsearch keep it, redis disposes of it — see [Redis](#redis) for the two numbers that decide it. Take the reasoning from whichever one your system resembles, not the shape.

## Choosing where to cut

The decision is written down, in `specrig.yaml`, as a permission list:

```yaml
replacements:
  allow:
    example.com/platform/api/contract: [ContractServiceClient]
```

What it does not name may not be replaced, in any form.
`TestReplacementsStayInsideTheList`, scaffolded beside the container by `go tool
specrig harness`, reads the `Replacements` literals in the tests and fails
naming the field and the interface — so a cut nobody granted stops being a
question of whether a reviewer noticed, and shows up in `go test ./...` rather
than only when someone runs a command. `go tool specrig replacements check` is the
same comparison on demand. Two things it deliberately does not
decide: **which form** the replacement takes — mock, spy, fake or no-op — which
follows from the assertion and belongs to whoever writes the test, and anything
in `tests/internal/`, which is the same person's harness as the list itself.

A grant is keyed by interface, not by field, so an interface serving two
container fields is granted for both. `google.golang.org/grpc:
[ClientConnInterface]` therefore grants every connection in the container at
once, which is a reason not to grant it.

A **composite** field — `interface{ A; B }`, produced when registrations share
one container field — is granted only when every constituent is, because
replacing the field replaces every contract it merges. Those are the most
informative lines in the report rather than noise: a composite names the
contracts one management object owes, and each constituent is a caller, which is
a place a scenario binds.

`go tool specrig replacements` prints the menu; a person picks from
it, and what they pick goes into `replacements.allow`. The `# leaf` marker means
one thing only — *this constructor takes nothing else out of the container* — and
that is a heuristic, not the answer. It separates the two kinds of test, though:
a **component test** replaces leaves and lets everything else run for real,
while replacing an interior seam deletes the subtree below it — the code the test
was meant to exercise. A **unit test** builds its subject by hand, so
`Replacements` plays no part and the list has nothing to say about it.

**Cut above anything that speaks a language the code itself generates.** A
stand-in for such a level would have to speak it too, and then the test checks
the language instead of the behaviour. `sql.Db` carries SQL the repository
builds; `grpc.ClientConnInterface.Invoke(ctx, method string, args, reply any)`
carries the wire — a method name as a string and an untyped payload; an
elasticsearch client carries the query DSL.

So for an outbound gRPC dependency the cut is the **typed client**, one level
above the transport, even though `go tool specrig replacements` does not mark it `# leaf` — its
constructor does take the connection out of the container, which is exactly why
it is not a leaf. Replacing it prunes the connection beneath, so the scenario
supplies the typed client and nothing else; a scenario that leaves a client real
gets a dead connection under it from the generated `Boot`. This shape recurs
wherever a service adapts one transport to another — an http-to-grpc adapter looks
the same.

### What the marker cannot know

- **`sql.Db` is a leaf and must not be replaced.** The component test hands it a
  real database, so it stays out of `replacements.allow` and the harness supplies
  it instead.
- **An interior node can be the right cut**, as above. Not being a leaf is not a
  reason to leave it alone.
- **A boundary can sit below the leaf and outside the container.** A factory that
  builds its client per call from runtime data keeps the real boundary out of
  `Replacements` entirely: nothing supplied there can reach it, so the component
  test cuts at the factory's own port and the boundary below is mocked only for a
  unit test of the factory — which needs no grant, because `Replacements` plays
  no part in it.
- **An interface with no methods is not a seam.** Anything satisfies it, so
  granting it grants nothing; if one exists in the code, somebody intended a seam
  and left a stub.

## Replacing the leaves

`Boot` fills the cut: everything that is a condition of booting rather than a choice the test makes belongs in `Boot`, not in the scenario. It supplies the database, installs the recorders and hands them back on `App` — a recorder the scenario had to construct would be a seam the scenario had to be granted, for something the harness owns either way.

`Dead` is there for the clients a scenario leaves real. A replaced typed client prunes its connection, but most scenarios replace only the clients their case calls, and every other one is built for real on a connection of its own; without `Dead` that connection would read the neighbour's host out of the environment and dial it. Reaching a dead connection panics with its label, which names the client the scenario forgot to replace — louder than a silent call to a real neighbour.

It covers only a transport that is a container field of its own. A client that builds its connection inside its own constructor — `http/client.New(url)` in an adapter, a device opened from a path — has no seam under it to fill; replacing the adapter is the cut, and the placeholder in its variable is what a call that slipped through reaches: `bladerunner-host.harness.invalid`, an error rather than a panic.

**Every fill is guarded with `if cut.X == nil`,** and a `boot` hook guards its own the same way. That is the seam by which a scenario overrides what `Boot` installs — a live fake instead of a dead connection, a hand-built double instead of the harness recorder — without `Boot` having to know which scenarios want one. A recorder goes onto `App` only where it went into the container, so a scenario that brought its own and then reads `app.Logger` gets a nil, not a recorder nobody wrote to.

One consequence of `di.For` worth expecting: it groups the consumers that share a connection, so the container has one seam per *connection* rather than one per client, and that seam is named after the first of its consumers in name order, so reordering the `di.For` lines does not rename it. Four `Dead` lines can cover eight typed clients, and the label lists all of them.

### `Noop()` is a recorder nobody queries

That is the rule that decides which to use. A silent stand-in is fine where the scenario asserts nothing about that leaf — and dangerous where it does. A silent consumer swallows `Apply` and `Bind`, so a typo in a handler's subject stays green forever while production receives nothing.

The generated `Boot` therefore installs the recording one: whether anything is ever asserted about the logger is not knowable from the container, and a recorder nobody queries costs a slice, while a `Noop()` somebody needed costs a debugging session. A scenario that wants silence supplies `pry.Noop()` in its own `Replacements`.

| Interface | Silent | Recording |
|---|---|---|
| `pry.Logger` | `pry.Noop()` | `pry.Recorder()` — levels, messages, fields, errors |
| `scribus.Consumer` | a recorder you do not query | `scribus.Recorder()` — wiring, plus `Deliver` |
| an amqp publisher | — | `amqp.NewPublisherRecorder()` — keys and payloads, `FailWith` |
| `outbox.Sender` | — | `outbox.Recorder()` — subjects and payloads, `FailWith`. Replace this and not the `Store`, so `Enqueue` still writes a row ([details](https://gitlab.com/gorib/waffle/-/blob/main/docs/tools/outbox.md#testing)) |
| `grpc.ClientConnInterface` | `grpcClient.Dead(name)` | — |

**`pry.Recorder()` is a driver, not a second logger.** It sits underneath the real logger, so a scenario asserts on records the production code built rather than on a parallel implementation of the same options.

### A mock is a declaration, a spy is an observation

Which one a seam gets follows from where the assertion sits, and that is decided by the notation rather than by taste.

A generated mock declares its expectation **before** the call. A specification written in Gherkin puts its assertions **after** the action — `Then` follows `When`, by grammar. So in a scenario suite every conversion to a mock ends in one of three places: the declaration moves into the `When` step and the `Then` line stops asserting anything; a new `Given` states the expectation, and the specification describes a call sequence instead of behaviour; or `.AnyTimes()` with a `.DoAndReturn()` writes into the very slices a spy would keep, plus a controller on top.

**So: in a godog suite a recorder is the default and a mock is the exception. In a unit test it is the other way round** — there the assertions are declarations already, the object under test is built by hand, and `.Times(0)` and `.Times(1)` are the clearest form there is.

Two things this does not excuse:

- A recorder must still assert the count. `.Times(1)` comes free with a mock; a spy that finds *a* matching call and stops will pass an implementation that calls twice.
- The lifecycle is not the obstacle it looks like. godog runs each scenario as a subtest when `Options.TestingT` is set, and `godog.T(ctx)` satisfies gomock's reporter, so a per-scenario controller with correct attribution is available. The grammar is the reason, not the plumbing.

**Waffle generates no mocks**, and no command will. The test that needs one
carries its own directive, which keeps the mock beside its only user and inside
`go generate ./...`:

```go
//go:generate go tool mockgen -typed -package security -self_package <this package> -destination client_mock_test.go <this package> Client
```

A `_test.go` destination inside the package under test is what makes a private
field reachable, and it keeps a single-use mock out of the production build and
out of everyone else's import graph.

### Delivering an event

`scribus.Recorder()` keeps the wiring the command asked for and delivers through **the handler the container installed** — so a scenario exercises the subject dispatch and the middlewares, not a bare `Run`:

```go
err := w.app.Consumer.Deliver(amqp.Delivery{
	RoutingKey: "contracts.updated",
	Body:       payload,
}.Source(ctx)) // nil settled, error requeued
```

`amqp.Delivery{...}.Source(ctx)` builds the message. It exists because the real constructor is unexported: without it every suite writes its own `Source`, and each one gets the lookup scopes slightly differently.

## The in-process server

`Serve` is a method on `App` of a service that has a gRPC or HTTP transport, and it answers with one `Clients` however many transports the service serves. The typed clients on it are read off the handlers — [above](#what-is-read-off-the-container) — so a handler added by `di.Tag` gets its client on the next `make generate`.

`waffleGrpc.Serve` runs the handlers over `bufconn` and chains the interceptors with **`UnaryChain`, the exact function the daemon uses**. That is the point of it being exported: interceptor order has caused a real production bug (see [tools/grpc](https://gitlab.com/gorib/waffle/-/blob/main/docs/tools/grpc.md)), and a harness that sorted its own copy would be checking its copy of the order rather than the service's.

The **lists** come from the container for the same reason — `a.ServerInterceptors()` and its siblings, and the router collection beside them, not the members written out one by one. A registration added to the chain later reaches the daemon and the suite together; written out, it would reach only the daemon, and the scenarios would go on passing against a chain that no longer ships.

`waffleHttp.Serve` is the same idea over `httptest`. It returns a `URL` and a `Client` — an ordinary `*http.Client`, so a scenario writes an ordinary request. The routers and the global chain are the container's own, and `Setup` is the same function the daemon runs, so the routes, the collision check and the middleware under test are the ones that ship — including its two errors, which is why `Serve` returns one. Both `InProcess` types close in `Clients.Close`.

## Rules that came from being wrong

- **Infrastructure is a requirement, not an inconvenience.** If the database is unreachable the harness fails loudly. A suite that silently skips looks green.
- **`Given` writes fixtures through the container's own repositories**, never through the operation under test. A `Given` expressed as a `Create` couples scenarios to each other; going through the repository means a schema change breaks fixtures loudly.
- **`Then` reads back through the repository**, not from the operation's own response. Strictly stronger: it catches "returned right, persisted wrong", and it is the same step whether the operation returns an object or nothing.
- **A scenario for a known defect asserts the intended behaviour**, so it is red until the defect is fixed. The red count has to equal the defect count — arithmetic that catches a case written with the wrong polarity.
- **Check whether you need a clock seam before adding one.** Comparing `time.Now()` against a fixture's date means relative fixtures express every time-dependent case and do not rot. A clock seam added for a test is production code written before the specification was approved.

## Not there yet

- **Elasticsearch is not generated.** The harness fills an `elastic.Client` seam only through the `resources` and `boot` hooks; `redistest` gets `cut.X = cache.Db()` from the generator.
- **Rabbit has no resource yet.** A `flushdb`-shaped afternoon when a service needs one.
