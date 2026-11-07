package risk

import "testing"

func TestKind(t *testing.T) {
	transfer := func(amount int64, cp CounterpartyType) Tx {
		return Tx{Type: TxTypeTransfer, Amount: amount, Transfer: &Transfer{CounterpartyType: cp}}
	}
	cases := []struct {
		name string
		tx   Tx
		want TxKind
	}{
		{"card spend", Tx{Type: TxTypeCard, Amount: -100, Card: &Card{}}, KindCard},
		{"card refund", Tx{Type: TxTypeCard, Amount: 100, Card: &Card{}}, KindCardRefund},
		{"debit order", Tx{Type: TxTypeDebitOrder, Amount: -100, DebitOrder: &DebitOrder{}}, KindDebitOrder},
		{"debit order reversal", Tx{Type: TxTypeDebitOrder, Amount: 100, DebitOrder: &DebitOrder{}}, KindDebitOrder},
		{"transfer out", transfer(-100, CounterpartyExternal), KindTransferOut},
		{"transfer in", transfer(100, CounterpartyExternal), KindTransferIn},
		{"own account out", transfer(-100, CounterpartyOwnAccount), KindOwnAccountTransfer},
		{"own account in", transfer(100, CounterpartyOwnAccount), KindOwnAccountTransfer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tx.Kind(); got != tc.want {
				t.Errorf("Kind() = %s, want %s", got, tc.want)
			}
		})
	}
}
