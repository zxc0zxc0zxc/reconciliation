// Result and its parts: the typed vocabulary a run reports back.

package recon

import "time"

// Class is the kind of disagreement found.
type Class string

const (
	// ClassMissingInA marks a key present on side B only.
	ClassMissingInA Class = "MISSING_IN_A"
	// ClassMissingInB marks a key present on side A only.
	ClassMissingInB Class = "MISSING_IN_B"
	// ClassAmountMismatch marks a key whose amounts differ beyond tolerance.
	ClassAmountMismatch Class = "AMOUNT_MISMATCH"
	// ClassCurrencyMismatch marks a key held in different currencies.
	ClassCurrencyMismatch Class = "CURRENCY_MISMATCH"
	// ClassStatusMismatch marks an otherwise matching pair with conflicting statuses.
	ClassStatusMismatch Class = "STATUS_MISMATCH"
	// ClassDuplicate marks a key carried by several records on one side.
	ClassDuplicate Class = "DUPLICATE"
)

// Severity ranks a discrepancy by the amount at stake.
type Severity string

// Severity levels, ordered by the amount at stake.
const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Discrepancy is one finding. Record pointers are nil on the side that lacks the key.
type Discrepancy struct {
	Key      string   `json:"key"`
	Class    Class    `json:"class"`
	Severity Severity `json:"severity"`
	Currency string   `json:"currency,omitempty"`
	// Delta is the amount at stake: the difference for a mismatch, the amount of
	// the present side for a missing record.
	Delta   int64   `json:"delta"`
	A       *Record `json:"a,omitempty"`
	B       *Record `json:"b,omitempty"`
	Details string  `json:"details,omitempty"`
}

// Result is the outcome of one run.
type Result struct {
	Rule            string        `json:"rule"`
	Sides           [2]string     `json:"sides"`
	Window          Window        `json:"window"`
	AsOf            time.Time     `json:"as_of"`
	ScannedA        int           `json:"scanned_a"`
	ScannedB        int           `json:"scanned_b"`
	SkippedInFlight int           `json:"skipped_in_flight"`
	Matched         int           `json:"matched"`
	Discrepancies   []Discrepancy `json:"discrepancies"`
}

// Clean reports whether the run found nothing.
func (r Result) Clean() bool { return len(r.Discrepancies) == 0 }

// CountBySeverity totals findings per severity.
func (r Result) CountBySeverity() map[Severity]int {
	out := make(map[Severity]int, 4)
	for _, d := range r.Discrepancies {
		out[d.Severity]++
	}
	return out
}

// MaxSeverity returns the worst severity found, or SeverityLow for a clean run.
func (r Result) MaxSeverity() Severity {
	worst := SeverityLow
	for _, d := range r.Discrepancies {
		if severityRank(d.Severity) > severityRank(worst) {
			worst = d.Severity
		}
	}
	return worst
}

func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityHigh:
		return 2
	case SeverityMedium:
		return 1
	default:
		return 0
	}
}

func severityFor(delta int64, t SeverityThresholds) Severity {
	if delta < 0 {
		delta = -delta
	}
	t = t.orDefault()
	switch {
	case delta >= t.Critical:
		return SeverityCritical
	case delta >= t.High:
		return SeverityHigh
	case delta >= t.Medium:
		return SeverityMedium
	default:
		return SeverityLow
	}
}
