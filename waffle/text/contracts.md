**One specification per declared contract, not per service.** The
`interface{ A; B; C }` entry that `go tool specrig replacements`
reports for a shared container field *is* the list of contracts a domain owes —
one constituent per caller, each a place a scenario binds. Cases written
against the gRPC operations alone cover one of them.
