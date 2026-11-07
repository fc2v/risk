//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/lib/pq"
)

const (
	rebalanceAccounts = 60
	rebalanceTimeout  = 2 * time.Minute
)

func TestGracefulLeaveLosesNothing(t *testing.T) {
	testRebalance(t, func(inst *instance) {
		if err := inst.stop(); err != nil {
			t.Errorf("%s exit after SIGTERM: %v, want clean exit", inst.name, err)
		}
	})
}

// A killed member's partitions move only after its session times out, so this takes about a minute.
func TestCrashLosesNothing(t *testing.T) {
	testRebalance(t, (*instance).kill)
}

type expectedTx struct {
	account string
	id      string
	risk    string
}

// Each account gets three small spends then a large one. The large one is MEDIUM only if the small
// ones were evaluated first, so a broken per-account order shows up as LOW.
func testRebalance(t *testing.T, disrupt func(*instance)) {
	base := time.Now().Add(-time.Hour)
	accounts := make([]string, rebalanceAccounts)
	steps := make([][]txPayload, 4)
	var expected []expectedTx
	for a := range accounts {
		accounts[a] = newAccount()
		for s := range steps {
			amount, risk := int64(-10_000), "LOW"
			if s == 3 {
				amount, risk = -50_000, "MEDIUM"
			}
			tx := cardTx(accounts[a], amount, base.Add(time.Duration(s)*time.Minute))
			steps[s] = append(steps[s], tx)
			expected = append(expected, expectedTx{accounts[a], tx.id(), risk})
		}
	}

	produceSteps(t, steps[:2])
	eventually(t, rebalanceTimeout, "some txs to be published", func() bool {
		return publishedCount(accounts) >= rebalanceAccounts/2
	})

	victim := suite.instances[1]
	disrupt(victim)
	produceSteps(t, steps[2:])

	replacement, err := startInstance(victim.name + "-replacement")
	if err != nil {
		t.Fatal(err)
	}
	suite.instances[1] = replacement

	eventually(t, rebalanceTimeout, "every tx to be published", func() bool {
		for _, e := range expected {
			if len(evaluatedTx(e.account, e.id)) == 0 {
				return false
			}
		}
		return true
	})

	for _, e := range expected {
		for _, p := range evaluatedTx(e.account, e.id) {
			if p.Risk != e.risk || p.FailureReason != "" {
				t.Errorf("tx %s published as %s (failureReason %q), want %s", e.id, p.Risk, p.FailureReason, e.risk)
			}
		}
	}
	assertStoredOnce(t, accounts, expected)
}

func produceSteps(t *testing.T, steps [][]txPayload) {
	t.Helper()
	for _, step := range steps {
		for _, tx := range step {
			produce(t, tx["accountId"].(string), tx.toJSON())
		}
	}
}

func publishedCount(accounts []string) int {
	n := 0
	for _, acc := range accounts {
		n += len(evaluatedFor(acc))
	}
	return n
}

func assertStoredOnce(t *testing.T, accounts []string, expected []expectedTx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := suite.db.QueryContext(ctx,
		`select id, risk from evaluated_txs where account_id = any($1)`, pq.Array(accounts))
	if err != nil {
		t.Fatalf("query stored txs: %v", err)
	}
	defer rows.Close()

	stored := make(map[string]string)
	for rows.Next() {
		var id, risk string
		if err := rows.Scan(&id, &risk); err != nil {
			t.Fatal(err)
		}
		stored[id] = risk
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(stored) != len(expected) {
		t.Errorf("stored %d txs, want %d", len(stored), len(expected))
	}
	for _, e := range expected {
		if risk, ok := stored[e.id]; !ok || risk != e.risk {
			t.Errorf("tx %s stored as %q (found %v), want %s", e.id, risk, ok, e.risk)
		}
	}
}
