package rules

import (
	"risk/risk"
	"risk/risk/engine"
)

type NewDevice struct {
	MinBaselineTxs int
	Score          int
}

func (NewDevice) SupportedKinds() []risk.TxKind {
	return []risk.TxKind{risk.KindTransferOut, risk.KindOwnAccountTransfer}
}

func (r NewDevice) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	txsWithDevice := ec.Profile.WithDevice()
	evidence := map[string]any{
		"baselineTxs":    txsWithDevice,
		"minBaselineTxs": r.MinBaselineTxs,
		"priorDevices":   ec.Profile.Distinct(risk.FieldDevice),
	}
	outcome := risk.RuleOutcome{Score: r.Score, Evidence: evidence}

	switch {
	case t.Transfer == nil:
		outcome.Reason = risk.ReasonNotApplicable
	case t.DeviceID == "":
		outcome.Reason = risk.ReasonDeviceUnknown
	case txsWithDevice < r.MinBaselineTxs:
		outcome.Reason = risk.ReasonInsufficientHistory
	default:
		outcome.Triggered = !ec.Profile.HasSeen(risk.FieldDevice, t.DeviceID)
		outcome.Reason = pick(outcome.Triggered, risk.ReasonNewDevice, risk.ReasonKnownDevice)
	}
	return outcome
}
