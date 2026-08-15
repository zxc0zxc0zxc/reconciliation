// Reconcile is the engine: two snapshots in, a typed list of disagreements out.

package recon

import (
	"fmt"
	"sort"
	"time"
)

// Reconcile compares snapshots a and b under rule. Records younger than the
// in-flight window are excluded from both sides. Findings come back in a stable order.
func Reconcile(rule Rule, a, b []Record, window Window, asOf time.Time) (Result, error) {
	if err := rule.Validate(); err != nil {
		return Result{}, err
	}

	cutoff := asOf.Add(-rule.InFlightWindow)
	liveA, skippedA := dropInFlight(a, cutoff)
	liveB, skippedB := dropInFlight(b, cutoff)

	res := Result{
		Rule:            rule.Name,
		Sides:           rule.Sides,
		Window:          window,
		AsOf:            asOf,
		ScannedA:        len(a),
		ScannedB:        len(b),
		SkippedInFlight: skippedA + skippedB,
	}

	switch rule.Mode {
	case ModeItem:
		reconcileItems(rule, liveA, liveB, &res)
	case ModeBalance:
		reconcileBalances(rule, liveA, liveB, &res)
	default:
		return Result{}, fmt.Errorf("%w %q: unknown mode %q", errRuleInvalid, rule.Name, rule.Mode)
	}
	return res, nil
}

func dropInFlight(records []Record, cutoff time.Time) (live []Record, skipped int) {
	live = make([]Record, 0, len(records))
	for _, r := range records {
		if r.OccurredAt.After(cutoff) {
			skipped++
			continue
		}
		live = append(live, r)
	}
	return live, skipped
}

func reconcileItems(rule Rule, a, b []Record, res *Result) {
	byKeyA := indexByKey(a)
	byKeyB := indexByKey(b)

	for _, key := range unionKeys(byKeyA, byKeyB) {
		groupA, groupB := byKeyA[key], byKeyB[key]

		if len(groupA) > 1 || len(groupB) > 1 {
			if len(groupA) > 1 {
				res.Discrepancies = append(res.Discrepancies, duplicate(rule, key, SideA, groupA))
			}
			if len(groupB) > 1 {
				res.Discrepancies = append(res.Discrepancies, duplicate(rule, key, SideB, groupB))
			}
			continue
		}

		switch {
		case len(groupB) == 0:
			rec := groupA[0]
			res.Discrepancies = append(res.Discrepancies, Discrepancy{
				Key:      key,
				Class:    ClassMissingInB,
				Severity: severityFor(rec.Amount, rule.Thresholds),
				Currency: rec.Currency,
				Delta:    rec.Amount,
				A:        &rec,
				Details:  fmt.Sprintf("present in %s only", rule.Sides[0]),
			})
		case len(groupA) == 0:
			rec := groupB[0]
			res.Discrepancies = append(res.Discrepancies, Discrepancy{
				Key:      key,
				Class:    ClassMissingInA,
				Severity: severityFor(rec.Amount, rule.Thresholds),
				Currency: rec.Currency,
				Delta:    rec.Amount,
				B:        &rec,
				Details:  fmt.Sprintf("present in %s only", rule.Sides[1]),
			})
		default:
			recA, recB := groupA[0], groupB[0]
			if d, ok := comparePair(rule, key, recA, recB); ok {
				res.Discrepancies = append(res.Discrepancies, d)
				continue
			}
			res.Matched++
		}
	}
}

func comparePair(rule Rule, key string, a, b Record) (Discrepancy, bool) {
	switch {
	case a.Currency != b.Currency:
		return Discrepancy{
			Key:      key,
			Class:    ClassCurrencyMismatch,
			Severity: severityFor(maxAbs(a.Amount, b.Amount), rule.Thresholds),
			Currency: a.Currency,
			Delta:    maxAbs(a.Amount, b.Amount),
			A:        &a,
			B:        &b,
			Details:  fmt.Sprintf("%s vs %s", a.Currency, b.Currency),
		}, true

	case abs(a.Amount-b.Amount) > rule.AmountTolerance:
		return Discrepancy{
			Key:      key,
			Class:    ClassAmountMismatch,
			Severity: severityFor(a.Amount-b.Amount, rule.Thresholds),
			Currency: a.Currency,
			Delta:    a.Amount - b.Amount,
			A:        &a,
			B:        &b,
			Details:  fmt.Sprintf("%d vs %d", a.Amount, b.Amount),
		}, true

	case rule.RequireStatusMatch && a.Status != b.Status:
		return Discrepancy{
			Key:      key,
			Class:    ClassStatusMismatch,
			Severity: severityFor(a.Amount, rule.Thresholds),
			Currency: a.Currency,
			Delta:    0,
			A:        &a,
			B:        &b,
			Details:  fmt.Sprintf("%s vs %s", a.Status, b.Status),
		}, true
	}
	return Discrepancy{}, false
}

func duplicate(rule Rule, key string, side Side, group []Record) Discrepancy {
	var total int64
	for _, r := range group {
		total += r.Amount
	}
	d := Discrepancy{
		Key:      key,
		Class:    ClassDuplicate,
		Severity: severityFor(total, rule.Thresholds),
		Currency: group[0].Currency,
		Delta:    total,
		Details:  fmt.Sprintf("%d records on side %s", len(group), side),
	}
	rec := group[0]
	if side == SideA {
		d.A = &rec
	} else {
		d.B = &rec
	}
	return d
}

func reconcileBalances(rule Rule, a, b []Record, res *Result) {
	totalA := totalsByCurrency(a)
	totalB := totalsByCurrency(b)

	for _, currency := range unionCurrencies(totalA, totalB) {
		delta := totalA[currency] - totalB[currency]
		if abs(delta) <= rule.AmountTolerance {
			res.Matched++
			continue
		}
		res.Discrepancies = append(res.Discrepancies, Discrepancy{
			Key:      currency,
			Class:    ClassAmountMismatch,
			Severity: severityFor(delta, rule.Thresholds),
			Currency: currency,
			Delta:    delta,
			Details:  fmt.Sprintf("%d vs %d", totalA[currency], totalB[currency]),
		})
	}
}

func indexByKey(records []Record) map[string][]Record {
	out := make(map[string][]Record, len(records))
	for _, r := range records {
		out[r.Key] = append(out[r.Key], r)
	}
	return out
}

func totalsByCurrency(records []Record) map[string]int64 {
	out := make(map[string]int64)
	for _, r := range records {
		out[r.Currency] += r.Amount
	}
	return out
}

func unionKeys(a, b map[string][]Record) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	keys := make([]string, 0, len(a)+len(b))
	for _, m := range []map[string][]Record{a, b} {
		for k := range m {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func unionCurrencies(a, b map[string]int64) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	keys := make([]string, 0, len(a)+len(b))
	for _, m := range []map[string]int64{a, b} {
		for k := range m {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxAbs(a, b int64) int64 {
	if abs(a) > abs(b) {
		return abs(a)
	}
	return abs(b)
}
