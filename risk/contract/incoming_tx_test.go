package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"risk/risk"
)

func TestIncomingTxSamples(t *testing.T) {
	posted := time.Date(2026, 11, 9, 2, 10, 0, 0, time.UTC)
	account := "3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14"

	cases := map[string]risk.Tx{
		"card": {
			ID: "0b7d2c1e-6a4f-4e8b-9c3d-2f1a5e7b9d04", AccountID: account, Type: risk.TxTypeCard,
			Amount: -95000, PostedAt: posted, Category: "electronics",
			Card: &risk.Card{
				MCC: "5732", PaymentChannel: "online", MerchantName: "Gadget Hub",
				MerchantCity: "Johannesburg", MerchantProvince: "Gauteng", MerchantPostalCode: "2196", MerchantCountry: "ZA",
			},
		},
		"debit_order": {
			ID: "5c9e1f3a-2b7d-4d6e-8a1c-9f4b2e7d3a61", AccountID: account, Type: risk.TxTypeDebitOrder,
			Amount: -45000, PostedAt: posted, Category: "insurance",
			DebitOrder: &risk.DebitOrder{
				MandateID: "MND-20251001-77", MandateType: "debicheck", MandateCreatedAt: new(posted.AddDate(-1, 0, 0)),
				MandateMaxAmount: new(int64(50000)), Frequency: "monthly", Sequence: "recurring", ActionDate: "2026-11-09",
				CreditorUserCode: "INSURECO", CreditorName: "Insure Co", CreditorBankCode: "250655",
			},
		},
		"transfer": {
			ID: "9b2e7c41-0d3f-4a8e-b6c5-1f7a2d9e3c08", AccountID: account, Type: risk.TxTypeTransfer,
			Amount: -450000, PostedAt: posted, Category: "transfer_out",
			Transfer: &risk.Transfer{
				Rail: "rtc", InitiationChannel: "app", DeviceID: "dev-c19e",
				CounterpartyName: "T Mokoena", CounterpartyBankCode: "470010", CounterpartyAccountHash: "e40f7b",
				CounterpartyProxy: "0821234567", CounterpartyType: risk.CounterpartyExternal,
				PayeeAddedAt: new(posted.Add(-15 * time.Minute)), PayeeMatch: "partialMatch", Reference: "rent",
			},
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "incoming_tx_"+name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			var in IncomingTx
			if err := dec.Decode(&in); err != nil {
				t.Fatalf("sample doesn't match the contract: %v", err)
			}

			got := in.ToTx()
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ToTx() =\n%+v\nwant\n%+v", got, want)
			}
			for _, at := range []*time.Time{&got.PostedAt, mandateCreatedAt(got), payeeAddedAt(got)} {
				if at != nil && at.Location() != time.UTC {
					t.Errorf("time %v not in UTC", at)
				}
			}
		})
	}
}

func mandateCreatedAt(t risk.Tx) *time.Time {
	if t.DebitOrder == nil {
		return nil
	}
	return t.MandateCreatedAt
}

func payeeAddedAt(t risk.Tx) *time.Time {
	if t.Transfer == nil {
		return nil
	}
	return t.PayeeAddedAt
}
