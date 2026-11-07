//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"
)

const waitTimeout = 30 * time.Second

type storedTx struct {
	ID          string    `json:"id"`
	Amount      int64     `json:"amount"`
	Risk        string    `json:"risk"`
	EvaluatedAt time.Time `json:"evaluatedAt"`
}

func TestCardTxEvaluatedAndStored(t *testing.T) {
	acc := newAccount()
	tx := cardTx(acc, -5000, time.Now().Add(-time.Minute))
	produce(t, acc, tx.toJSON())

	got := waitPublished(t, acc, tx.id())
	if got.txIDHeader != tx.id() || got.AccountID != acc {
		t.Errorf("published with tx-id header %q, accountId %q; want %q, %q", got.txIDHeader, got.AccountID, tx.id(), acc)
	}
	assertRisk(t, got, "LOW", 0)
	if got.FailureReason != "" {
		t.Errorf("failureReason = %q, want none", got.FailureReason)
	}
	if len(got.RuleOutcomes) != 1 || got.RuleOutcomes[0].Rule != "highAmountSpend" || got.triggered("highAmountSpend") ||
		got.RuleOutcomes[0].Reason != "AMOUNT_WITHIN_THRESHOLD" {
		t.Errorf("ruleOutcomes = %+v, want highAmountSpend not triggered with AMOUNT_WITHIN_THRESHOLD", got.RuleOutcomes)
	}

	stored := waitStored(t, tx.id())
	if stored.Risk != "LOW" || stored.EvaluatedAt.IsZero() {
		t.Errorf("stored = %+v, want risk LOW with evaluatedAt", stored)
	}
}

func TestHistoryRaisesRisk(t *testing.T) {
	acc := newAccount()
	base := time.Now().Add(-time.Hour)
	var payloads [][]byte
	for i := range 3 {
		payloads = append(payloads, cardTx(acc, -10_000, base.Add(time.Duration(i)*time.Minute)).toJSON())
	}
	large := cardTx(acc, -50_000, base.Add(10*time.Minute))
	produce(t, acc, append(payloads, large.toJSON())...)

	got := waitPublished(t, acc, large.id())
	assertRisk(t, got, "MEDIUM", 40)
	if !got.triggered("highAmountSpend") {
		t.Error("highAmountSpend not triggered for 5x the median spend")
	}
}

func TestFreshPayeeLargeTransferIsHigh(t *testing.T) {
	acc := newAccount()
	postedAt := time.Now().Add(-time.Minute)
	payeeAddedAt := postedAt.Add(-10 * time.Minute)
	tx := transferTx(acc, -1_500_000, postedAt, &payeeAddedAt)
	produce(t, acc, tx.toJSON())

	got := waitPublished(t, acc, tx.id())
	assertRisk(t, got, "HIGH", 75)
	if !got.triggered("highAmountSpend") || !got.triggered("freshPayee") {
		t.Errorf("ruleOutcomes = %+v, want highAmountSpend and freshPayee triggered", got.RuleOutcomes)
	}
}

func TestNoRuleAppliesIsUnknown(t *testing.T) {
	acc := newAccount()
	tx := transferTx(acc, 20_000, time.Now().Add(-time.Minute), nil)
	produce(t, acc, tx.toJSON())

	got := waitPublished(t, acc, tx.id())
	assertRisk(t, got, "UNKNOWN", 0)
	if got.FailureReason != "" || len(got.RuleOutcomes) != 0 {
		t.Errorf("got failureReason %q, ruleOutcomes %+v; want neither", got.FailureReason, got.RuleOutcomes)
	}
	if stored := waitStored(t, tx.id()); stored.Risk != "UNKNOWN" {
		t.Errorf("stored risk = %q, want UNKNOWN", stored.Risk)
	}
}

func TestDuplicateRecordEvaluatedOnce(t *testing.T) {
	acc := newAccount()
	tx := cardTx(acc, -5000, time.Now().Add(-2*time.Minute))
	sentinel := cardTx(acc, -100, time.Now().Add(-time.Minute))
	produce(t, acc, tx.toJSON(), tx.toJSON(), sentinel.toJSON())

	waitPublished(t, acc, sentinel.id())
	if n := len(evaluatedTx(acc, tx.id())); n != 1 {
		t.Errorf("tx published %d times, want 1", n)
	}
}

