package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zxc0zxc0zxc/reconciliation/recon"
	"github.com/zxc0zxc0zxc/reconciliation/source"
)

type fakeFetcher struct {
	name     string
	snapshot source.Snapshot
	err      error
	delay    time.Duration
}

func (f fakeFetcher) Name() string { return f.name }

func (f fakeFetcher) Fetch(ctx context.Context, _ recon.Window) (source.Snapshot, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return source.Snapshot{}, ctx.Err()
		}
	}
	if f.err != nil {
		return source.Snapshot{}, f.err
	}
	return f.snapshot, nil
}

var (
	now    = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	window = recon.Window{From: now.Add(-24 * time.Hour), To: now}
)

func record(key string, amount int64) recon.Record {
	return recon.Record{
		ID:         key,
		Key:        key,
		Amount:     amount,
		Currency:   "USD",
		OccurredAt: now.Add(-2 * time.Hour),
		Status:     "settled",
	}
}

func rule() recon.Rule {
	return recon.Rule{Name: "r", Sides: [2]string{"ledger", "provider"}, Mode: recon.ModeItem}
}

func TestRunReconcilesBothSides(t *testing.T) {
	a := fakeFetcher{name: "ledger", snapshot: source.Snapshot{
		Source: "ledger", AsOf: now, Records: []recon.Record{record("k1", 100), record("k2", 200)},
	}}
	b := fakeFetcher{name: "provider", snapshot: source.Snapshot{
		Source: "provider", AsOf: now, Records: []recon.Record{record("k1", 100)},
	}}

	res, err := Run(context.Background(), rule(), a, b, window)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Matched != 1 {
		t.Errorf("matched = %d, want 1", res.Matched)
	}
	if len(res.Discrepancies) != 1 || res.Discrepancies[0].Class != recon.ClassMissingInB {
		t.Fatalf("discrepancies = %+v, want one MISSING_IN_B", res.Discrepancies)
	}
}

func TestRunUsesEarliestAsOf(t *testing.T) {
	r := rule()
	r.InFlightWindow = time.Hour

	lagging := now.Add(-90 * time.Minute)
	a := fakeFetcher{name: "ledger", snapshot: source.Snapshot{
		AsOf: now, Records: []recon.Record{record("k1", 100)},
	}}
	b := fakeFetcher{name: "provider", snapshot: source.Snapshot{AsOf: lagging}}

	res, err := Run(context.Background(), r, a, b, window)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.AsOf.Equal(lagging) {
		t.Errorf("as-of = %s, want %s", res.AsOf, lagging)
	}
	if res.SkippedInFlight != 1 {
		t.Errorf("skipped in flight = %d, want 1", res.SkippedInFlight)
	}
	if !res.Clean() {
		t.Errorf("discrepancies = %+v, want none: the record is still in flight", res.Discrepancies)
	}
}

func TestRunFailsWhenASideFails(t *testing.T) {
	wantErr := errors.New("provider unavailable")
	a := fakeFetcher{name: "ledger", delay: time.Second, snapshot: source.Snapshot{AsOf: now}}
	b := fakeFetcher{name: "provider", err: wantErr}

	start := time.Now()
	_, err := Run(context.Background(), rule(), a, b, window)
	if !errors.Is(err, wantErr) && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("run took %s: a failing side must cancel the other", elapsed)
	}
}

func TestRunRejectsInvalidInput(t *testing.T) {
	fetcher := fakeFetcher{snapshot: source.Snapshot{AsOf: now}}

	if _, err := Run(context.Background(), recon.Rule{}, fetcher, fetcher, window); err == nil {
		t.Error("Run with invalid rule = nil error, want error")
	}
	inverted := recon.Window{From: now, To: now.Add(-time.Hour)}
	if _, err := Run(context.Background(), rule(), fetcher, fetcher, inverted); err == nil {
		t.Error("Run with inverted window = nil error, want error")
	}
}
