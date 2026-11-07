package rules

import (
	"time"

	"risk/risk"
)

func seconds(d time.Duration) int64 { return int64(d / time.Second) }

func pick(triggered bool, ifTriggered, ifNot risk.Reason) risk.Reason {
	if triggered {
		return ifTriggered
	}
	return ifNot
}
