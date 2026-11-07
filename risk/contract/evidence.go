package contract

import (
	"maps"

	"risk/risk"
)

type EvidenceType string

const (
	EvidenceInteger EvidenceType = "integer"
	EvidenceNumber  EvidenceType = "number"
	EvidenceBoolean EvidenceType = "boolean"
	EvidenceString  EvidenceType = "string"
	EvidenceStrings EvidenceType = "string array"
)

// PromisedEvidence lists the evidence keys every outcome with a given reason carries, as published JSON types.
// Outcomes may carry more keys; only these are promised. Durations are whole seconds; amounts are minor units.
var PromisedEvidence = map[risk.Reason]map[string]EvidenceType{
	risk.ReasonNotApplicable: {},
	risk.ReasonInsufficientHistory: {
		"baselineTxs":    EvidenceInteger,
		"minBaselineTxs": EvidenceInteger,
	},

	risk.ReasonVelocityAboveNormal:  velocityEvidence,
	risk.ReasonVelocityWithinNormal: velocityEvidence,

	risk.ReasonAmountAboveThreshold:  highAmountEvidence,
	risk.ReasonAmountWithinThreshold: highAmountEvidence,

	risk.ReasonUnusualTime: unusualTimeEvidence,
	risk.ReasonUsualTime:   unusualTimeEvidence,

	risk.ReasonPayeeAgeUnknown:    {"maxPayeeAge": EvidenceInteger, "minAmount": EvidenceInteger},
	risk.ReasonFreshPayee:         freshPayeeEvidence,
	risk.ReasonPayeeNotFresh:      freshPayeeEvidence,
	risk.ReasonAmountBelowMinimum: freshPayeeEvidence,

	risk.ReasonPayeeNameMismatch:   payeeNameEvidence,
	risk.ReasonPayeeNameNotFlagged: payeeNameEvidence,

	risk.ReasonInflowBelowMinimum: passThroughEvidence,
	risk.ReasonPassThrough:        withShare(passThroughEvidence),
	risk.ReasonShareBelowMinimum:  withShare(passThroughEvidence),
	risk.ReasonPayeePaidBefore:    withShare(passThroughEvidence),

	risk.ReasonNewCounterparty:   newCounterpartyEvidence,
	risk.ReasonKnownCounterparty: newCounterpartyEvidence,

	risk.ReasonNewDevice:     newDeviceEvidence,
	risk.ReasonKnownDevice:   newDeviceEvidence,
	risk.ReasonDeviceUnknown: newDeviceEvidence,

	risk.ReasonNewCountry:        newCountryEvidence,
	risk.ReasonKnownCountry:      newCountryEvidence,
	risk.ReasonChannelNotChecked: newCountryEvidence,

	risk.ReasonNewCreditor:           newCreditorEvidence,
	risk.ReasonKnownCreditor:         newCreditorEvidence,
	risk.ReasonMandateTypeNotChecked: newCreditorEvidence,

	risk.ReasonMandateMaxUnknown: {},
	risk.ReasonAboveMandateMax:   {"mandateMaxAmount": EvidenceInteger},
	risk.ReasonWithinMandateMax:  {"mandateMaxAmount": EvidenceInteger},

	risk.ReasonRepresentedDebit: representedDebitEvidence,
	risk.ReasonNotRepresented:   representedDebitEvidence,

	risk.ReasonHighRiskMCC:           {"mccs": EvidenceStrings, "mcc": EvidenceString},
	risk.ReasonMCCNotHighRisk:        {"mccs": EvidenceStrings, "mcc": EvidenceString},
	risk.ReasonHighRiskMCCSeenBefore: {"mccs": EvidenceStrings, "mcc": EvidenceString, "priorHighRiskMCCs": EvidenceInteger},

	risk.ReasonHighRiskCategory:    {"categories": EvidenceStrings, "category": EvidenceString},
	risk.ReasonCategoryNotHighRisk: {"categories": EvidenceStrings, "category": EvidenceString},
}

var (
	velocityEvidence = map[string]EvidenceType{
		"countInWindow":    EvidenceInteger,
		"window":           EvidenceInteger,
		"minCount":         EvidenceInteger,
		"multiplier":       EvidenceNumber,
		"baselineTxs":      EvidenceInteger,
		"minBaselineTxs":   EvidenceInteger,
		"baselineSpan":     EvidenceInteger,
		"expectedInWindow": EvidenceNumber,
	}
	highAmountEvidence = map[string]EvidenceType{
		"usedDefault":    EvidenceBoolean,
		"baselineTxs":    EvidenceInteger,
		"minBaselineTxs": EvidenceInteger,
		"multiplier":     EvidenceNumber,
		"threshold":      EvidenceNumber,
	}
	unusualTimeEvidence = map[string]EvidenceType{
		"hour":           EvidenceInteger,
		"neighbourHours": EvidenceInteger,
		"txsNearHour":    EvidenceInteger,
		"baselineTxs":    EvidenceInteger,
		"minBaselineTxs": EvidenceInteger,
		"maxShare":       EvidenceNumber,
		"excludeRecent":  EvidenceInteger,
		"share":          EvidenceNumber,
	}
	freshPayeeEvidence = map[string]EvidenceType{
		"maxPayeeAge": EvidenceInteger,
		"minAmount":   EvidenceInteger,
		"payeeAge":    EvidenceInteger,
	}
	payeeNameEvidence = map[string]EvidenceType{
		"mismatchValues": EvidenceStrings,
		"payeeMatch":     EvidenceString,
	}
	passThroughEvidence = map[string]EvidenceType{
		"window":              EvidenceInteger,
		"inboundInWindow":     EvidenceInteger,
		"minInbound":          EvidenceInteger,
		"outboundSinceInflow": EvidenceInteger,
		"minShare":            EvidenceNumber,
		"paidBefore":          EvidenceBoolean,
		"newCounterpartyOnly": EvidenceBoolean,
	}
	newCounterpartyEvidence = map[string]EvidenceType{
		"baselineTxs":         EvidenceInteger,
		"minBaselineTxs":      EvidenceInteger,
		"priorCounterparties": EvidenceInteger,
	}
	newDeviceEvidence = map[string]EvidenceType{
		"baselineTxs":    EvidenceInteger,
		"minBaselineTxs": EvidenceInteger,
		"priorDevices":   EvidenceInteger,
	}
	newCountryEvidence = map[string]EvidenceType{
		"baselineTxs":     EvidenceInteger,
		"minBaselineTxs":  EvidenceInteger,
		"priorCountries":  EvidenceInteger,
		"paymentChannels": EvidenceStrings,
		"merchantCountry": EvidenceString,
		"paymentChannel":  EvidenceString,
	}
	newCreditorEvidence = map[string]EvidenceType{
		"baselineTxs":      EvidenceInteger,
		"minBaselineTxs":   EvidenceInteger,
		"priorCreditors":   EvidenceInteger,
		"mandateTypes":     EvidenceStrings,
		"creditorUserCode": EvidenceString,
		"mandateType":      EvidenceString,
		"sequence":         EvidenceString,
	}
	representedDebitEvidence = map[string]EvidenceType{
		"sequences": EvidenceStrings,
		"sequence":  EvidenceString,
	}
)

func withShare(m map[string]EvidenceType) map[string]EvidenceType {
	out := maps.Clone(m)
	out["share"] = EvidenceNumber
	return out
}