func TestConflictingTxFailsThrough(t *testing.T) {
	acc := newAccount()
	tx := cardTx(acc, -5000, time.Now().Add(-2*time.Minute))
	conflicting := tx.with("amount", -9000)
	sentinel := cardTx(acc, -100, time.Now().Add(-time.Minute))
	produce(t, acc, tx.toJSON(), conflicting.toJSON(), sentinel.toJSON())

	waitPublished(t, acc, sentinel.id())
	copies := evaluatedTx(acc, tx.id())
	if len(copies) != 2 {
		t.Fatalf("tx published %d times, want 2 (evaluated, then conflict)", len(copies))
	}
	assertRisk(t, copies[0], "LOW", 0)
	assertRisk(t, copies[1], "UNKNOWN", 0)
	if copies[1].FailureReason != "CONFLICT" || copies[1].Amount != -9000 {
		t.Errorf("second copy = %+v, want failureReason CONFLICT with the conflicting amount", copies[1])
	}

	assertDeadLettered(t, acc, conflicting.toJSON(), "CONFLICT")
	if stored := waitStored(t, tx.id()); stored.Amount != -5000 {
		t.Errorf("stored amount = %d, want the original -5000", stored.Amount)
	}
}

func TestInvalidTxFailsThrough(t *testing.T) {
	acc := newAccount()
	invalid := cardTx(acc, -5000, time.Now().Add(-2*time.Minute)).with("mcc", "abc")
	sentinel := cardTx(acc, -100, time.Now().Add(-time.Minute))
	produce(t, acc, invalid.toJSON(), sentinel.toJSON())

	waitPublished(t, acc, sentinel.id())
	copies := evaluatedTx(acc, invalid.id())
	if len(copies) != 1 {
		t.Fatalf("invalid tx published %d times, want 1", len(copies))
	}
	assertRisk(t, copies[0], "UNKNOWN", 0)
	if copies[0].FailureReason != "INVALID" {
		t.Errorf("failureReason = %q, want INVALID", copies[0].FailureReason)
	}

	assertDeadLettered(t, acc, invalid.toJSON(), "INVALID")
	waitStored(t, sentinel.id())
	if _, status := getStored(t, invalid.id()); status != http.StatusNotFound {
		t.Errorf("GET invalid tx = %d, want 404", status)
	}
}

func TestUnidentifiableRecordsOnlyDeadLettered(t *testing.T) {
	acc := newAccount()
	noID := cardTx(acc, -5000, time.Now().Add(-2*time.Minute)).without("id")
	sentinel := cardTx(acc, -100, time.Now().Add(-time.Minute))
	produce(t, acc, noID.toJSON(), []byte("not json"), sentinel.toJSON())

	waitPublished(t, acc, sentinel.id())
	if published := evaluatedFor(acc); len(published) != 1 {
		t.Errorf("published %d records for the account, want only the sentinel", len(published))
	}
	eventually(t, waitTimeout, "2 dead-lettered records", func() bool { return len(deadLetteredFor(acc)) >= 2 })
	if n := len(deadLetteredFor(acc)); n != 2 {
		t.Errorf("dead-lettered %d records, want 2", n)
	}
}

func TestListPagesNewestFirst(t *testing.T) {
	acc := newAccount()
	base := time.Now().Add(-time.Hour)
	var want []string
	var payloads [][]byte
	for i := range 5 {
		tx := cardTx(acc, -1000, base.Add(time.Duration(i)*time.Minute))
		want = append(want, tx.id())
		payloads = append(payloads, tx.toJSON())
	}
	slices.Reverse(want)
	produce(t, acc, payloads...)

	eventually(t, waitTimeout, "5 stored txs", func() bool {
		page, _ := listPage(t, acc, 100, "")
		return len(page) == 5
	})

	var got []string
	var pages int
	for cursor := ""; ; {
		page, next := listPage(t, acc, 2, cursor)
		pages++
		for _, tx := range page {
			got = append(got, tx.ID)
		}
		if next == nil {
			break
		}
		cursor = *next
	}
	if !slices.Equal(got, want) || pages != 3 {
		t.Errorf("paged %d pages of %v, want 3 pages of %v", pages, got, want)
	}
}

