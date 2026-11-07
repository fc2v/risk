package contract

import (
	"time"

	"risk/risk"
)

type EvaluatedTx struct {
	ID              string    `json:"id"`
	AccountID       string    `json:"accountId"`
	TransactionType string    `json:"transactionType"`
	Amount          int64     `json:"amount"`
	PostedAt        time.Time `json:"postedAt"`
	Category        string    `json:"category"`
	*Card
	*DebitOrder
	*Transfer
	Score         int           `json:"score"`
	Risk          string        `json:"risk"`
	RuleOutcomes  []RuleOutcome `json:"ruleOutcomes"`
	FailureReason string        `json:"failureReason,omitempty"`
}

type RuleOutcome struct {
	Rule      string         `json:"rule"`
	Triggered bool           `json:"triggered"`
	Reason    string         `json:"reason"`
	Score     int            `json:"score"`
	Evidence  map[string]any `json:"evidence,omitempty"`
}

// Fields match the domain types exactly so conversion compiles; a new domain field breaks it until it's mapped.
type Card struct {
	MCC                string `json:"mcc"`
	PaymentChannel     string `json:"paymentChannel"`
	MerchantName       string `json:"merchantName"`
	MerchantCity       string `json:"merchantCity,omitempty"`
	MerchantProvince   string `json:"merchantProvince,omitempty"`
	MerchantPostalCode string `json:"merchantPostalCode,omitempty"`
	MerchantCountry    string `json:"merchantCountry"`
}

type DebitOrder struct {
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

type Transfer struct {
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

func FromEvaluated(et risk.EvaluatedTx) EvaluatedTx {
	out := EvaluatedTx{
		ID:              et.ID,
		AccountID:       et.AccountID,
		TransactionType: string(et.Type),
		Amount:          et.Amount,
		PostedAt:        et.PostedAt.UTC(),
		Category:        et.Category,
		Score:           et.Score,
		Risk:            string(et.Risk),
		RuleOutcomes:    make([]RuleOutcome, len(et.RuleOutcomes)),
		FailureReason:   string(et.FailureReason),
	}
	if et.Card != nil {
		out.Card = new(Card(*et.Card))
	}
	if et.DebitOrder != nil {
		out.DebitOrder = new(DebitOrder(*et.DebitOrder))
	}
	if et.Transfer != nil {
		out.Transfer = new(Transfer(*et.Transfer))
	}
	for i, o := range et.RuleOutcomes {
		out.RuleOutcomes[i] = FromOutcome(o)
	}
	return out
}

func FromOutcome(o risk.RuleOutcome) RuleOutcome {
	return RuleOutcome{
		Rule:      o.Rule,
		Triggered: o.Triggered,
		Reason:    string(o.Reason),
		Score:     o.Score,
		Evidence:  o.Evidence,
	}
}
