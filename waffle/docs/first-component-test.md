# Your first component test

[specrig-go](../../README.md) · the waffle adapter

From `go tool specrig init` to one green scenario and one red one. The
generated harness gets you as far as "the container boots"; this page is the
rest of the way.

**Who this is for.** Setting the harness up is a platform job, done once per
service, and by a person — under Rule #11 an agent may not touch
`tests/internal/` or `specrig.yaml` at all. After this page the scenarios
themselves are ordinary work anybody does.

[Component harness](harness.md) is the
reference for every decision below: what each resource does, why the container
is reused and the database is not, where to cut. This page does not repeat it —
it links into it at each step.

## What you need

- A container that compiles. `go tool specrig replacements check` refuses a module
  that does not type-check, and so does everything else here.
- Docker or podman on the path — **or** nothing at all, if you already have a
  postgres and set `<SERVICE>_TEST_DSN`. The harness starts a container only as
  a last resort.

Nothing has to be running. That is the point of the address chain.

## 1. Generate it

```sh
go get -tool github.com/specrig/specrig-go/waffle/cmd/specrig
go tool specrig init
```

```
+ specrig.yaml
~ AGENTS.md: the line that brings specrig.md in, at its end
+ specrig.md
  + config/diogen/replacements_test.go
  + tests/boot_test.go
  + tests/internal/harness/harness.go
  + tests/migrations_test.go
  + tests/suite_test.go
  ! the suite imports what go.mod may not require yet: run go mod tidy
  + generated tests/internal/harness/harness_gen.go
```

```sh
go mod tidy
```

