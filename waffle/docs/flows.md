# Flows

[specrig-go](../../README.md) · the waffle adapter

A flow is what happens after one way in: the route, command or message from outside, every service it reaches, what each sends on, and in which of its cases. Waffle writes flows from two records and never from prose:

| file | written from | written by |
|---|---|---|
| `tests/surface.yaml` | the service's code: what it publishes, consumes, serves and calls | `go tool specrig surface`, which the `generate` target runs |
| `tests/flows.yaml` | the component suite: every input a case sent, and what the service sent on | `harness.Main`, after every whole run of the suite |
| an index and one page per input, beside it | the two above, for every service below a root | `go tool specrig flows` |

Each is an artifact, like `AGENTS.md`: whatever has its source rewrites it, and nothing compares it on the way. A stale one is a diff, so CI runs `make generate` and `make test` — and, in a monorepo, `go tool specrig flows` — then `git diff --exit-code`.

The records are specrig's format, which a web app's harness writes too. Each says so first, as an OpenAPI document does under `openapi:` — `specrig: surface/1`, `specrig: flows/1` — and what a record cannot learn from the code goes into `specrig.yaml` beside it, which exists only where there is something to declare.

The code says what *can* happen, the suite what the cases *make* happen. Each alone misleads: the code cannot tell a path that runs from one that cannot, and the suite is silent about what no case reaches. Read together, the difference between them is the list of gaps.

## The code's record

```
go tool specrig surface
```

writes `tests/surface.yaml` from the module's code; the `generate` target runs it, so the record follows every edit. What is recorded is what the libraries a service talks through receive as the name of a message:

| | recognised at |
|---|---|
| publishes | `outbox.Publisher.Publish`/`Enqueue`, `amqp.Publisher.Publish`, the `amqp091` channel's `Publish*` |
| consumes | `amqp` `Bind`, the `amqp091` channel's `QueueBind` |
| serves | `grpc` `RegisterService` (by the `_ServiceDesc` it is given), and every route of a `gitlab.com/gorib/http/server` mux under its group prefixes |
| calls | `grpc` `Invoke` and `NewStream` — a generated client's own calls — and requests through `gitlab.com/gorib/http/client`, a variable path segment written `{}`, a path with no constant part at all written `external` |
| domains | in a modular monolith, a call from `domain/A/adapter` to a `domain/B/service` type the container wires in: `rent → invoice: Invoices.Issue` |

A service's own interface in front of one of those — its `Publisher` over the outbox — counts as the library, and a wrapper function is followed to its callers. A name that is not a constant at the end of that chain is refused: it is a contract, and a contract has a name. The one exception is an HTTP request whose whole address comes from data — a supplier's file URL — which is recorded as `GET external` and lands outside the set.

A group prefix configuration sets is the other exception, and it is declared: the router's field goes under `flows.prefixes` in `specrig.yaml`, keyed as the refusal names it, and the routes under it are recorded under `{}`:

```yaml
# specrig.yaml
flows:
  prefixes:
    - transport/http.router.prefix
```

`GET {}/*` is a way in no request of the set lands on, since where it sits cannot be told. The record refuses a key no group prefix reads, so one left behind by a rename fails rather than lingers.

A record may also group HTTP requests by the client that sends them, under `clients`, keyed by where the client lives in the code:

```yaml
clients:
  lib/http-client.httpClient:
    - GET /customers/contracts/{}
    - GET /rest/prices
```

A client has one base URL, and the base URL is configuration, so no record says where it points. Every client still has a **host**, and the check fails on one without:

- **told by the set** — the service whose routes take the most of the client's requests. Each of them lands there alone: a wildcard route of another service that happens to fit one does not compete.
- **named in the caller's `specrig.yaml`** — for a party outside the set, and for the last resort inside it: two services the set cannot tell apart, though the domains they live on can.

```yaml
# specrig.yaml
flows:
  clients:
    domain/template/adapter.bladeRunner.client: bladerunner
    domain/catalog/adapter.download.client: suppliers   # any URL from data
    lib/http-client.httpClient: apostle                 # only on a tie
```

The key is the one the record writes, so it is copied, not made up. A name outside the set is a party, and its requests are reported outside under it; a service of the set is the host its requests are checked against. The check refuses a name it does not need — the set tells the same host, or the record has no such client — so a declaration exists only where there is no way out, and one left behind by a rename fails rather than lingers.

The Go extraction does not group yet, so its requests are matched one by one and one no service routes lands outside without failing; a record written by another extractor — a frontend's — may group.

## The suite's record

The generated harness traces everything that crosses the service's edge during the suite: the calls the suite sends to its server, the calls the service makes to what a scenario replaced, the events it publishes and consumes, and the commands a step runs. A godog suite calls

