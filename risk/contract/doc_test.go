package contract

import (
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"risk/risk"
)

var docTypes = map[string]EvidenceType{
	"int": EvidenceInteger, "num": EvidenceNumber, "bool": EvidenceBoolean, "str": EvidenceString, "strs": EvidenceStrings,
}

func TestContractDocMatchesPromisedEvidence(t *testing.T) {
	raw, err := os.ReadFile("../../docs/contract.md")
	if err != nil {
		t.Fatal(err)
	}

	row := regexp.MustCompile("^\\| `([A-Z_]+)` \\| (yes|no) \\| (.+) \\|$")
	key := regexp.MustCompile("`([A-Za-z]+)` (int|num|bool|strs|str)\\b")

	documented := map[risk.Reason]map[string]EvidenceType{}
	var previous map[string]EvidenceType
	for line := range strings.Lines(string(raw)) {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		keys := map[string]EvidenceType{}
		switch m[3] {
		case "same as above":
			keys = previous
		case "none":
		default:
			for _, k := range key.FindAllStringSubmatch(m[3], -1) {
				keys[k[1]] = docTypes[k[2]]
			}
		}
		documented[risk.Reason(m[1])] = keys
		previous = keys
	}

	for reason, promised := range PromisedEvidence {
		got, ok := documented[reason]
		if !ok {
			t.Errorf("%s is missing from docs/contract.md", reason)
			continue
		}
		if !maps.Equal(got, promised) {
			t.Errorf("%s: docs/contract.md promises %v, code promises %v", reason, sorted(got), sorted(promised))
		}
	}
	for reason := range documented {
		if _, ok := PromisedEvidence[reason]; !ok {
			t.Errorf("docs/contract.md lists %s, which isn't a reason code", reason)
		}
	}
}

func sorted(m map[string]EvidenceType) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+" "+string(v))
	}
	slices.Sort(out)
	return out
}
