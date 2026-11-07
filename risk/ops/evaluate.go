package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"risk/risk"
	"risk/risk/contract"
	"risk/risk/engine"
)

const EvaluatedTxTopic = "txs.evaluated"

// Publish happens before insert, so a tx in the DB has already been published.
func EvaluateTx(ctx context.Context, b Backends, t risk.Tx) error {
	logger := b.Logger().With("txID", t.ID, "accountID", t.AccountID)

	processed, err := alreadyProcessed(ctx, b, t)
	if err != nil {
		return err
	}
	if processed {
		b.Metrics().TxAlreadyEvaluated()
		logger.Info("tx already evaluated, skipping")
		return nil
	}

	start := time.Now()
	w := b.ProfileWindow()
	w.At = t.PostedAt
	profile, err := b.Profiles().Load(ctx, t.AccountID, w)
	if err != nil {
		return err
	}
	start = stageDone(b, StageLoadHistory, start)

	res := b.Engine().Evaluate(t, engine.EvaluationContext{Profile: profile})
	et := risk.EvaluatedTx{Tx: t, Score: res.Score, Risk: res.Risk, RuleOutcomes: res.Outcomes}

	payload, err := json.Marshal(contract.FromEvaluated(et))
	if err != nil {
		return fmt.Errorf("encode evaluated tx: %w", err)
	}
	start = stageDone(b, StageEvaluate, start)

	msg := Message{Key: et.AccountID, ID: et.ID, Payload: payload}
	if err := b.Publisher().Publish(ctx, EvaluatedTxTopic, msg); err != nil {
		return fmt.Errorf("publish evaluated tx: %w", err)
	}
	start = stageDone(b, StagePublish, start)

	err = b.EvaluatedTxDB().Insert(ctx, et)
	if errors.Is(err, risk.ErrDuplicateTx) {
		// A concurrent delivery inserted it between our check and insert.
		_, err = alreadyProcessed(ctx, b, t)
	}
	if err != nil {
		return fmt.Errorf("insert published tx: %w", err)
	}
	stageDone(b, StageInsert, start)

	b.Metrics().TxEvaluated(et)
	logger.Info("tx evaluated", "score", et.Score, "risk", et.Risk)
	return nil
}

func stageDone(b Backends, stage string, start time.Time) time.Time {
	now := time.Now()
	b.Metrics().StageTook(stage, now.Sub(start))
	return now
}

// PublishFailure is not followed by an insert, so a later redelivery is evaluated afresh.
func PublishFailure(ctx context.Context, b Backends, t risk.Tx, reason risk.FailureReason) error {
	et := risk.EvaluatedTx{Tx: t, Risk: risk.RiskUnknown, RuleOutcomes: []risk.RuleOutcome{}, FailureReason: reason}
	payload, err := json.Marshal(contract.FromEvaluated(et))
	if err != nil {
		return fmt.Errorf("encode failed tx: %w", err)
	}
	if err := b.Publisher().Publish(ctx, EvaluatedTxTopic, Message{Key: t.AccountID, ID: t.ID, Payload: payload}); err != nil {
		return fmt.Errorf("publish failed tx: %w", err)
	}
	return nil
}

func alreadyProcessed(ctx context.Context, b Backends, t risk.Tx) (bool, error) {
	stored, found, err := b.EvaluatedTxDB().Get(ctx, t.ID)
	if err != nil {
		return false, fmt.Errorf("get stored tx: %w", err)
	}
	if !found {
		return false, nil
	}
	if !sameTx(stored, t) {
		return false, fmt.Errorf("%w: %s", risk.ErrConflictingTx, t.ID)
	}
	return true, nil
}

// Postgres keeps microseconds, so PostedAt is compared at that precision. The rest is compared as
// it would be published, since the detail pointers differ even when the values match.
func sameTx(a, b risk.Tx) bool {
	if !a.PostedAt.Truncate(time.Microsecond).Equal(b.PostedAt.Truncate(time.Microsecond)) {
		return false
	}
	a.PostedAt, b.PostedAt = time.Time{}, time.Time{}

	aJSON, aErr := json.Marshal(contract.FromEvaluated(risk.EvaluatedTx{Tx: a}))
	bJSON, bErr := json.Marshal(contract.FromEvaluated(risk.EvaluatedTx{Tx: b}))
	return aErr == nil && bErr == nil && bytes.Equal(aJSON, bJSON)
}