```go
func initialize(sc *godog.ScenarioContext) {
	harness.Steps(sc)
	// …
}
```

so that a case is known by its file as well as its title, and only its `When` counts as its input — a `Given` that calls the service sets up, a `Then` checks. Without it every call is an input.

`harness.Main` rewrites `tests/flows.yaml` from the trace after every whole run of the suite, a failing one included: it records what the run saw, and the passing run after it records that. A narrowed run — `-run`, `-skip`, `-godog.tags`, `-godog.paths` — saw part of the service and writes nothing.

The file lists every input, and under each its **branches**: the cases that, having received it, sent the same messages on in the same order, or were refused the same way. A refusal is named by what a case asserts about it — `NotFound contract_not_found`, `HTTP 403 forbidden` — not by its message, which carries ids. A block sent again straight after itself is one pass of a loop, so the number of items a case had does not split its branch; a message sent again after others is a step of its own. A case in two branches — a scenario that received the input twice by different paths — is two cases.

A branch also records the fields of what it sent, by message, under `sent`. The input records under `needs`, by message it sends, the fields that split its answered branches exactly: carried in every case of the branches sending the message, in no case of the others. Anything weaker is the fixtures' habits — a `comment` most cases fill, a unit most cases name while a promo is redeemed in one of them — and would make a need of it. A refusal is left out, since any body can be refused. Values are never compared: two suites' fixtures share field names through the contract, not values.

`description` fields are the only thing written by hand. They are kept by anchor across rewrites; one whose input or branch is gone moves to `orphaned` and returns when its anchor does, so a failing run loses none. While `orphaned` holds anything the run fails, until somebody deletes it or moves it.

## The project's flows

```
go tool specrig flows <root> <index.yaml>
```

finds every service below `root` by its records the way `go test ./...` finds packages — hidden directories, `vendor`, `node_modules` and `testdata` left out — takes them as the whole system, and writes the index and the pages beside it, removing a page no flow writes any more and naming each link between services that was lost or appeared. No file marks a service: a directory with a record is one. A file at a record's path that does not open with `specrig:` is somebody else's, and is named and passed over; one that does and cannot be read fails the command. A flow starts at every way in: a route, a command, or a message no traced service sends. A monorepo keeps them in its own directory — `.docs/flows/` — and runs the command from its root; the index is the project's, not a service's.

**Pages.** One per flow, and one per input a flow reaches. A flow's page draws the sequence, each step's sends nested under the arrow that reached it and inside the condition — "in 2 of 3 cases of step 1" — that made them; an event a service consumes itself is marked. The page lists the cases of its own input with their comments from the `.feature` file; a step further on shows its branches and how many cases each has, and links to its own page, so a step many flows share — authentication, say — is written once.

A step further on is drawn with what this flow can make it send. A message that needs a field nothing reaching the step carries — a saved card's `methodId`, when the flow pays with a new card — is folded to a note on the step and a line under its branches, and is not followed. The note says what the cases show, not what the code allows: either the message cannot be sent here, or the receiver has no case sending it with what this flow carries. It is folded, not dropped, so the second reading stays on the page as a gap to close.

**The index** holds, besides the flows:

| section | what it is |
|---|---|
| `links` | every message one traced service sends and another receives; `notes` names fields the receiver's cases send and the sender's never do |
| `untested` | a link the code has between two traced services and no case follows |
| `outside` | what a service sends that no traced service received: taken by nobody's case, taken only by a wildcard subscription, or out of the system |
| `system` | the records against each other, below |
| `orphaned` | a flow's `title` or `description` whose flow is gone; it returns with the flow, and fails the command meanwhile |

`system` fails the command on an event published and consumed by nobody, subscribed to and published by nobody, an RPC called and served by nobody, served and called by nobody, a client with no host, a client's request its host does not route, an HTTP request more than one service routes, or a host named without need. It only reports an event taken by a wildcard alone, a service consuming its own event, and an HTTP request no service routes: each of those is a legitimate design.

## What is not there

- **A party outside waffle.** A service with no records is not in the set: an RPC only it calls is "served, nobody calls", and an HTTP request to it leaves the system — both true of the records, which is all the check reads. A link is never declared by hand, in config or in code — a declared link is one more thing to forget — except a client's host the set cannot tell, above, which the check fails without.
- **Outgoing HTTP in a trace.** The trace sees the service's own server and the calls a scenario replaced; a request to another system goes through a client the harness does not wrap, so the pages show it only where the code's record names it.
- **Paths no case takes.** A branch in the code that no case reaches is in neither record. Coverage of the suite over the production packages is the tool for that.
