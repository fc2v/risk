package contract

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"risk/risk"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

func TestEvaluatedTxGolden(t *testing.T) {
	at := time.Date(2026, 11, 9, 2, 10, 0, 0, time.UTC)
	highAmount := risk.RuleOutcome{
		Rule:      "highAmountSpend",
		Triggered: true,
		Reason:    risk.ReasonAmountAboveThreshold,
		Score:     30,
		Evidence: map[string]any{
			"baselineTxs": 36, "medianSize": 25000.0, "minBaselineTxs": 5,
			"multiplier": 3.0, "threshold": 75000.0, "usedDefault": false,
		},
	}
	freshPayee := risk.RuleOutcome{
		Rule:      "freshPayee",
		Triggered: true,
		Reason:    risk.ReasonFreshPayee,
		Score:     35,
		Evidence:  map[string]any{"maxPayeeAge": 3600, "minAmount": 100000, "payeeAge": 900},
	}

	cases := map[string]risk.EvaluatedTx{
		"card": {
			Tx: risk.Tx{
				ID: "0b7d2c1e-6a4f-4e8b-9c3d-2f1a5e7b9d04", AccountID: "3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14",
				Type: risk.TxTypeCard, Amount: -95000, PostedAt: at, Category: "electronics",
				Card: &risk.Card{
					MCC: "5732", PaymentChannel: "online", MerchantName: "Gadget Hub",
					MerchantCity: "Johannesburg", MerchantProvince: "Gauteng", MerchantPostalCode: "2196", MerchantCountry: "ZA",
				},
			},
			Score: 30, Risk: risk.RiskMedium, RuleOutcomes: []risk.RuleOutcome{highAmount},
		},
		"debit_order": {
			Tx: risk.Tx{
				ID: "5c9e1f3a-2b7d-4d6e-8a1c-9f4b2e7d3a61", AccountID: "3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14",
				Type: risk.TxTypeDebitOrder, Amount: -45000, PostedAt: at, Category: "insurance",
				DebitOrder: &risk.DebitOrder{
					MandateID: "MND-20251001-77", MandateType: "debicheck", MandateCreatedAt: new(at.AddDate(-1, 0, 0)),
					MandateMaxAmount: new(int64(50000)), Frequency: "monthly", Sequence: "recurring", ActionDate: "2026-11-09",
					CreditorUserCode: "INSURECO", CreditorName: "Insure Co", CreditorBankCode: "250655",
				},
			},
			Risk: risk.RiskLow, RuleOutcomes: []risk.RuleOutcome{},
		},
		"transfer": {
			Tx: risk.Tx{
				ID: "9b2e7c41-0d3f-4a8e-b6c5-1f7a2d9e3c08", AccountID: "3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14",
				Type: risk.TxTypeTransfer, Amount: -450000, PostedAt: at, Category: "transfer_out",
				Transfer: &risk.Transfer{
					Rail: "rtc", InitiationChannel: "app", DeviceID: "dev-c19e",
					CounterpartyName: "T Mokoena", CounterpartyBankCode: "470010", CounterpartyAccountHash: "e40f7b",
					CounterpartyProxy: "0821234567", CounterpartyType: risk.CounterpartyExternal, PayeeAddedAt: new(at.Add(-15 * time.Minute)),
					PayeeMatch: "partialMatch", Reference: "rent",
				},
			},
			Score: 65, Risk: risk.RiskHigh, RuleOutcomes: []risk.RuleOutcome{highAmount, freshPayee},
		},
		"failure": {
			Tx: risk.Tx{
				ID: "e1a4c7d2-3f6b-4a9e-8d2c-7b5f1e3a9c60", AccountID: "3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14",
				Type: risk.TxTypeCard, Amount: -1500, PostedAt: at, Category: "groceries",
				Card: &risk.Card{MCC: "54x1", PaymentChannel: "in store", MerchantName: "Corner Shop", MerchantCountry: "ZA"},
			},
			Risk: risk.RiskUnknown, RuleOutcomes: []risk.RuleOutcome{}, FailureReason: risk.FailureInvalid,
		},
	}

	for name, et := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := json.MarshalIndent(FromEvaluated(et), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			assertGolden(t, filepath.Join("testdata", "evaluated_tx_"+name+".json"), append(got, '\n'))
		})
	}
}

func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./risk/contract -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s changed; if intended, run go test ./risk/contract -update and review the diff\ngot:\n%s", path, got)
	}
}
