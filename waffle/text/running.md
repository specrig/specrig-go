## Running

`go test ./...` runs the suite with the Go on this machine, and starts the
resources it needs in Docker. `go generate ./...` rewrites
`tests/internal/harness/harness_gen.go` from the container, and
`go tool specrig surface` rewrites `tests/surface.yaml` from the code. A project that rewrites them in CI and
then runs `git diff --exit-code` sees one that fell behind as a diff.

`tests/flows.yaml` is what the cases make the service send: every `When` step's call
is an input, and what left the service after it is what it sent. Only the
`description` fields are written by hand: they survive a rewrite, and one whose step
is gone moves to `orphaned` and fails the run until it is moved or deleted.
