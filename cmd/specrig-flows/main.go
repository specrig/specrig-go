// Command specrig-flows assembles a project's flows from its services' records,
// whatever stack each service is on.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/specrig/specrig-go/flows"
)

const usage = `usage: specrig-flows <root> <index.yaml>

Every service below root, a directory holding tests/surface.yaml or
tests/flows.yaml, is taken as the whole system. The index is written to
index.yaml and a page per flow beside it; a page it wrote before and would not
write now is removed. It fails on prose that lost its flow and on what the
services' records leave unclosed.`

func main() {
	args := os.Args[1:]
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println(usage)
		return
	}
	if len(args) != 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err := flows.Run(args[0], args[1], os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
