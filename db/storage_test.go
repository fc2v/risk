package db

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"risk/risk"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

func TestDetailsStorage(t *testing.T) {
	at := time.Date(2026, 11, 9, 2, 10, 0, 0, time.UTC)
	cases := map[string]risk.Tx{
		"card": {
			ID: "c", Type: risk.TxTypeCard,
			Card: &risk.Card{
				MCC: "5732", PaymentChannel: "online", MerchantName: "Gadget Hub",
				MerchantCity: "Johannesburg", MerchantProvince: "Gauteng", MerchantPostalCode: "2196", MerchantCountry: "ZA",
			},
		},
		"debit_order": {
			ID: "d", Type: risk.TxTypeDebitOrder,
			DebitOrder: &risk.DebitOrder{
				MandateID: "MND-20251001-77", MandateType: "debicheck", MandateCreatedAt: new(at.AddDate(-1, 0, 0)),
				MandateMaxAmount: new(int64(50000)), Frequency: "monthly", Sequence: "recurring", ActionDate: "2026-11-09",
				CreditorUserCode: "INSURECO", CreditorName: "Insure Co", CreditorBankCode: "250655",
			},
		},
		"transfer": {
			ID: "t", Type: risk.TxTypeTransfer,
			Transfer: &risk.Transfer{
				Rail: "rtc", InitiationChannel: "app", DeviceID: "dev-c19e",
				CounterpartyName: "T Mokoena", CounterpartyBankCode: "470010", CounterpartyAccountHash: "e40f7b",
				CounterpartyProxy: "0821234567", CounterpartyType: risk.CounterpartyExternal,
				PayeeAddedAt: new(at.Add(-15 * time.Minute)), PayeeMatch: "partialMatch", Reference: "rent",
			},
		},
	}

	for name, tx := range cases {
		t.Run(name, func(t *testing.T) {
			stored, err := encodeDetails(tx)
			if err != nil {
				t.Fatal(err)
			}
			assertGolden(t, "details_"+name+".json", stored)

			got := risk.Tx{ID: tx.ID, Type: tx.Type}
			if err := decodeDetails(&got, stored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tx) {
				t.Errorf("round trip =\n%+v\nwant\n%+v", got, tx)
			}
		})
	}
}

func TestOutcomesStorage(t *testing.T) {
	outcomes := []risk.RuleOutcome{{
		Rule: "freshPayee", Triggered: true, Reason: risk.ReasonFreshPayee, Score: 35,
		Evidence: map[string]any{"maxPayeeAge": 3600.0, "minAmount": 100000.0, "payeeAge": 900.0},
	}}

	stored, err := encodeOutcomes(outcomes)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "outcomes.json", stored)

	got, err := decodeOutcomes(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, outcomes) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, outcomes)
	}

	if empty, _ := encodeOutcomes(nil); string(empty) != "[]" {
		t.Errorf("nil outcomes stored as %s, want []", empty)
	}
}

func assertGolden(t *testing.T, name string, compact []byte) {
	t.Helper()
	var got bytes.Buffer
	if err := json.Indent(&got, compact, "", "  "); err != nil {
		t.Fatal(err)
	}
	got.WriteByte('\n')

	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./db -update to create it)", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("%s changed: stored rows use the old format, so this needs a migration\ngot:\n%s", path, got.Bytes())
	}
}
