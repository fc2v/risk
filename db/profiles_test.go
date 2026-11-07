package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"risk/migrations"
	"risk/risk"
	"risk/risk/engine"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	adminDSN := os.Getenv("RISK_TEST_DB_DSN")
	if adminDSN == "" {
		t.Skip("set RISK_TEST_DB_DSN to a Postgres admin URL to run database tests")
	}
	admin, err := sql.Open("postgres", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	rand.Read(b)
	name := "risk_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec("create database " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec("drop database if exists " + name + " with (force)")
		admin.Close()
	})

	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m, err := migrations.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	return db
}

var hour0 = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

func profileFixture() []risk.Tx {
	at := func(h, m int) time.Time { return hour0.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	card := func(id, acc string, amount int64, posted time.Time, mcc, country string) risk.Tx {
		return risk.Tx{ID: id, AccountID: acc, Type: risk.TxTypeCard, Amount: amount, PostedAt: posted,
			Card: &risk.Card{MCC: mcc, PaymentChannel: "online", MerchantName: "m", MerchantCountry: country}}
	}
	transfer := func(id, acc string, amount int64, posted time.Time, cp, device string, cpType risk.CounterpartyType) risk.Tx {
		return risk.Tx{ID: id, AccountID: acc, Type: risk.TxTypeTransfer, Amount: amount, PostedAt: posted,
			Transfer: &risk.Transfer{Rail: "rtc", CounterpartyAccountHash: cp, DeviceID: device, CounterpartyType: cpType}}
	}
	debit := func(id, acc string, amount int64, posted time.Time, creditor string) risk.Tx {
		return risk.Tx{ID: id, AccountID: acc, Type: risk.TxTypeDebitOrder, Amount: amount, PostedAt: posted,
			DebitOrder: &risk.DebitOrder{MandateID: "m", MandateType: "eft", Sequence: "recurring", ActionDate: "2026-11-10", CreditorUserCode: creditor}}
	}
	return []risk.Tx{
		card("c1", "a", -1000, at(0, 5), "5411", "ZA"),
		card("c2", "a", -3000, at(0, 50), "5411", "ZA"),
		card("refund", "a", 700, at(1, 10), "5411", "ZA"),
		card("c3", "a", -9000, at(3, 0), "7995", "US"),
		card("late", "a", -2000, at(0, 30), "5812", "ZA"),
		transfer("in", "a", 50000, at(2, 0), "sender", "", risk.CounterpartyExternal),
		transfer("out1", "a", -20000, at(2, 15), "cp-1", "dev-1", risk.CounterpartyExternal),
		transfer("out2", "a", -5000, at(2, 45), "cp-1", "dev-2", risk.CounterpartyExternal),
		transfer("own", "a", -8000, at(2, 50), "own-acc", "", risk.CounterpartyOwnAccount),
		debit("d1", "a", -4500, at(4, 0), "INSURECO"),
		card("b1", "b", -100, at(0, 5), "5411", "ZA"),
	}
}

func insertAll(t *testing.T, db *sql.DB, txs []risk.Tx) {
	t.Helper()
	model := EvaluatedTxModel{DB: db}
	for _, tx := range txs {
		if err := model.Insert(t.Context(), risk.EvaluatedTx{Tx: tx, Risk: risk.RiskLow}); err != nil {
			t.Fatalf("insert %s: %v", tx.ID, err)
		}
	}
}

func rows(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	r, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cols, _ := r.Columns()
	var out []string
	for r.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := r.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			if tm, ok := v.(time.Time); ok {
				vals[i] = tm.UTC().Format("15:04")
			}
		}
		out = append(out, fmt.Sprint(vals...))
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

const (
	hoursQuery = `SELECT account_id, ' ', kind, ' ', hour, ' ', tx_count, ' ', inflow, ' ', outflow, ' ', with_device
		FROM account_hours ORDER BY 1, 3, 5`
	seenQuery = `SELECT account_id, ' ', field, ' ', value, ' ', kind, ' ', first_seen, ' ', last_seen
		FROM account_seen ORDER BY 1, 3, 5, 7`
)

func TestInsertMaintainsProfiles(t *testing.T) {
	db := testDB(t)
	txs := profileFixture()
	insertAll(t, db, txs)

	err := EvaluatedTxModel{DB: db}.Insert(t.Context(), risk.EvaluatedTx{Tx: txs[0], Risk: risk.RiskLow})
	if !errors.Is(err, risk.ErrDuplicateTx) {
		t.Fatalf("redelivered insert = %v, want ErrDuplicateTx", err)
	}

	hours := rows(t, db, hoursQuery)
	for _, want := range []string{
		"a card 09:00 3 0 6000 0",
		"a cardRefund 10:00 1 700 0 0",
		"a card 12:00 1 0 9000 0",
		"a transferIn 11:00 1 50000 0 0",
		"a transferOut 11:00 2 0 25000 2",
		"a ownAccountTransfer 11:00 1 0 8000 0",
		"a debitOrder 13:00 1 0 4500 0",
		"b card 09:00 1 0 100 0",
	} {
		if !slices.Contains(hours, want) {
			t.Errorf("account_hours missing %q; have:\n%v", want, hours)
		}
	}
	if len(hours) != 8 {
		t.Errorf("account_hours has %d rows, want 8", len(hours))
	}

	seen := rows(t, db, seenQuery)
	for _, want := range []string{
		"a merchantCountry ZA card 09:05 09:50",
		"a mcc 5411 card 09:05 09:50",
		"a mcc 5812 card 09:30 09:30",
		"a counterparty cp-1 transferOut 11:15 11:45",
		"a device dev-1 transferOut 11:15 11:15",
		"a counterparty sender transferIn 11:00 11:00",
		"a creditor INSURECO debitOrder 13:00 13:00",
	} {
		if !slices.Contains(seen, want) {
			t.Errorf("account_seen missing %q; have:\n%v", want, seen)
		}
	}
	for _, s := range seen {
		if s == "a device  transferIn 11:00 11:00" || s == "a device  ownAccountTransfer 11:50 11:50" {
			t.Errorf("empty device recorded as seen: %q", s)
		}
	}
}

func TestProfilesMatchRebuild(t *testing.T) {
	db := testDB(t)
	insertAll(t, db, profileFixture())
	hours, seen := rows(t, db, hoursQuery), rows(t, db, seenQuery)

	counts, err := RebuildProfiles(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(t, db, hoursQuery); !slices.Equal(got, hours) {
		t.Errorf("account_hours after rebuild differs:\nincremental %v\nrebuilt     %v", hours, got)
	}
	if got := rows(t, db, seenQuery); !slices.Equal(got, seen) {
		t.Errorf("account_seen after rebuild differs:\nincremental %v\nrebuilt     %v", seen, got)
	}
	if counts.Hours != int64(len(hours)) || counts.Seen != int64(len(seen)) {
		t.Errorf("rebuild reported %+v, want %d hour rows and %d seen rows", counts, len(hours), len(seen))
	}
}

func TestLoadMatchesBuildProfile(t *testing.T) {
	db := testDB(t)
	txs := profileFixture()
	insertAll(t, db, txs)
	model := ProfileModel{DB: db}

	byAccount := map[string][]risk.Tx{}
	for _, tx := range txs {
		byAccount[tx.AccountID] = append(byAccount[tx.AccountID], tx)
	}

	for _, acc := range []string{"a", "b", "nobody"} {
		for _, at := range []time.Time{hour0.Add(150 * time.Minute), hour0.Add(2*time.Hour + 15*time.Minute), hour0.Add(5 * time.Hour), hour0.Add(-time.Hour)} {
			for _, lookback := range []time.Duration{time.Hour, 3 * time.Hour, 48 * time.Hour} {
				for _, recent := range []time.Duration{30 * time.Minute, 6 * time.Hour} {
					for _, n := range []int{1, 2, 200} {
						w := engine.ProfileWindow{At: at, Lookback: lookback, RecentWindow: recent, LastAmounts: n}
						got, err := model.Load(t.Context(), acc, w)
						if err != nil {
							t.Fatal(err)
						}
						want := engine.BuildProfile(byAccount[acc], w)
						if !reflect.DeepEqual(normalize(got), normalize(want)) {
							t.Errorf("account %s at %s lookback %s recent %s last %d:\nload  %+v\nbuild %+v",
								acc, at.Format("15:04"), lookback, recent, n, got, want)
						}
					}
				}
			}
		}
	}
}

// normalize treats nil and empty slices alike; JSON decoding yields empty, building yields nil.
func normalize(p engine.Profile) engine.Profile {
	return engine.Profile{
		Hours:   append([]engine.HourSummary{}, p.Hours...),
		Seen:    append([]engine.SeenValue{}, p.Seen...),
		Amounts: append([]engine.RecentAmount{}, p.Amounts...),
		Recent:  append([]engine.RecentTx{}, p.Recent...),
	}
}
