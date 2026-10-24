package tests

import (
"testing"

"{{.Module}}/tests/internal/harness"

"{{.Container}}"
)

// Before any case: one run says what the cut is missing, instead of an hour
// inside a half-written suite. It asserts nothing about behaviour.
func TestBoot(t *testing.T) {
ctx := t.Context()
if err := harness.Reset(ctx); err != nil {
t.Fatalf("resources: %v", err)
}

app, err := harness.Boot(ctx, {{.Package}}.Replacements{})
if err != nil {
t.Fatalf("boot: %v", err)
}
t.Cleanup(func() {
if err := app.Shutdown()(); err != nil {
t.Errorf("shutdown: %v", err)
}
}){{if or .Grpc .Http}}

clients, err := app.Serve()
if err != nil {
t.Fatalf("serve: %v", err)
}
t.Cleanup(clients.Close){{end}}
}
