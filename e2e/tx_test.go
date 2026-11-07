//go:build e2e

package e2e

import (
	"encoding/json"
	"maps"
	"time"
)

type txPayload map[string]any

func cardTx(account string, amount int64, postedAt time.Time) txPayload {
	return txPayload{
		"id":              newTxID(),
		"accountId":       account,
		"transactionType": "card",
		"amount":          amount,
		"postedAt":        postedAt.UTC().Format(time.RFC3339Nano),
		"category":        "groceries",
		"mcc":             "5411",
		"paymentChannel":  "in store",
		"merchantName":    "Corner Shop",
		"merchantCountry": "ZA",
	}
}

func transferTx(account string, amount int64, postedAt time.Time, payeeAddedAt *time.Time) txPayload {
	p := txPayload{
		"id":                      newTxID(),
		"accountId":               account,
		"transactionType":         "transfer",
		"amount":                  amount,
		"postedAt":                postedAt.UTC().Format(time.RFC3339Nano),
		"category":                "transfers",
		"rail":                    "rtc",
		"initiationChannel":       "app",
		"counterpartyName":        "A Person",
		"counterpartyBankCode":    "250655",
		"counterpartyAccountHash": "hash-" + randomHex(8),
		"counterpartyType":        "EXTERNAL",
	}
	if payeeAddedAt != nil {
		p["payeeAddedAt"] = payeeAddedAt.UTC().Format(time.RFC3339Nano)
	}
	return p
}

func (p txPayload) with(key string, value any) txPayload {
	c := maps.Clone(p)
	c[key] = value
	return c
}

func (p txPayload) without(key string) txPayload {
	c := maps.Clone(p)
	delete(c, key)
	return c
}

func (p txPayload) id() string { return p["id"].(string) }

func (p txPayload) toJSON() []byte {
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return b
}
