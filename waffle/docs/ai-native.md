# AI-native development

[specrig-go](../../README.md) · the waffle adapter

How a change gets made when a coding agent writes most of the code.

The premise is a shift in what is scarce. Writing code is no longer the
bottleneck; deciding what *correct* means is. So the human artifact of a change
stops being reviewed code and becomes an approved specification, and the agent's
job is to satisfy it without being able to move the goalposts.

**The protocol itself is Rule #11 in `specrig.md`** — the file specrig writes in the
root of your own service, and `AGENTS.md` brings in, written for the agent that
works to it: the roles,
the three steps and the stop after each, what may not appear in the diff. This
page is the other half — why the cycle is shaped that way, what the framework
gives you, and, honestly, what is not built yet. Read this to decide; read Rule
#11 to follow. [cli/agents](https://github.com/specrig/protocol/blob/main/specrig-md.md) describes the
file and how to keep it current.

## The cycle

```
requirements ──▶ specification + delta list ──▶ approval ──▶ red-green
                          ▲                                       │
                          └─────── conflict or constraint ────────┘
```

1. **Requirements in, specification out.** The agent produces an executable
   specification and a *delta list*: every place the specification says something
   the source document does not, plus what was deliberately left out of scope.
2. **Verification, then approval.** A human reads the source document and the
   delta, and corrects or approves. No production code before approval.
3. **Red-green.** The specification is frozen. Steps bound to the real container
   run every case red for its own reason before any behaviour is written; then
   production code until they pass.

Anything that cannot be built without changing the specification goes back to
step 2 through the person who approved it. The agent never edits an approved
test on its own judgement — an agent told to make a test pass will otherwise
weaken the test, and that is the failure mode this whole shape exists to prevent.

There is a subtler version of that failure, and it took catching in practice: a
case written for a *known defect* must assert the behaviour you want, so it is
red until the fix lands. Write what the code currently does and the defect
becomes a green regression boundary — frozen, protected, and reading as
passing. The check that catches it is arithmetic: the number of red cases has to
equal the number of known defects, and a mismatch means either a defect got
frozen or a case is wrong.

### Why it holds

Three things, and none was designed up front: each came out of an agent breaking
the rule with the rule in front of it. The incidents are in the
[field notes](ai-native-field-notes.md); what they left behind is here.

**A review surface that does not depend on the author's compliance.** It costs
two glances:

- the platform's files, production files and `CONTRIBUTING.md` do not appear in the diff
  of a specification task — one look at the file list;
- the number of red cases equals the number of known defects, and every
  production file in the diff is named by a case that went red — two numbers and
  a list.

**Prose describing a state does not stop an agent with momentum.** A prohibition
needs either a mechanism or a surface a third party can check. Every incident was
argued against, at length, in the file the agent had read — the third time it
lost to an argument about which rule applies.

So the step boundary is a mechanism. Every edit is made in a **mode** —
`spec`, `redgreen`, `platform`, `auto` — and each allows a fixed set of
paths. Only the human switches the mode. The table is specrig's: `specrig.md`
renders it, and `go tool specrig rules` prints the same copy for the plugin, which
names the mode that would allow a refused path. The agent's move stops being
"decide whether this is still the task" and becomes "ask for the mode" — a
question the human answers in one word.

Two decisions in it are deliberate:

- **The rules stay in the file the agent reads.** The mode table is part of Rule
  #11 in `specrig.md`, so an agent in any tool — or in a cloud session, where
  plugins do not load — works to the same protocol and holds the boundary itself.
  A tool adds enforcement, not content.
- **The table is versioned with the service.** `go tool specrig rules` is the
  service's own tool, so services on different versions each get their own rules,
  and whatever calls it holds none.

