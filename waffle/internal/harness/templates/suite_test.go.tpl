package tests

import (
	"testing"

	"{{.Module}}/tests/internal/harness"
)

func TestMain(m *testing.M) {
	harness.Main(m)
}
