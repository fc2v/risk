package contract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"risk/risk"
)

func TestEveryReasonHasPromisedEvidence(t *testing.T) {
	declared := declaredReasons(t)
	for _, r := range declared {
		if _, ok := PromisedEvidence[r]; !ok {
			t.Errorf("%s has no entry in PromisedEvidence", r)
		}
	}

	known := map[risk.Reason]bool{}
	for _, r := range declared {
		known[r] = true
	}
	for r := range PromisedEvidence {
		if !known[r] {
			t.Errorf("PromisedEvidence has %s, which reasons.go doesn't declare", r)
		}
	}
}

func declaredReasons(t *testing.T) []risk.Reason {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "../reasons.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var reasons []risk.Reason
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if typ, ok := spec.Type.(*ast.Ident); !ok || typ.Name != "Reason" {
			return true
		}
		for _, v := range spec.Values {
			if lit, ok := v.(*ast.BasicLit); ok {
				s, _ := strconv.Unquote(lit.Value)
				reasons = append(reasons, risk.Reason(s))
			}
		}
		return true
	})
	if len(reasons) == 0 {
		t.Fatal("found no Reason constants in reasons.go")
	}
	return reasons
}
