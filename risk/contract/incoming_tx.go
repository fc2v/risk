package contract

import (
	"time"

	"risk/risk"
)

type IncomingTx struct {
	ID              string    `json:"id"`
	AccountID       string    `json:"accountId"`
	TransactionType string    `json:"transactionType"`
	Amount          int64     `json:"amount"`
	PostedAt        time.Time `json:"postedAt"`
	Category        string    `json:"category"`
	Card
	DebitOrder
	Transfer
}

func (in IncomingTx) ToTx() risk.Tx {
	t := risk.Tx{
		ID:        in.ID,
		AccountID: in.AccountID,
		Type:      risk.TxType(in.TransactionType),
		Amount:    in.Amount,
		PostedAt:  in.PostedAt.UTC(),
		Category:  in.Category,
	}

	switch t.Type {
	case risk.TxTypeCard:
		t.Card = new(risk.Card(in.Card))
	case risk.TxTypeDebitOrder:
		debitOrder := risk.DebitOrder(in.DebitOrder)
		debitOrder.MandateCreatedAt = utc(debitOrder.MandateCreatedAt)
		t.DebitOrder = &debitOrder
	case risk.TxTypeTransfer:
		transfer := risk.Transfer(in.Transfer)
		transfer.PayeeAddedAt = utc(transfer.PayeeAddedAt)
		t.Transfer = &transfer
	}
	return t
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return new(t.UTC())
}