In Claude Code a plugin enforces the table —
[cli/agents](https://github.com/specrig/specrig) has how to install and use
it.

**Two boundaries pay for a switch.** Measured on the first sessions with the
modes: the costliest mistake a mode catches is production code before the
specification is approved, and the one that goes unnoticed is a case changed to
fit the code. So spec mode is the only mode closed to production code and the
only place a case changes, and after approval the steps and the code are one
mode, with the cases frozen. A boundary that catches nothing costs the human a
command every time it is crossed.

### Why a delta list rather than "read the scenarios"

Requirements written by analysts always have gaps, and an agent transforming them
into a specification must resolve those gaps to make each case unambiguous. It
will do so silently, because a case that is ambiguous is not executable.

So the thing worth reviewing is not "does the specification match the document" —
it is "where did the specification decide something the document did not". That
comparison is bounded: read the document, then confirm or correct a short list.
Diffing prose against twenty scenarios in your head is not bounded, and a
specification nobody reviewed carefully is worse than none, because approval has
made a guess authoritative.

The out-of-scope half of the list matters for a specific reason: a reviewer
comparing two documents reliably catches a wrong answer and reliably misses a
missing question. Absence has to be stated to be reviewable.

### Measurements the rule leans on

The instructions in `AGENTS.md` are terse where these numbers are the reason for
them.

- **One specification per declared contract, not per service.** A domain's
  management can serve four callers — gRPC, a consumer and two cron commands —
  with no overlap at all, so a specification written against the gRPC operations
  alone would cover one contract in four. The
  `interface{ A; B; C }` entry that `go tool specrig replacements` prints
  for a shared container field *is* that list of contracts.
- **Adjudication of defects is the human's.** Deriving candidate defects from
  code is the cheap half and it is wrong often: on the services this was first
  built on, several proposed defects turned out to be deliberate business rules
  and one could not be reproduced at all.
- **The red-count check earns its place.** "The number of red cases equals the
  number of known defects" has caught a real mistake more than once — a defect
  frozen as a green boundary, and a case that was simply wrong.
- **A case can fail to discriminate and still look complete.** Three cases
  pinning "the adapter is asked with the facility's own configuration" all used
  one facility with one provider, so an implementation holding a single
  configuration for the whole process passed all three. The second facility is
  what turned them into cases.
- **Hand-computed numbers fail at about one in a hundred.** One row of a hundred
  and four had the wrong date — month-end clamping done in the head — and the
  code was right. Which is why the gate is told that the arithmetic was read
  rather than run.
- **An unexpected failure is as likely to be the step as the service.** One of
  sixteen: a column added to a fixture table that the step never read.

## Why this needs the container cut

A specification for a feature is stated at the boundary of the service — given
this state, when this call arrives, then this happens. Running it means the
service is assembled, and there are two ways to get there: the full environment,
or the service with its outside edges replaced.

In a fleet of two dozen services the first is rarely available, so the second is
what makes the cycle possible at all. The generated container already provides
it:

```go
app, err := diogen.InitAppWith(diogen.Replacements{ /* the cut */ })
```

Every field of `Replacements` is a seam — a place a test *may* fill in instead of
letting the container build it, if `replacements.allow` in `specrig.yaml` grants
it. See [DI overview](https://gitlab.com/gorib/waffle/-/blob/main/docs/di/overview.md#testing-with-the-container) for the
mechanics and
[`go tool specrig replacements`](commands.md#go-tool-specrig-replacements) for the
tooling.

### Cut at the leaves

Where to cut is a question with its own page —
[Component harness](harness.md#choosing-where-to-cut) has
the `# leaf` marker, the two directions in which it misses the real process
boundary, and which stand-in each interface gets. What matters *here* is why the
specification depends on the answer.

A specification is stated at the boundary of the service: given this state, when
this call arrives, then this happens. It is satisfied only if the code between
the call and the outcome actually runs. So:

- a **component test** replaces leaves and lets the rest run for real. That is
  the only arrangement in which the specification is checking what it claims to.
- replacing an **interior** seam deletes the subtree below it — which is the code
  the specification was written about. The suite stays green, the specification
  stops being evidence, and nothing about the diff says so. It is a unit test
  wearing a component test's clothes.
- a **unit test** builds its subject directly and never touches `Replacements`,
  so the permission list has nothing to say about it.

Two consequences are the specification's own, not the harness's.

**The negative assertion is written, never implied.** `.Times(0)`, rather than
leaving the call undeclared and letting the controller fail on it. The suite is
a document somebody approves, and an assertion that exists only as a missing
line cannot be approved — the reviewer would have to notice an absence.

**What is deliberately not replaced is stated too.** A port whose production
implementation already has a null variant selected by data needs no stand-in; if
that is not written down, the next reader cannot tell a decision from an
oversight, which is the same failure in a different place.

None of it is derivable, which is why `replacements.allow` is curated by a
person and the marker is a fact about the graph rather than a recommendation.

## What it costs

**Not the thinking.** If you already read incoming requirements, fit them into
the project, and find the contradictions before writing code, this cycle adds no
new step — it externalizes one you were doing in your head. The delta is the cost
of writing it down.

**Writing it down buys two things** that the in-your-head version cannot: an
agent can work against it unsupervised, and a second person can do the
verification later. It is also what onboarding cannot otherwise transfer — a new
joiner reading code infers intent, while a specification shows which decisions
were deliberate.

The two artifacts do not end up in the same place, and should not. The
specification is about the code, is read as current, and belongs in the
repository — with the reasoning for each case written into it, which is the part
a new joiner needs. The delta list is about the *requirement*: a record of what
the document left open and what was decided, addressed to whoever wrote it. It
belongs with the ticket. Committing it would put a negotiation that was finished
at approval into a place where everything is read as still true.

**The ceiling moves to verification.** Team output becomes bounded by how many
specifications one person can carefully approve, and that does not improve by
adding agents. It is a better ceiling than code review — a misunderstanding
caught in a specification is cheaper than one caught in a merge request — but it
is a ceiling, and it is lower than people expect.

## Opting out

Rule #11 is a convention, not a lock. A service can decline it, and that call
belongs to whoever owns the repository. A rule its owner cannot switch off does
not produce the work it describes — it produces cases written to satisfy a file.

The mechanism already exists and needs nothing built: `CONTRIBUTING.md` overrides
`AGENTS.md` wherever the two disagree, as the generated file's own preamble says,
and `waffle agents` never touches it. So declining is a paragraph there, and it
survives every upgrade.

**The wording is yours.** How you instruct an agent in your own project is not
the framework's business, and nothing here prescribes a form of words. Two
properties of it are worth knowing, because they are what the agent side is told
to look for:

- **An opt-out has to be about this rule.** An agent is instructed not to infer
  one from general remarks about pace — "we move fast here", "tests come after" —
  so a paragraph meant to switch the workflow off has to say that it does. This
  constrains how strictly an agent reads, not how you write.
- **Nothing else falls with it.** The platform boundary is not part of Rule #11:
  `replacements.allow` is enforced by a test in the ordinary suite, and an agent
  may no more grant itself a seam in traditional mode than in this one. Turning
  the workflow off does not hand over the cut.

**Say what replaces it.** "Traditional mode" is not one protocol. If an approved
specification stops being the artifact that says what *correct* means, something
else has to — at a minimum, a test for the behaviour that changed. Leaving that
open reproduces the failure this cycle exists to prevent, by policy instead of by
accident: a production change nothing pins, which the next reader takes for a
decision because the code is the only thing left to read.

**It is not binary.** The rule already exempts two categories on its own — a
code defect at the level of a function takes a table test where the defect is,
and a spike is exempt entirely. Keeping the cycle for features and design
defects while dropping it elsewhere is a smaller step than switching it off, and
it is the shape most teams actually want.

**A repository file and a task are different scopes.** `CONTRIBUTING.md` says how
this service lives, always, and a session cannot choose otherwise. "Quickly, just
this once" is a property of one task and belongs in the task. Putting it in the
repository turns a standing decision into a switch for whatever mood the day is
in.

**What declining gives up.** On the first service taken through the cycle end to
end, writing the cases surfaced defects in code that was already in production: a
grid keeping the wrong version, freeze boundaries off by a day at both ends, a
missing facility requirement, a currency anchored to the wrong moment, a promo
window short by its last day, and a product kind that could be changed after the
fact. None of them needed a test run to find. They came out of having to state,
per case, what the answer should be — which is the part traditional mode removes,
and it is not the same as the part that runs in CI.

## What this does not cover

A component test with the edges cut verifies one service against its *assumed*
contract with the others. The expensive failures in a fleet are contract drift
and retry or ordering semantics between services — precisely what the cut removes
from the test. This raises confidence inside a service and says nothing about the
seams between them; that needs contract tests or protobuf compatibility checks,
which are a separate mechanism.

## Status

Honest inventory, because a workflow documented ahead of its tooling is a
workflow that gets abandoned at the first step that does not work.

**Available:**

- `go tool specrig harness` generates the harness from the container — the
  database, the transport and its clients, the connections that must be dead,
  the environment — and regenerates it on every `make generate`. Its first run
  scaffolds the suite's `TestMain`, the boot smoke test, the migrations check
  and `TestReplacementsStayInsideTheList`.
- `replacements.Check()`, scaffolded as a test beside the container, enforces
  `replacements.allow` — what a test may replace is a decision someone made, not
  a habit — and `go tool specrig replacements` prints everything the containers
  expose, with `check` for the same comparison on demand.
- Rule #11 in `specrig.md`, which is phrased over "executable specification"
  rather than any one format, so it is followable today with ordinary Go tests.
- Rule #11's modes, `go tool specrig rules`, and the Claude Code plugin that
  enforces them — see [specrig](https://github.com/specrig/specrig).
- **the component harness**, as libraries — see
  [Component harness](harness.md). The resource seam is
  specrig-go's `resource`; postgres, redis and elasticsearch are `sql/sqltest`,
  `redis/redistest` and `elastic/elastictest`; the in-process servers are
  `waffle/tools/grpc.Serve`,
  chaining interceptors with the same `UnaryChain` the daemon uses, and
  `waffle/tools/http.Serve` over the container's own mux and routers; the leaves
  that used to be hand-written per service — a logger, a consumer, a dead
  connection — are `pry.Recorder()`, `scribus.Recorder()` and `grpcClient.Dead()`.
  Services run on them, which is what puts this under "available" rather than
  under the list below. A run needs no configuration at all: without a DSN the
  harness starts a postgres container and the next run reuses it.

**Not available yet:**

- **a check that behaviour did not change without its specification.** Decidable
  from a diff: domain service or model files changed, and no `.feature` or
  baseline did. It fails on the case that actually happens — code edited in a
  hurry, no specification touched — and asks for nothing when nothing is wrong.
  Not written yet.
- **the task type, declared by the human.** A code defect and a spike take no
  cycle, and the agent still decides which a task is — so a feature relabelled
  as a code defect reaches production code with no mode switched. It no longer
  reaches the specification: with no mode on, the cases are closed.
- **enforcement in other tools.** Kiro, Antigravity and the rest read
  `AGENTS.md` and hold the modes themselves; `go tool specrig rules` prints plain
  JSON, so an adapter for any of them reads the same rules.

**Deliberately not a gate.** Rule #11 is satisfied by a person reading a
specification and judging it, and no pipeline can check that a person did. A
pipeline can check that a *string* is present, which is worse than checking
nothing: it trains everyone to paste a link, and then the merge request carries a
"specification approved" mark that nobody consciously gave. Approval is an act
inside a session; what has to survive the session is the specification itself, in
the repository, and a link to the ticket where the delta was argued. Only the
first of those is worth a machine check, and the bullet above is its honest form.

**Deliberately unchecked: writes through the shell.** The plugin checks the
editing tools, and a file rewritten by a shell command passes it. Comparing the
tree before and after each command would catch the write only after it happened,
and a parallel session in another mode writes to the same tree legitimately — the
check would report on correct work as often as on a violation.

What the first services through this showed, worth knowing before yours: the
expensive parts were not the runner but the decisions around it — binding at the
declared contract rather than one layer below, and noticing that most cases need
no clock at all if fixture dates are set relative to now. Those are written down in Rule #11 so
they are not rediscovered.

The extraction itself taught one thing worth repeating: **what deserved to be
shared was not the silent stand-in but the recording one.** A silent leaf is
three lines written on the spot; a spy is the part that gets rewritten badly —
and every service that needed one wrote its own until these existed. `Noop()` is
a recorder nobody queries.
