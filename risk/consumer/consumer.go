package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"risk/risk"
	"risk/risk/contract"
	"risk/risk/ops"
	"risk/validator"
)

var (
	ErrUnprocessable = errors.New("message can never be processed")
	ErrInvalid       = errors.New("invalid tx")
	ErrUnpublishable = errors.New("message has no tx id or account to publish under")
)

type Consumer struct {
	b ops.Backends
}

func NewConsumer(b ops.Backends) *Consumer {
	return &Consumer{b: b}
}

func (c *Consumer) Handle(ctx context.Context, payload []byte) error {
	in, err := decodeIncomingTx(payload)
	if err != nil {
		return err
	}
	if err := checkIncomingTx(in); err != nil {
		return err
	}
	return c.evaluate(ctx, in)
}

// FailThrough publishes the tx as UNKNOWN with a reason code, so a tx that can't be evaluated never
// holds up the ones after it. The full cause stays in logs and the dead-letter copy, not downstream.
func (c *Consumer) FailThrough(ctx context.Context, payload []byte, cause error) error {
	var in contract.IncomingTx
	if err := json.Unmarshal(payload, &in); err != nil || in.ID == "" || in.AccountID == "" {
		return ErrUnpublishable
	}
	return ops.PublishFailure(ctx, c.b, in.ToTx(), FailureReason(cause))
}

func FailureReason(cause error) risk.FailureReason {
	switch {
	case errors.Is(cause, ErrInvalid):
		return risk.FailureInvalid
	case errors.Is(cause, risk.ErrConflictingTx):
		return risk.FailureConflict
	default:
		return risk.FailureInternal
	}
}

func decodeIncomingTx(payload []byte) (contract.IncomingTx, error) {
	var in contract.IncomingTx
	if err := json.Unmarshal(payload, &in); err != nil {
		return contract.IncomingTx{}, fmt.Errorf("%w: %w: decode: %v", ErrUnprocessable, ErrInvalid, err)
	}
	return in, nil
}

func checkIncomingTx(in contract.IncomingTx) error {
	v := validator.New()
	if validateIncomingTx(v, in); !v.Valid() {
		return fmt.Errorf("%w: %w: tx %s: fields %v", ErrUnprocessable, ErrInvalid, in.ID, v.Errors)
	}
	return nil
}

func (c *Consumer) evaluate(ctx context.Context, in contract.IncomingTx) error {
	err := ops.EvaluateTx(ctx, c.b, in.ToTx())
	if errors.Is(err, risk.ErrConflictingTx) {
		return fmt.Errorf("%w: tx %s: %w", ErrUnprocessable, in.ID, err)
	}
	if err != nil {
		return fmt.Errorf("tx %s: %w", in.ID, err)
	}
	return nil
}

var (
	mccRX     = regexp.MustCompile(`^\d{4}$`)
	countryRX = regexp.MustCompile(`^[A-Z]{2}$`)
)

func validateIncomingTx(v *validator.Validator, in contract.IncomingTx) {
	v.Check(in.ID != "", "id", "must be provided")
	v.Check(in.AccountID != "", "accountId", "must be provided")
	v.Check(in.Amount != 0, "amount", "must not be zero")
	v.Check(!in.PostedAt.IsZero(), "postedAt", "must be provided")

	switch risk.TxType(in.TransactionType) {
	case risk.TxTypeCard:
		v.Check(validator.Matches(in.MCC, mccRX), "mcc", "must be 4 digits")
		v.Check(in.PaymentChannel != "", "paymentChannel", "must be provided")
		v.Check(in.MerchantName != "", "merchantName", "must be provided")
		v.Check(validator.Matches(in.MerchantCountry, countryRX), "merchantCountry", "must be an ISO 3166-1 alpha-2 code")
	case risk.TxTypeDebitOrder:
		v.Check(in.MandateID != "", "mandateId", "must be provided")
		v.Check(in.MandateType != "", "mandateType", "must be provided")
		v.Check(in.Sequence != "", "sequence", "must be provided")
		v.Check(in.CreditorUserCode != "", "creditorUserCode", "must be provided")
		_, err := time.Parse(time.DateOnly, in.ActionDate)
		v.Check(err == nil, "actionDate", "must be a date like 2026-11-01")
	case risk.TxTypeTransfer:
		v.Check(in.Rail != "", "rail", "must be provided")
		v.Check(in.CounterpartyAccountHash != "", "counterpartyAccountHash", "must be provided")
		v.Check(validator.In(string(in.CounterpartyType), string(risk.CounterpartyOwnAccount), string(risk.CounterpartyExternal)),
			"counterpartyType", "must be OWN_ACCOUNT or EXTERNAL")
	default:
		v.AddError("transactionType", "must be card, debitOrder or transfer")
	}
}
