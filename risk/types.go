package risk

import (
	"errors"
	"time"
)

var (
	ErrDuplicateTx   = errors.New("tx already stored")
	ErrConflictingTx = errors.New("stored tx differs from incoming tx")
)

type TxType string

const (
	TxTypeCard       TxType = "card"
	TxTypeDebitOrder TxType = "debitOrder"
	TxTypeTransfer   TxType = "transfer"
)

type Tx struct {
	ID        string
	AccountID string
	Type      TxType
	Amount    int64 // minor units; negative is money out
	PostedAt  time.Time
	Category  string
	*Card
	*DebitOrder
	*Transfer
}

type CounterpartyType string

const (
	CounterpartyOwnAccount CounterpartyType = "OWN_ACCOUNT"
	CounterpartyExternal   CounterpartyType = "EXTERNAL"
)

type TxKind string

const (
	KindCard               TxKind = "card"
	KindCardRefund         TxKind = "cardRefund"
	KindDebitOrder         TxKind = "debitOrder"
	KindTransferOut        TxKind = "transferOut"
	KindTransferIn         TxKind = "transferIn"
	KindOwnAccountTransfer TxKind = "ownAccountTransfer"
)

var TxKinds = []TxKind{KindCard, KindCardRefund, KindDebitOrder, KindTransferOut, KindTransferIn, KindOwnAccountTransfer}

func (t Tx) Kind() TxKind {
	switch {
	case t.Type == TxTypeCard && t.Amount > 0:
		return KindCardRefund
	case t.Type == TxTypeCard:
		return KindCard
	case t.Type == TxTypeDebitOrder:
		return KindDebitOrder
	case t.Transfer != nil && t.CounterpartyType == CounterpartyOwnAccount:
		return KindOwnAccountTransfer
	case t.Amount > 0:
		return KindTransferIn
	default:
		return KindTransferOut
	}
}

type Field string

const (
	FieldCounterparty    Field = "counterparty"
	FieldDevice          Field = "device"
	FieldMerchantCountry Field = "merchantCountry"
	FieldMCC             Field = "mcc"
	FieldCreditor        Field = "creditor"
)

// Fields are the values a tx adds to its account's seen sets; empty values are left out.
func (t Tx) Fields() map[Field]string {
	fields := map[Field]string{}
	add := func(f Field, v string) {
		if v != "" {
			fields[f] = v
		}
	}
	switch {
	case t.Card != nil:
		add(FieldMerchantCountry, t.MerchantCountry)
		add(FieldMCC, t.MCC)
	case t.DebitOrder != nil:
		add(FieldCreditor, t.CreditorUserCode)
	case t.Transfer != nil:
		add(FieldCounterparty, t.CounterpartyAccountHash)
		add(FieldDevice, t.DeviceID)
	}
	return fields
}

type Card struct {
	MCC                string
	PaymentChannel     string
	MerchantName       string
	MerchantCity       string
	MerchantProvince   string
	MerchantPostalCode string
	MerchantCountry    string
}

type DebitOrder struct {
	MandateID        string
	MandateType      string
	MandateCreatedAt *time.Time
	MandateMaxAmount *int64
	Frequency        string
	Sequence         string
	ActionDate       string
	CreditorUserCode string
	CreditorName     string
	CreditorBankCode string
}

type Transfer struct {
	Rail                    string
	InitiationChannel       string
	DeviceID                string
	CounterpartyName        string
	CounterpartyBankCode    string
	CounterpartyAccountHash string
	CounterpartyProxy       string
	CounterpartyType        CounterpartyType
	PayeeAddedAt            *time.Time
	PayeeMatch              string
	Reference               string
}

type Risk string

const (
	RiskUnknown Risk = "UNKNOWN"
	RiskLow     Risk = "LOW"
	RiskMedium  Risk = "MEDIUM"
	RiskHigh    Risk = "HIGH"
)

type RuleOutcome struct {
	Rule      string
	Triggered bool
	Reason    Reason
	Score     int
	Evidence  map[string]any
}

type EvaluatedTx struct {
	Tx
	CreatedAt     time.Time
	Score         int
	Risk          Risk
	RuleOutcomes  []RuleOutcome
	FailureReason FailureReason
}

type FailureReason string

const (
	FailureInvalid  FailureReason = "INVALID"
	FailureConflict FailureReason = "CONFLICT"
	FailureInternal FailureReason = "INTERNAL"
)
