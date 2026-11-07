package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/lib/pq"

	"risk/risk"
	"risk/risk/engine"
)

// Seen fields must match risk.Tx.Fields; TestProfilesMatchRebuild checks they do.
const rebuildProfiles = `
	TRUNCATE account_hours, account_seen;

	INSERT INTO account_hours (account_id, kind, hour, tx_count, inflow, outflow, with_device)
	SELECT account_id, kind, date_trunc('hour', posted_at, 'UTC'), count(*),
		sum(greatest(amount, 0))::bigint, sum(greatest(-amount, 0))::bigint,
		count(*) FILTER (WHERE transaction_type = 'transfer' AND coalesce(details->>'deviceId', '') <> '')
	FROM evaluated_txs
	GROUP BY 1, 2, 3;

	INSERT INTO account_seen (account_id, field, value, kind, first_seen, last_seen)
	SELECT account_id, f.field, f.value, kind, min(posted_at), max(posted_at)
	FROM evaluated_txs, LATERAL (VALUES
		('merchantCountry', CASE WHEN transaction_type = 'card' THEN details->>'merchantCountry' END),
		('mcc', CASE WHEN transaction_type = 'card' THEN details->>'mcc' END),
		('creditor', CASE WHEN transaction_type = 'debitOrder' THEN details->>'creditorUserCode' END),
		('counterparty', CASE WHEN transaction_type = 'transfer' THEN details->>'counterpartyAccountHash' END),
		('device', CASE WHEN transaction_type = 'transfer' THEN details->>'deviceId' END)
	) AS f(field, value)
	WHERE coalesce(f.value, '') <> ''
	GROUP BY 1, 2, 3, 4;`

// Filters must match engine.BuildProfile; TestLoadMatchesBuildProfile checks they do.
const loadProfile = `
	SELECT
		(SELECT coalesce(json_agg(json_build_object(
			'kind', kind, 'hour', hour, 'count', tx_count, 'inflow', inflow, 'outflow', outflow, 'withDevice', with_device)), '[]')
		FROM account_hours
		WHERE account_id = $1 AND hour >= $2 AND hour <= $3),

		(SELECT coalesce(json_agg(json_build_object(
			'field', field, 'value', value, 'kind', kind, 'firstSeen', first_seen, 'lastSeen', last_seen)), '[]')
		FROM account_seen
		WHERE account_id = $1 AND last_seen > $4 AND first_seen < $5),

		(SELECT coalesce(json_agg(json_build_object('kind', a.kind, 'postedAt', a.posted_at, 'size', abs(a.amount))), '[]')
		FROM unnest($6::text[]) AS k(kind), LATERAL (
			SELECT kind, posted_at, amount FROM evaluated_txs
			WHERE account_id = $1 AND kind = k.kind AND posted_at > $4 AND posted_at <= $5
			ORDER BY posted_at DESC LIMIT $7
		) a),

		(SELECT coalesce(json_agg(json_build_object(
			'kind', kind, 'postedAt', posted_at, 'amount', amount,
			'counterparty', CASE WHEN transaction_type = 'transfer' THEN coalesce(details->>'counterpartyAccountHash', '') ELSE '' END)), '[]')
		FROM evaluated_txs
		WHERE account_id = $1 AND posted_at > $8 AND posted_at <= $5)`

type ProfileModel struct {
	DB *sql.DB
}

func (m ProfileModel) Load(ctx context.Context, accountID string, w engine.ProfileWindow) (engine.Profile, error) {
	firstHour, lastHour := w.HourRange()
	kinds := make([]string, len(risk.TxKinds))
	for i, k := range risk.TxKinds {
		kinds[i] = string(k)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var hours, seen, amounts, recent []byte
	err := m.DB.QueryRowContext(ctx, loadProfile,
		accountID, firstHour, lastHour, w.At.Add(-w.Lookback), w.At, pq.Array(kinds), w.LastAmounts, w.At.Add(-w.RecentWindow),
	).Scan(&hours, &seen, &amounts, &recent)
	if err != nil {
		return engine.Profile{}, fmt.Errorf("load profile: %w", err)
	}

	var p engine.Profile
	for _, part := range []struct {
		raw  []byte
		dest any
	}{{hours, &p.Hours}, {seen, &p.Seen}, {amounts, &p.Amounts}, {recent, &p.Recent}} {
		if err := json.Unmarshal(part.raw, part.dest); err != nil {
			return engine.Profile{}, fmt.Errorf("decode profile: %w", err)
		}
	}
	p.UTC()
	p.Sort()
	return p, nil
}

type ProfileCounts struct {
	Hours int64
	Seen  int64
}

// RebuildProfiles recomputes the profile tables from evaluated_txs. Inserts wait until it commits.
func RebuildProfiles(ctx context.Context, db *sql.DB) (ProfileCounts, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ProfileCounts{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, rebuildProfiles); err != nil {
		return ProfileCounts{}, fmt.Errorf("rebuild profiles: %w", err)
	}
	var c ProfileCounts
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM account_hours), (SELECT count(*) FROM account_seen)`).Scan(&c.Hours, &c.Seen)
	if err != nil {
		return ProfileCounts{}, err
	}
	return c, tx.Commit()
}
