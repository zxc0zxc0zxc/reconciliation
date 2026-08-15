// Package report renders a reconciliation result for a terminal or a pipeline.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/zxc0zxc0zxc/reconciliation/recon"
)

// JSON writes the result as indented JSON.
func JSON(w io.Writer, res recon.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return nil
}

// Text writes a human-readable summary followed by the findings.
func Text(w io.Writer, res recon.Result) error {
	counts := res.CountBySeverity()

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "rule     %s (%s vs %s)\n", res.Rule, res.Sides[0], res.Sides[1])
	fmt.Fprintf(&buf, "window   %s .. %s\n", stamp(res.Window.From), stamp(res.Window.To))
	fmt.Fprintf(&buf, "as of    %s\n", stamp(res.AsOf))
	fmt.Fprintf(&buf, "scanned  %d / %d records, %d in flight\n", res.ScannedA, res.ScannedB, res.SkippedInFlight)
	fmt.Fprintf(&buf, "matched  %d\n", res.Matched)
	fmt.Fprintf(&buf, "findings %d (critical %d, high %d, medium %d, low %d)\n",
		len(res.Discrepancies),
		counts[recon.SeverityCritical], counts[recon.SeverityHigh],
		counts[recon.SeverityMedium], counts[recon.SeverityLow])

	if !res.Clean() {
		buf.WriteString("\n")
		tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
		// tabwriter buffers rows until Flush, which is where a write error surfaces.
		row := func(format string, args ...any) {
			_, _ = io.WriteString(tw, fmt.Sprintf(format, args...))
		}
		row("KEY\tCLASS\tSEVERITY\tDELTA\tCURRENCY\tDETAILS\n")
		for _, d := range res.Discrepancies {
			row("%s\t%s\t%s\t%d\t%s\t%s\n", d.Key, d.Class, d.Severity, d.Delta, d.Currency, d.Details)
		}
		if err := tw.Flush(); err != nil {
			return fmt.Errorf("render findings: %w", err)
		}
	}

	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}
