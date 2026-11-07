package db

import (
	"encoding/json"
	"fmt"
	"time"

	"risk/risk"
)

// Keys are the stored format of existing rows; changing one needs a migration.
// Fields match the domain types exactly so conversion compiles; a new domain field breaks it until it's mapped.
type cardDetails struct {
	MCC                string `json:"mcc"`
	PaymentChannel     string `json:"paymentChannel"`
	MerchantName       string `json:"merchantName"`
	MerchantCity       string `json:"merchantCity,omitempty"`
	MerchantProvince   string `json:"merchantProvince,omitempty"`
	MerchantPostalCode string `json:"merchantPostalCode,omitempty"`
	MerchantCountry    string `json:"merchantCountry"`
}

type debitOrderDetails struct {
	MandateID        string     `json:"mandateId"`
	MandateType      string     `json:"mandateType"`
	MandateCreatedAt *time.Time `json:"mandateCreatedAt,omitempty"`
	MandateMaxAmount *int64     `json:"mandateMaxAmount,omitempty"`
	Frequency        string     `json:"frequency"`
	Sequence         string     `json:"sequence"`
	ActionDate       string     `json:"actionDate"`
	CreditorUserCode string     `json:"creditorUserCode"`
	CreditorName     string     `json:"creditorName"`
	CreditorBankCode string     `json:"creditorBankCode,omitempty"`
}

type transferDetails struct {
	Rail                    string                `json:"rail"`
	InitiationChannel       string                `json:"initiationChannel"`
	DeviceID                string                `json:"deviceId,omitempty"`
	CounterpartyName        string                `json:"counterpartyName"`
	CounterpartyBankCode    string                `json:"counterpartyBankCode"`
	CounterpartyAccountHash string                `json:"counterpartyAccountHash"`
	CounterpartyProxy       string                `json:"counterpartyProxy,omitempty"`
	CounterpartyType        risk.CounterpartyType `json:"counterpartyType"`
	PayeeAddedAt            *time.Time            `json:"payeeAddedAt,omitempty"`
	PayeeMatch              string                `json:"payeeMatch,omitempty"`
	Reference               string                `json:"reference,omitempty"`
}

type storedOutcome struct {
	Rule      string         `json:"rule"`
	Triggered bool           `json:"triggered"`
	Reason    risk.Reason    `json:"reason"`
	Score     int            `json:"score"`
	Evidence  map[string]any `json:"evidence,omitempty"`
}

func encodeDetails(t risk.Tx) ([]byte, error) {
	var details any
	switch {
	case t.Type == risk.TxTypeCard && t.Card != nil:
		details = cardDetails(*t.Card)
	case t.Type == risk.TxTypeDebitOrder && t.DebitOrder != nil:
		details = debitOrderDetails(*t.DebitOrder)
	case t.Type == risk.TxTypeTransfer && t.Transfer != nil:
		details = transferDetails(*t.Transfer)
	default:
		return nil, fmt.Errorf("tx %s: no details for type %q", t.ID, t.Type)
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("encode details of tx %s: %w", t.ID, err)
	}
	return detailsJSON, nil
}

func decodeDetails(t *risk.Tx, detailsJSON []byte) error {
	var err error
	switch t.Type {
	case risk.TxTypeCard:
		var d cardDetails
		err = json.Unmarshal(detailsJSON, &d)
		t.Card = new(risk.Card(d))
	case risk.TxTypeDebitOrder:
		var d debitOrderDetails
		err = json.Unmarshal(detailsJSON, &d)
		t.DebitOrder = new(risk.DebitOrder(d))
	case risk.TxTypeTransfer:
		var d transferDetails
		err = json.Unmarshal(detailsJSON, &d)
		t.Transfer = new(risk.Transfer(d))
	default:
		err = fmt.Errorf("unknown type %q", t.Type)
	}
	if err != nil {
		return fmt.Errorf("decode details of tx %s: %w", t.ID, err)
	}
	return nil
}

func encodeOutcomes(outcomes []risk.RuleOutcome) ([]byte, error) {
	stored := make([]storedOutcome, len(outcomes))
	for i, o := range outcomes {
		stored[i] = storedOutcome(o)
	}
	return json.Marshal(stored)
}

func decodeOutcomes(outcomesJSON []byte) ([]risk.RuleOutcome, error) {
	var stored []storedOutcome
	if err := json.Unmarshal(outcomesJSON, &stored); err != nil {
		return nil, err
	}
	outcomes := make([]risk.RuleOutcome, len(stored))
	for i, s := range stored {
		outcomes[i] = risk.RuleOutcome(s)
	}
	return outcomes, nil
}
