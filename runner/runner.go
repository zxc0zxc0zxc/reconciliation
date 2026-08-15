// Package runner collects both sides of a rule and hands them to the engine.
package runner

import (
	"context"
	"fmt"
	"sync"

	"github.com/zxc0zxc0zxc/reconciliation/recon"
	"github.com/zxc0zxc0zxc/reconciliation/source"
)

// Run fetches both sides concurrently and reconciles them. The as-of instant is the
// earlier of the two, so a lagging source never makes records look settled too early.
func Run(ctx context.Context, rule recon.Rule, a, b source.Fetcher, window recon.Window) (recon.Result, error) {
	if err := rule.Validate(); err != nil {
		return recon.Result{}, err
	}
	if window.To.Before(window.From) {
		return recon.Result{}, fmt.Errorf("rule %q: window ends before it starts", rule.Name)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg        sync.WaitGroup
		snapshots [2]source.Snapshot
		errs      [2]error
	)
	for i, fetcher := range [2]source.Fetcher{a, b} {
		wg.Add(1)
		go func(i int, f source.Fetcher) {
			defer wg.Done()
			snapshots[i], errs[i] = f.Fetch(ctx, window)
			if errs[i] != nil {
				cancel()
			}
		}(i, fetcher)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return recon.Result{}, err
		}
	}

	asOf := window.To
	for _, s := range snapshots {
		if s.AsOf.IsZero() {
			continue
		}
		if asOf.IsZero() || s.AsOf.Before(asOf) {
			asOf = s.AsOf
		}
	}

	return recon.Reconcile(rule, snapshots[0].Records, snapshots[1].Records, window, asOf)
}
