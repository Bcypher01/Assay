package event

import "github.com/bcypher01/assay/internal/core"

// Kind is a controlled event category. Values are versioned by TaxonomyVersion.
type Kind string

const (
	KindUSCPI              Kind = "US_CPI"
	KindFOMCRateDecision   Kind = "FOMC_RATE_DECISION"
	KindUSNonfarmPayrolls  Kind = "US_NONFARM_PAYROLLS"
	KindEmergencyPolicy    Kind = "EMERGENCY_POLICY_ACTION"
	KindElection           Kind = "ELECTION"
	KindReferendum         Kind = "REFERENDUM"
	KindConflictEscalation Kind = "CONFLICT_ESCALATION"
	KindBankFailure        Kind = "BANK_FAILURE"
	KindExchangeFailure    Kind = "EXCHANGE_FAILURE"
)

// Impact is the source or curator's declared market significance.
type Impact string

const (
	ImpactLow    Impact = "low"
	ImpactMedium Impact = "medium"
	ImpactHigh   Impact = "high"
)

// Status describes the state of a record at KnowledgeAt.
type Status string

const (
	StatusScheduled Status = "scheduled"
	StatusReleased  Status = "released"
	StatusRevised   Status = "revised"
	StatusEstimated Status = "estimated"
)

// TimePrecision prevents uncertain dates from being silently treated as an
// intraday market timestamp.
type TimePrecision string

const (
	PrecisionExact TimePrecision = "exact"
	PrecisionDay   TimePrecision = "day"
	PrecisionMonth TimePrecision = "month"
)

// Decimal keeps source numeric values exact. It deliberately remains a string:
// economic releases have source-defined units and scales, so a float would both
// lose precision and imply arithmetic semantics this package does not own.
type Decimal string

// SourceRef identifies the source record that was normalised into Event.
type SourceRef struct {
	Provider string `json:"provider"`
	RecordID string `json:"recordId"`
	URL      string `json:"url,omitempty"`
}

// EvidenceRef is a human-inspectable source supporting the event record.
type EvidenceRef struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Event is the canonical, provider-independent historical event record.
// OccurredAt is when the market could react; KnowledgeAt is when this version
// of the record became available. Values are UTC epoch milliseconds.
type Event struct {
	ID           string        `json:"id"`
	OccurredAt   core.Time     `json:"occurredAt"`
	Precision    TimePrecision `json:"precision"`
	Kind         Kind          `json:"kind"`
	Title        string        `json:"title"`
	Jurisdiction string        `json:"jurisdiction"`
	Currencies   []string      `json:"currencies"`
	Impact       Impact        `json:"impact"`
	Status       Status        `json:"status"`
	Actual       *Decimal      `json:"actual,omitempty"`
	Forecast     *Decimal      `json:"forecast,omitempty"`
	Previous     *Decimal      `json:"previous,omitempty"`
	RevisionOf   string        `json:"revisionOf,omitempty"`
	KnowledgeAt  core.Time     `json:"knowledgeAt"`
	Source       SourceRef     `json:"source"`
	Evidence     []EvidenceRef `json:"evidence,omitempty"`
}

// Manifest pins one immutable event snapshot.
type Manifest struct {
	ContentHash     string    `json:"contentHash"`
	Rows            int       `json:"rows"`
	First           core.Time `json:"first"`
	Last            core.Time `json:"last"`
	Source          string    `json:"source"`
	BuildVersion    string    `json:"buildVersion"`
	TaxonomyVersion string    `json:"taxonomyVersion"`
	SchemaVersion   int       `json:"schemaVersion"`
}

// Snapshot is an immutable, content-addressed event dataset.
type Snapshot struct {
	Manifest Manifest `json:"manifest"`
	Events   []Event  `json:"events"`
}
