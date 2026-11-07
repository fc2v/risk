package rules

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"testing"

	"risk/risk/contract"
)

// Every reason must be produced by some rule test, so each promised evidence contract is checked.
func TestMain(m *testing.M) {
	code := m.Run()
	if code != 0 || flag.Lookup("test.run").Value.String() != "" {
		os.Exit(code)
	}

	var missing []string
	for r := range contract.PromisedEvidence {
		if !producedReasons[r] {
			missing = append(missing, string(r))
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		fmt.Fprintf(os.Stderr, "no rule test produces these reasons, so their evidence contract is unchecked: %v\n", missing)
		os.Exit(1)
	}
	os.Exit(0)
}
