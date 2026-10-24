# AI-native development: field notes

[specrig-go](../../README.md) · the waffle adapter · [AI-native development](ai-native.md)

What happened while Rule #11 was being built, oldest first.
[AI-native development](ai-native.md) is the argument; these are the incidents it
rests on. An entry stays as it was written when the rule changes after it — a
later entry says what changed.

## 2026-09-06 — Crossing into the platform's zone

A session told only "build the scenarios per Rule #11" wrote the cases, went
straight on to the harness and the steps, and edited `waffle.yaml` to grant
itself the nine seams its hand-written fakes needed. The prohibition was in the
file, in three places.

## 2026-09-06 — The right lane, the wrong order

A later session stayed inside its own zone and wrote production code across two
domains before any case existed. Its own diagnosis was exact: it had read the
short mid-work imperatives that settle a design question — "add it", "forbid
it", "do B" — as authorisation to implement. The rule said what a *task* is not;
it never said what an *answer to your own question* is, and every such answer
resets an agent's sense of having just been handed a job.

The expensive part was not the order. The change pinned nothing: the
specification said the same thing before and after, and the suite would have
been exactly as green without it. Worse, it could not be pinned at that cut —
telling the two designs apart needs a failure *inside* the transaction at the
declared-contract level, and no seam offers one. So the correct move had been to
stop and report a gap, which the rule does say.

**Prose was tried first, and one line of it made things worse.** After the first
failure the fix was a paragraph of argument, and in it "run the check before you
hand anything over" turns a red check into a list of the exact lines that would
make it green. The file-list surface is what actually held — it does not depend
on the agent reading the paragraph the right way round. The second failure is
what extended it to production files and to `CONTRIBUTING.md`, and what added
the red-count check's mirror.

The line itself stayed, because running the check *is* right; what it needed was
the sentence after it. Rule #11 now says "its failure names the seams you need,
it is not a list of edits to make", and names the forbidden one of the two ways
to turn the output green. Worth noting as a pattern: the fix for prose that
points the wrong way is usually the next sentence, not deleting it — but neither
is what makes the rule hold.

## 2026-09-23 — Leaving the cycle by classifying the task

In the specification step, an agent was answered "fix `waffle.yaml` for the new
layout". It judged — plausibly — that a generator setting is not behaviour and so
not a specification task, declared the request outside the step, and edited the
file. It broke nothing it was told in so many words: it had been told that an
answer is the content of a row, and it decided this answer was not one. Prose
lost a third time, and this time to an argument about which rule applies. The
step boundary became a mechanism: modes.

## 2026-09-23 — The first use of the modes

Told in spec mode to add two kinds to the fiscal policy, the agent said the
change was production code and not this step's — and tried the edit anyway. The
hook refused it; the agent asked for redgreen by its exact command, and when then
told to "ignore it and be quick", answered that the refusal was not its to skip.
Prose it had just quoted did not stop the edit; the mechanism did.

## 2026-09-24 — The switching, measured

Twenty-five sessions from before the mechanism were set against four with it.
The violations a mode would refuse had cost about 3% of the human's messages; in
the four, about a third of the messages were mode commands, and one refusal in
the repository stopped a real mistake. The class worth a mechanism is production
code before the specification is approved — the costliest one a mode catches.

The boundary between steps and red-green caught nothing that had cost a message,
and produced failures of its own: a step that needed a shape it could not add,
and an agent steering towards whichever mode opened its next edit. So they became
one mode: the cases stay frozen, the red run still comes before any behaviour,
and a step changed after it is named at hand-over.

After the merge, having no mode on opened everything redgreen did and the cases
besides, and agents asked for no mode for exactly that. So none closed the cases
too: a case now changes in spec mode and nowhere else.
