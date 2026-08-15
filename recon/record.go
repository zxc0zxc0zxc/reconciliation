// Package recon compares two snapshots of records and reports where they disagree.
// It performs no I/O and reads no clock: every input is passed in explicitly.
package recon

import "time"

// Record is the unit of comparison. Every source maps its own model onto it.
type Record struct {
	ID         string            `json:"id"`
	Key        string            `json:"key"`
	Amount     int64             `json:"amount"`
	Currency   string            `json:"currency"`
	OccurredAt time.Time         `json:"occurred_at"`
	Status     string            `json:"status,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Side identifies which snapshot a record came from.
type Side string

// The two sides of a comparison, in rule order.
const (
	SideA Side = "A"
	SideB Side = "B"
)

// Window is the half-open period [From, To) a run covers.
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}
