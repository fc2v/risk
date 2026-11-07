package risk

// Reason is part of the published contract: downstream turns it into text using the outcome's evidence.
// Codes may be added, so consumers need a fallback for unknown ones; existing codes never change meaning.
type Reason string

const (
	ReasonNotApplicable       Reason = "NOT_APPLICABLE"
	ReasonInsufficientHistory Reason = "INSUFFICIENT_HISTORY"

	ReasonVelocityAboveNormal  Reason = "VELOCITY_ABOVE_NORMAL"
	ReasonVelocityWithinNormal Reason = "VELOCITY_WITHIN_NORMAL"

	ReasonAmountAboveThreshold  Reason = "AMOUNT_ABOVE_THRESHOLD"
	ReasonAmountWithinThreshold Reason = "AMOUNT_WITHIN_THRESHOLD"

	ReasonUnusualTime Reason = "UNUSUAL_TIME"
	ReasonUsualTime   Reason = "USUAL_TIME"

	ReasonFreshPayee         Reason = "FRESH_PAYEE"
	ReasonPayeeNotFresh      Reason = "PAYEE_NOT_FRESH"
	ReasonAmountBelowMinimum Reason = "AMOUNT_BELOW_MINIMUM"
	ReasonPayeeAgeUnknown    Reason = "PAYEE_AGE_UNKNOWN"

	ReasonPayeeNameMismatch   Reason = "PAYEE_NAME_MISMATCH"
	ReasonPayeeNameNotFlagged Reason = "PAYEE_NAME_NOT_FLAGGED"

	ReasonPassThrough        Reason = "PASS_THROUGH"
	ReasonInflowBelowMinimum Reason = "INFLOW_BELOW_MINIMUM"
	ReasonShareBelowMinimum  Reason = "SHARE_BELOW_MINIMUM"
	ReasonPayeePaidBefore    Reason = "PAYEE_PAID_BEFORE"

	ReasonNewCounterparty   Reason = "NEW_COUNTERPARTY"
	ReasonKnownCounterparty Reason = "KNOWN_COUNTERPARTY"

	ReasonNewDevice     Reason = "NEW_DEVICE"
	ReasonKnownDevice   Reason = "KNOWN_DEVICE"
	ReasonDeviceUnknown Reason = "DEVICE_UNKNOWN"

	ReasonNewCountry        Reason = "NEW_COUNTRY"
	ReasonKnownCountry      Reason = "KNOWN_COUNTRY"
	ReasonChannelNotChecked Reason = "CHANNEL_NOT_CHECKED"

	ReasonNewCreditor           Reason = "NEW_CREDITOR"
	ReasonKnownCreditor         Reason = "KNOWN_CREDITOR"
	ReasonMandateTypeNotChecked Reason = "MANDATE_TYPE_NOT_CHECKED"

	ReasonAboveMandateMax   Reason = "ABOVE_MANDATE_MAX"
	ReasonWithinMandateMax  Reason = "WITHIN_MANDATE_MAX"
	ReasonMandateMaxUnknown Reason = "MANDATE_MAX_UNKNOWN"

	ReasonRepresentedDebit Reason = "REPRESENTED_DEBIT"
	ReasonNotRepresented   Reason = "NOT_REPRESENTED"

	ReasonHighRiskMCC           Reason = "HIGH_RISK_MCC"
	ReasonHighRiskMCCSeenBefore Reason = "HIGH_RISK_MCC_SEEN_BEFORE"
	ReasonMCCNotHighRisk        Reason = "MCC_NOT_HIGH_RISK"

	ReasonHighRiskCategory    Reason = "HIGH_RISK_CATEGORY"
	ReasonCategoryNotHighRisk Reason = "CATEGORY_NOT_HIGH_RISK"
)