[The commands](commands.md#go-tool-specrig-init) say what the first three lines
are; the rest is the harness.

Everything in `harness_gen.go` was read off your generated container — the
database and its migrations, the transports and a typed client per handler, the
connections that must be dead, a placeholder for every variable the boot reads.
It is rewritten on every `make generate`, through the `go:generate` line in
`harness.go`, so a new domain, handler or neighbour reaches the suite with no
edit. `harness.go` is yours, and on most services it stays one line. The
[tables](harness.md#the-generated-file-and-yours) say
which file holds what.

## 2. Run the smoke test

`boot_test.go` asserts nothing about behaviour. It is there so that "the cut is
incomplete" costs one run instead of an hour inside a half-written suite.

```sh
go test ./tests/ -run TestBoot -v
```

The first run pulls `postgres:18-alpine` and starts it — about three seconds,
once. Every run after that reuses the container and pays ~0.2s for a fresh
database.

Three failures are the common ones, and they are all at this step by design:

| It says | What to do |
|---|---|
| a constructor refuses `…harness.invalid` | the placeholder parses as its type but means nothing — a URL without a scheme, a key that is not a key. Give it a real value in the `env` hook |
| a variable is missing | it is read without `gitlab.com/gorib/env` — `os.Getenv` — so it has no field. Put it in `v.Custom` in the `env` hook |
| `interface conversion: interface is nil` | a seam the boot needs is neither built nor replaced. The panic names the type |

Green here means the whole graph constructs over a real database. That is the
expensive part, and it is now done.

## 3. Add what the container cannot say

Everything else goes into `harness.go` as a hook, and only when the service
needs one — [the table](harness.md#hooks) lists them.
The two you are most likely to meet:

**A value with a meaning.**

```go
// tests/internal/harness/harness.go
func env(v *Variables) {
	// Unroutable rather than absent: unset means the acquirer's production API.
	v.ACQUIRER_URL = "https://acquirer.harness.invalid/"
}
```

`Variables` has a field per variable the service reads, typed as it is read, so
a misspelt key does not compile.

**A second resource.** Elasticsearch, Kafka, anything not read off the
container:

```go
func resources() []resource.Resource {
	return []resource.Resource{search}
}
```

See [writing a resource](harness.md#writing-a-resource)
for the three methods and where the package belongs.

`harness_gen.go` is not edited: the next `make generate` puts it back.

## 4. Decide what a test may replace

```sh
go tool specrig replacements
```

This prints every seam the container exposes, `# leaf` on the ones whose
constructor takes nothing else out of it. **It is a menu, not a recommendation.**
Pick from it, and put what you picked in `specrig.yaml`:

```yaml
replacements:
  allow:
    example.com/catalog/domain/product/service: [PricingAdapter]
```

An absent key and an empty one both grant nothing, so on a fresh service the
first component test fails `TestReplacementsStayInsideTheList` until somebody
writes a line here. That is deliberate and it is this step.

Three things decide the list, and all three are argued in
[Choosing where to cut](harness.md#choosing-where-to-cut):
do not grant `sql.Db` (the test wants the real database), cut at the **typed
client** rather than the connection for an outbound service, and do not grant
what only has to be non-nil for the boot — the harness already supplies it.

## 5. The first scenario, green

Three rules shape it, and they come from
[Rules that came from being wrong](harness.md#rules-that-came-from-being-wrong):

- **`Given` writes through the container's own repository**, never through the
  operation under test.
- **`When` drives the declared contract** — the handler, over a real connection.
  Not the management interface below it: one layer short skips conversion and
  the error-to-status map.
- **`Then` reads back through the repository**, not out of the response. It is
  strictly stronger — it catches "answered right, persisted wrong".

```go
// tests/product_test.go
package tests

import (
	"net/http"
	"strings"
	"testing"

	"example.com/catalog/config/diogen"
	"example.com/catalog/tests/internal/harness"
)

func TestCreateProductPersistsIt(t *testing.T) {
	ctx := t.Context()
	booted, clients := boot(t, diogen.Replacements{})

	// Given — through the repository, so a schema change breaks this loudly.
	repo := booted.ServiceProductRepository()

	// When — over the wire the daemon serves.
	res, err := clients.Client.Post(
		clients.URL+"/products",
		"application/json",
		strings.NewReader(`{"title":"Waffle iron"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", res.StatusCode)
	}

	// Then — read it back, not off the response.
	stored, err := repo.ByTitle(ctx, "Waffle iron")
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil {
		t.Fatal("the product was answered for but not persisted")
	}
}
```

`ServiceProductRepository` is diogen's name for the field, not one you choose —
it follows the interface the registration exposes and changes when the
registration does. `go tool specrig replacements` prints the current names, and so
does `config/diogen/di_gen.go`.

The helper is the part worth writing once:

```go
func boot(t *testing.T, cut diogen.Replacements) (*harness.App, *harness.Clients) {
	t.Helper()
	ctx := t.Context()

	// Between scenarios, not between runs: Reset calls Up itself.
	if err := harness.Reset(ctx); err != nil {
		t.Fatalf("resources: %v", err)
	}
	booted, err := harness.Boot(ctx, cut)
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	t.Cleanup(func() {
		if err := booted.Shutdown()(); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	clients, err := booted.Serve()
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	t.Cleanup(clients.Close)
	return booted, clients
}
```

`Reset` truncates with `restart identity cascade` over the tables it discovered
after migrating — there is no list to keep in step. Not a transaction per
scenario: the scenario talks to the service over a real connection, and context
values do not cross that boundary.

### Asserting on a recorder

`Boot` installed the recorders and handed them back on `App`, so nothing about
them needs a grant:

```go
if len(booted.Logger.At(pry.LevelError)) != 0 {
	t.Error("the request logged an error")
}
```

An event goes in through the consumer the container installed, so the subject
dispatch and the middlewares run:

```go
err := booted.Consumer.Deliver(amqp.Delivery{
	RoutingKey: "product.created",
	Body:       payload,
}.Source(ctx))
```

`amqp.Delivery{…}.Source(ctx)` exists because the real constructor is
unexported. `Deliver` returns what the handler returned — nil is settled, an
error is requeued.

## 6. The first scenario, red

A case written for a **known defect asserts the behaviour you want**, so it
fails until the fix lands. Writing what the code does today turns the defect
into a green regression boundary — protected, and reading as correct.

```go
// Red until the fix: Create currently accepts an empty title.
// The rule is that a product without a title is refused with 422.
func TestCreateRefusesAnEmptyTitle(t *testing.T) {
	_, clients := boot(t, diogen.Replacements{})

	res, err := clients.Client.Post(
		clients.URL+"/products",
		"application/json",
		strings.NewReader(`{"title":""}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want 422", res.StatusCode)
	}
}
```

The comment carries the rule in the present tense and says the case is red on
purpose, so nobody reads it as a broken test. It carries no verdict tag and no
date: that is review state, and it stops being true the moment somebody decides
otherwise.

**The arithmetic that catches a mistake here:** the number of red cases equals
the number of known defects. A mismatch means a defect got frozen green, or a
case was written with the wrong polarity — both have happened.

## 7. Wire it into the suite

Nothing special: `go test ./...` runs all of it, and the scaffolded checks run
with it —

- `TestReplacementsStayInsideTheList` fails on a seam `specrig.yaml` does not
  grant, naming the field and the interface;
- `TestDomainsStayBehindTheirAdapters` fails on a cross-domain import or
  registration the [boundary rule](https://gitlab.com/gorib/waffle/-/blob/main/docs/architectures.md#the-boundary-rule) forbids;
- `TestMigrationsAreIndependent` fails on a domain whose migrations do not apply
  to an empty database on their own, or a version two domains ship differently
  ([why](harness.md#migrations-of-one-domain-do-not-depend-on-anothers)).

All three are ordinary tests on purpose: they ride in the suite a pipeline runs
anyway, rather than each needing a step of its own. `make test` is that suite,
and it is **not** part of `make` — the component tests boot real
infrastructure, which is too slow for the loop that runs after every edit. So a
green `make` says the service builds and nothing more; put `make test` in the
pre-commit hook or the pipeline, or both.

In CI, give the job a database rather than letting it start a container —
`<SERVICE>_TEST_DSN` comes before `Docker` in the address chain, and the
container is last precisely so that CI never reaches it.

## On Gherkin

`waffle` scaffolds Go tests, and the harness is notation-agnostic: `Boot` and
`Serve` are the same two calls under godog. What waffle does not ship is the
godog wiring, because the step vocabulary is the service's. The one call it asks
for is `harness.Steps(sc)` in the `ScenarioInitializer`, so that the
[flows](flows.md#the-suites-record) know each case's file and its action.

If you go that way: `.feature` files in `tests/feature/`, the Go beside Go in
`tests/` — only one of the two is a specification, and it is read by someone who
does not read the other. And **in a godog suite a recorder is the default and a
mock is the exception**: a mock declares its expectation before the call, while
`Then` follows `When` by grammar. The
[reasoning](harness.md#a-mock-is-a-declaration-a-spy-is-an-observation)
is worth reading before the first conversion, not after twenty.

**One notation per service.** Two ways of writing cases in one repository is
load with no payoff.

## Next

| | |
|---|---|
| Every decision on this page, argued | [Component harness](harness.md) |
| Why the specification comes before the code | [conventions/ai-native](ai-native.md) |
| The protocol an agent follows against this suite | [cli/agents](https://github.com/specrig/protocol/blob/main/specrig-md.md) |
| `Replacements`, and what has no entry in it | [di/overview](https://gitlab.com/gorib/waffle/-/blob/main/docs/di/overview.md#testing-with-the-container) |
| The report and the check behind the permission list | [cli/overview](commands.md#go-tool-specrig-replacements) |
| The interfaces a resource implements | [reference/interfaces](https://gitlab.com/gorib/waffle/-/blob/main/docs/reference/interfaces.md#resource-package) |