// Last in this file: it stops the shared Postgres.
func TestDBOutageDelaysButDoesNotFail(t *testing.T) {
	dockerCompose(t, "stop", "postgres")
	t.Cleanup(func() { dockerCompose(t, "start", "postgres") })

	eventually(t, waitTimeout, "readiness to report unavailable", func() bool {
		return suite.instances[0].readyStatus() == http.StatusServiceUnavailable
	})

	acc := newAccount()
	tx := cardTx(acc, -5000, time.Now().Add(-time.Minute))
	produce(t, acc, tx.toJSON())

	time.Sleep(3 * time.Second) // well past failAfterAttempts' backoff
	if published := evaluatedFor(acc); len(published) != 0 {
		t.Fatalf("published %+v while the DB was down, want nothing until it's back", published)
	}

	dockerCompose(t, "start", "postgres")
	for _, inst := range suite.instances {
		eventually(t, time.Minute, inst.name+" to be ready again", func() bool {
			return inst.readyStatus() == http.StatusOK
		})
	}

	got := waitPublished(t, acc, tx.id())
	assertRisk(t, got, "LOW", 0)
	if got.FailureReason != "" {
		t.Errorf("failureReason = %q, want none: an outage must not fail txs through", got.FailureReason)
	}
	if n := len(evaluatedTx(acc, tx.id())); n != 1 {
		t.Errorf("tx published %d times, want 1", n)
	}
}

func waitPublished(t *testing.T, account, txID string) publishedTx {
	t.Helper()
	eventually(t, waitTimeout, "tx "+txID+" to be published", func() bool { return len(evaluatedTx(account, txID)) > 0 })
	return evaluatedTx(account, txID)[0]
}

func waitStored(t *testing.T, txID string) storedTx {
	t.Helper()
	var stored storedTx
	eventually(t, waitTimeout, "tx "+txID+" to be stored", func() bool {
		var status int
		stored, status = getStored(t, txID)
		return status == http.StatusOK
	})
	return stored
}

func getStored(t *testing.T, txID string) (storedTx, int) {
	t.Helper()
	var body struct {
		EvaluatedTx storedTx `json:"evaluatedTx"`
	}
	status, err := suite.instances[0].getJSON("/v1/evaluated-txs/"+txID, &body)
	if err != nil {
		t.Fatalf("GET tx %s: %v", txID, err)
	}
	return body.EvaluatedTx, status
}

func listPage(t *testing.T, account string, pageSize int, cursor string) ([]storedTx, *string) {
	t.Helper()
	var body struct {
		EvaluatedTxs []storedTx `json:"evaluatedTxs"`
		NextCursor   *string    `json:"nextCursor"`
	}
	path := fmt.Sprintf("/v1/evaluated-txs?account_id=%s&page_size=%d", account, pageSize)
	if cursor != "" {
		path += "&cursor=" + cursor
	}
	status, err := suite.instances[0].getJSON(path, &body)
	if err != nil || status != http.StatusOK {
		t.Fatalf("GET %s: status %d, err %v", path, status, err)
	}
	return body.EvaluatedTxs, body.NextCursor
}

func assertRisk(t *testing.T, got publishedTx, risk string, score int) {
	t.Helper()
	if got.Risk != risk || got.Score != score {
		t.Errorf("tx %s: risk %s score %d, want %s score %d", got.ID, got.Risk, got.Score, risk, score)
	}
}

func assertDeadLettered(t *testing.T, account string, payload []byte, reason string) {
	t.Helper()
	eventually(t, waitTimeout, "a dead-lettered record", func() bool { return len(deadLetteredFor(account)) > 0 })
	recs := deadLetteredFor(account)
	if len(recs) != 1 || string(recs[0].Value) != string(payload) || header(recs[0], "error") == "" || header(recs[0], "failure-reason") != reason {
		t.Errorf("dead-lettered %d records, want 1 copy with failure-reason %s and an error header", len(recs), reason)
	}
}
