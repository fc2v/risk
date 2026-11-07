package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"risk/risk"
)

const queryTimeout = 3 * time.Second

type EvaluatedTxModel struct {
	DB *sql.DB
}

const (
	txColumns          = `id, account_id, transaction_type, amount, posted_at, category, details`
	evaluatedTxColumns = txColumns + `, created_at, score, risk, rule_outcomes`
)

func (m EvaluatedTxModel) Insert(ctx context.Context, et risk.EvaluatedTx) error {
	detailsJSON, err := encodeDetails(et.Tx)
	if err != nil {
		return err
	}

	outcomesJSON, err := encodeOutcomes(et.RuleOutcomes)
	if err != nil {
		return fmt.Errorf("encode rule outcomes: %w", err)
	}

	// One statement, so the tx and its profile are stored together, and a redelivered tx changes nothing.
	query := `
		WITH tx AS (
			INSERT INTO evaluated_txs (` + txColumns + `, kind, score, risk, rule_outcomes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (id) DO NOTHING
			RETURNING account_id, kind, amount, posted_at, created_at
		), hours AS (
			INSERT INTO account_hours AS h (account_id, kind, hour, tx_count, inflow, outflow, with_device)
			SELECT account_id, kind, date_trunc('hour', posted_at, 'UTC'), 1, greatest(amount, 0), greatest(-amount, 0), $12
			FROM tx
			ON CONFLICT (account_id, kind, hour) DO UPDATE SET
				tx_count = h.tx_count + 1,
				inflow = h.inflow + excluded.inflow,
				outflow = h.outflow + excluded.outflow,
				with_device = h.with_device + excluded.with_device
		), seen AS (
			INSERT INTO account_seen AS s (account_id, field, value, kind, first_seen, last_seen)
			SELECT tx.account_id, f.field, f.value, tx.kind, tx.posted_at, tx.posted_at
			FROM tx, unnest($13::text[], $14::text[]) AS f(field, value)
			ON CONFLICT (account_id, field, value, kind) DO UPDATE SET
				first_seen = least(s.first_seen, excluded.first_seen),
				last_seen = greatest(s.last_seen, excluded.last_seen)
		)
		SELECT created_at FROM tx`

	withDevice := 0
	if _, ok := et.Fields()[risk.FieldDevice]; ok {
		withDevice = 1
	}
	var fields, values []string
	for f, v := range et.Fields() {
		fields, values = append(fields, string(f)), append(values, v)
	}

	args := []any{
		et.ID, et.AccountID, string(et.Type), et.Amount, et.PostedAt, et.Category, detailsJSON,
		string(et.Kind()), et.Score, string(et.Risk), outcomesJSON,
		withDevice, pq.Array(fields), pq.Array(values),
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var createdAt time.Time
	err = m.DB.QueryRowContext(ctx, query, args...).Scan(&createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return risk.ErrDuplicateTx
	}
	return err
}

func (m EvaluatedTxModel) Get(ctx context.Context, id string) (risk.Tx, bool, error) {
	query := `SELECT ` + txColumns + ` FROM evaluated_txs WHERE id = $1`

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var t risk.Tx
	err := scanTx(m.DB.QueryRowContext(ctx, query, id), &t)
	if errors.Is(err, sql.ErrNoRows) {
		return risk.Tx{}, false, nil
	}
	if err != nil {
		return risk.Tx{}, false, err
	}
	return t, true, nil
}

func (m EvaluatedTxModel) GetEvaluated(ctx context.Context, id string) (risk.EvaluatedTx, bool, error) {
	query := `SELECT ` + evaluatedTxColumns + ` FROM evaluated_txs WHERE id = $1`

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	et, err := scanEvaluatedTx(m.DB.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return risk.EvaluatedTx{}, false, nil
	}
	if err != nil {
		return risk.EvaluatedTx{}, false, err
	}
	return et, true, nil
}

type EvaluatedTxCursor struct {
	PostedAt time.Time
	ID       string
}

type EvaluatedTxFilter struct {
	AccountID    string
	PostedFrom   time.Time
	PostedBefore time.Time
	Risk         risk.Risk
	After        *EvaluatedTxCursor
	PageSize     int
}

func (m EvaluatedTxModel) List(ctx context.Context, f EvaluatedTxFilter) (page []risk.EvaluatedTx, next *EvaluatedTxCursor, err error) {
	query := `SELECT ` + evaluatedTxColumns + `
		FROM evaluated_txs
		WHERE ($1 = '' OR account_id = $1)
			AND ($2::timestamptz IS NULL OR posted_at >= $2)
			AND ($3::timestamptz IS NULL OR posted_at < $3)
			AND ($4 = '' OR risk = $4)
			AND ($5::timestamptz IS NULL OR (posted_at, id) < ($5, $6))
		ORDER BY posted_at DESC, id DESC
		LIMIT $7`

	var afterPostedAt sql.NullTime
	var afterID string
	if f.After != nil {
		afterPostedAt = sql.NullTime{Time: f.After.PostedAt, Valid: true}
		afterID = f.After.ID
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	pageAndOneMore := f.PageSize + 1
	rows, err := m.DB.QueryContext(ctx, query, f.AccountID, nullTime(f.PostedFrom), nullTime(f.PostedBefore),
		string(f.Risk), afterPostedAt, afterID, pageAndOneMore)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		et, err := scanEvaluatedTx(rows)
		if err != nil {
			return nil, nil, err
		}
		page = append(page, et)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	if len(page) > f.PageSize {
		page = page[:f.PageSize]
		last := page[len(page)-1]
		next = &EvaluatedTxCursor{PostedAt: last.PostedAt, ID: last.ID}
	}
	return page, next, nil
}

type scanner interface{ Scan(...any) error }

func scanTx(row scanner, t *risk.Tx, extra ...any) error {
	var txType string
	var detailsJSON []byte
	dest := append([]any{&t.ID, &t.AccountID, &txType, &t.Amount, &t.PostedAt, &t.Category, &detailsJSON}, extra...)
	if err := row.Scan(dest...); err != nil {
		return err
	}
	t.Type = risk.TxType(txType)
	t.PostedAt = t.PostedAt.UTC()
	return decodeDetails(t, detailsJSON)
}

func scanEvaluatedTx(row scanner) (risk.EvaluatedTx, error) {
	var et risk.EvaluatedTx
	var riskLevel string
	var outcomesJSON []byte
	if err := scanTx(row, &et.Tx, &et.CreatedAt, &et.Score, &riskLevel, &outcomesJSON); err != nil {
		return risk.EvaluatedTx{}, err
	}
	et.CreatedAt = et.CreatedAt.UTC()
	et.Risk = risk.Risk(riskLevel)
	outcomes, err := decodeOutcomes(outcomesJSON)
	if err != nil {
		return risk.EvaluatedTx{}, fmt.Errorf("decode rule outcomes of tx %s: %w", et.ID, err)
	}
	et.RuleOutcomes = outcomes
	return et, nil
}

func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: !t.IsZero()}
}
