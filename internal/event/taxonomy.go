package event

// TaxonomyVersion identifies the initial controlled event taxonomy.
const TaxonomyVersion = "v1"

var knownKinds = map[Kind]struct{}{
	KindUSCPI: {}, KindFOMCRateDecision: {}, KindUSNonfarmPayrolls: {},
	KindEmergencyPolicy: {}, KindElection: {}, KindReferendum: {},
	KindConflictEscalation: {}, KindBankFailure: {}, KindExchangeFailure: {},
}

// IsKnownKind reports whether k is part of the current taxonomy.
func IsKnownKind(k Kind) bool {
	_, ok := knownKinds[k]
	return ok
}
