package replacements

import (
	"fmt"

	"github.com/specrig/specrig-go/waffle/internal/command"
	check "github.com/specrig/specrig-go/waffle/replacements"
)

func report(rep check.Report, err error) error {
	// Before the refusal, not instead of it: a grant naming a seam that no longer
	// exists is worth saying either way.
	for _, warning := range rep.Warnings {
		command.ReportWarning(warning)
	}
	if err != nil {
		return err
	}
	if rep.Granted == 0 {
		fmt.Println("# no test replaces anything — the containers are built for real")
		return nil
	}
	fmt.Printf("# %d replaced %s in replacements.allow\n", rep.Granted,
		plural(rep.Granted, "field, and it is", "fields, and all are"))
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
