// Command specrig is specrig's tool for a waffle service: go tool specrig rules|agents.
package main

import (
	"os"

	"github.com/specrig/specrig-go/cli"
	"github.com/specrig/specrig-go/waffle"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], waffle.Preset()))
}
