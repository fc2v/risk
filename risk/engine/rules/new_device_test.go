package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestNewDevice(t *testing.T) {
	r := NewDevice{MinBaselineTxs: 3, Score: 25}
	from := func(device string, at time.Time) risk.Tx {
		tx := transfer(-1000, at, "payee")
		tx.DeviceID = device
		return tx
	}
	fromD1 := before(3, 24*time.Hour, now, func(at time.Time) risk.Tx { return from("d1", at) })
	noDevice := before(3, 24*time.Hour, now, func(at time.Time) risk.Tx { return from("", at) })

	runCases(t, r, []ruleCase{
		{name: "new device", tx: from("d2", now), prior: fromD1, want: risk.ReasonNewDevice},
		{name: "known device", tx: from("d1", now), prior: fromD1, want: risk.ReasonKnownDevice},
		{name: "device unknown", tx: from("", now), prior: fromD1, want: risk.ReasonDeviceUnknown},
		{name: "prior txs without device don't count", tx: from("d2", now), prior: noDevice, want: risk.ReasonInsufficientHistory},
	})
}
