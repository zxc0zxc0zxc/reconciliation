// Rule declares how two snapshots are compared and how much disagreement is tolerated.

package recon

import (
	"errors"
	"fmt"
	"time"
)

// Mode selects the comparison strategy.
type Mode string

const (
	// ModeItem compares record by record on the correlation key.
	ModeItem Mode = "item"
	// ModeBalance compares only the totals per currency.
	ModeBalance Mode = "balance"
)

// Rule is the full configuration of one comparison.
type Rule struct {
	Name string
	// Sides names the two sources, A first. Used for reporting only.
	Sides [2]string
	Mode  Mode
	// AmountTolerance is the absolute difference, in minor units, treated as equal.
	AmountTolerance int64
	// InFlightWindow excludes records too recent to be expected on both sides.
	InFlightWindow time.Duration
	// RequireStatusMatch turns a status conflict on an otherwise matching pair
	// into a finding.
	RequireStatusMatch bool
	Thresholds         SeverityThresholds
}

// SeverityThresholds maps the amount at stake, in minor units, onto a severity.
// A difference below Medium is Low.
type SeverityThresholds struct {
	Medium   int64
	High     int64
	Critical int64
}

// DefaultThresholds is used when a rule leaves thresholds unset.
var DefaultThresholds = SeverityThresholds{
	Medium:   1_000,
	High:     100_000,
	Critical: 10_000_000,
}

var errRuleInvalid = errors.New("invalid rule")

// Validate reports whether the rule can be executed.
func (r Rule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("%w: name is empty", errRuleInvalid)
	}
	if r.Sides[0] == "" || r.Sides[1] == "" {
		return fmt.Errorf("%w %q: both sides must be named", errRuleInvalid, r.Name)
	}
	if r.Sides[0] == r.Sides[1] {
		return fmt.Errorf("%w %q: sides must differ", errRuleInvalid, r.Name)
	}
	switch r.Mode {
	case ModeItem, ModeBalance:
	default:
		return fmt.Errorf("%w %q: unknown mode %q", errRuleInvalid, r.Name, r.Mode)
	}
	if r.AmountTolerance < 0 {
		return fmt.Errorf("%w %q: amount_tolerance must not be negative", errRuleInvalid, r.Name)
	}
	if r.InFlightWindow < 0 {
		return fmt.Errorf("%w %q: in_flight_window must not be negative", errRuleInvalid, r.Name)
	}
	return r.Thresholds.validate(r.Name)
}

func (t SeverityThresholds) validate(rule string) error {
	if t == (SeverityThresholds{}) {
		return nil
	}
	if t.Medium <= 0 || t.High <= 0 || t.Critical <= 0 {
		return fmt.Errorf("%w %q: thresholds must be positive", errRuleInvalid, rule)
	}
	if t.Medium >= t.High || t.High >= t.Critical {
		return fmt.Errorf("%w %q: thresholds must ascend medium < high < critical", errRuleInvalid, rule)
	}
	return nil
}

func (t SeverityThresholds) orDefault() SeverityThresholds {
	if t == (SeverityThresholds{}) {
		return DefaultThresholds
	}
	return t
}
