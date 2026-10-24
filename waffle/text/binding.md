**Bind at the declared contract** — the operation: a handler in `cmd`, a
consumer command, a cron command. Not the management interface below it: one
layer short skips conversion and the error→code map, and makes stand-ins invent
database behaviour (uniqueness, not-found) that belongs to the schema. Fixtures
go in through the container's own repositories, never through the operation under
test.

Then bind to the real container through the harness, which fills in what is a
condition of booting rather than a choice the case makes:

```go
app, err := harness.Boot(ctx, diogen.Replacements{ /* the cut */ })
clients, err := app.Serve()
```

`Boot` wraps `diogen.InitAppWith`; call that directly only on a service with no
harness. The `.feature` files go in `tests/feature/` and the Go beside Go in
`tests/` — only one of the two is a specification, and it is read by someone who
does not read the other.

#### What may be replaced is given, not chosen

`replacements.allow` in `waffle.yaml` is a permission list: what it does not
name may not be replaced, in any form — mock, spy, fake or no-op.
`./waffle tests:replacements` prints every seam, granted or not; it is a menu for
the platform owner, not a licence.

**A missing or empty `replacements:` key grants nothing** — not "unrestricted",
and not an oversight you may repair. Check it before writing cases: if the list
and the contract do not meet, **stop there and say so**.

`TestReplacementsStayInsideTheList` fails in the ordinary suite on every field
the list does not cover; `./waffle tests:replacements check` prints the same —
run it before handing over and paste what it said. **Its failure names the seams
you need, not an edit to make:** the hand-over is the red output plus the seams
you are asking for. Putting them into `waffle.yaml` turns the check into a
formality that agrees with whatever you did.

`tests/internal/harness/` is read-only too. It supplies what is a condition of
loading rather than a choice — the database, the logger, a dead connection under
a replaced client. Do not change it, and do not move a replacement in there
because a scenario was inconvenient.

**The form of the replacement is yours**, because it follows from the assertion:

- a **spy** keeps what it was given and is read in a `Then`. A Gherkin step
  asserts after the call, which is the shape a spy answers — so this is the
  default, and the recorders already exist rather than being written per service.
- a **mock** declares before the call, so it fits where the expectation belongs
  to the `Given`. Declare a negative expectation explicitly (`.Times(0)`): an
  assertion that exists only as a missing line cannot be reviewed.
- a **no-op** keeps nothing, so no `Then` can read it.

An outage is not a replacement: the step calls `app.Postgres.FailWith(nil)` —
`Redis` and `Publisher` likewise — and the next `Reset` heals it. Do not ask for
a grant on the client to fake one.

`docs/conventions/component-harness.md` in waffle has the catalogue — which
recorder exists for which seam — and the reasoning behind a particular grant.
Read it to understand what you were given; it is not a licence to change it.

#### When the cut does not work, stop

Say what you hit and what the specification needs, and hand it back. Do not work
around it. The recognisable cases:

- **a case needs a seam `replacements.allow` does not grant.** Name the
  interface and the assertion it would carry. Adding the grant is not yours.
- **a `Then` cannot see what it must assert** — the seam is a no-op, or a mock
  whose expectation would have to be declared before the scenario knows the
  value.
- **the boot reaches for the environment or the network.** A replaced typed
  client prunes its connection, and the harness gives every client a scenario
  leaves real a dead one. A boot that still reaches out is a gap in the harness.
- **a case needs a seam that does not exist** — controlled time is the usual
  one. Where the code compares a fixture's date against `time.Now()`, set the
  fixture relative to now (`now.AddDate(-1, 0, 0)` for "already started") and no
  seam is needed. A case that needs time to *move* does need one, and a new seam
  is the platform owner's, in platform mode.
- **the cut removes what the case was meant to exercise** — a repository
  replaced by a mock makes the test check the mock's SQL, not the schema's.

**Do not build the infrastructure by hand.** `./waffle tests:harness` generates
`tests/internal/harness/harness_gen.go` from the container — the database and its
migrations, the transport and a typed client per handler, the interceptor order,
a dead connection for every `ClientConnInterface` seam, a typed field for every
variable the boot reads — and `go generate ./...` regenerates it, in any mode;
that change is not a platform edit. Never edit that file. A run needs no
configuration either — with no DSN set, a postgres container is started and the
next run reuses it, so do not ask for one. `docs/conventions/component-harness.md`
in waffle is the reference.

**The boot smoke test comes before any case**, and the first run writes it: it
brings the resources up, boots on an empty cut and, where the service has a
transport, serves it; it asserts nothing about behaviour. One run then tells you
what the cut is missing instead of an hour inside a half-written suite.

What the container cannot say for the whole suite — a second resource, a value
every case boots with, `Custom` for a key no code reads through
`gitlab.com/gorib/env` — goes into hooks in `tests/internal/harness/harness.go`,
which is the harness owner's file, not yours.
