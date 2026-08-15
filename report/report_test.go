package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zxc0zxc0zxc/reconciliation/recon"
)

func result() recon.Result {
	at := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	return recon.Result{
		Rule:            "ledger-vs-provider",
		Sides:           [2]string{"ledger", "provider"},
		Window:          recon.Window{From: at.Add(-24 * time.Hour), To: at},
		AsOf:            at,
		ScannedA:        10,
		ScannedB:        9,
		SkippedInFlight: 1,
		Matched:         8,
		Discrepancies: []recon.Discrepancy{
			{Key: "op-1", Class: recon.ClassMissingInB, Severity: recon.SeverityHigh, Currency: "USD", Delta: 500, Details: "present in ledger only"},
		},
	}
}

func TestTextIncludesSummaryAndFindings(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, result()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"ledger-vs-provider",
		"2026-01-09T12:00:00Z .. 2026-01-10T12:00:00Z",
		"scanned  10 / 9 records, 1 in flight",
		"findings 1 (critical 0, high 1, medium 0, low 0)",
		"op-1",
		"MISSING_IN_B",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestTextOmitsTableOnCleanRun(t *testing.T) {
	res := result()
	res.Discrepancies = nil

	var buf bytes.Buffer
	if err := Text(&buf, res); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if strings.Contains(buf.String(), "KEY") {
		t.Errorf("clean run printed a findings table:\n%s", buf.String())
	}
}

func TestJSONRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, result()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var decoded recon.Result
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Rule != "ledger-vs-provider" || decoded.Matched != 8 {
		t.Errorf("decoded = %+v", decoded)
	}
	if len(decoded.Discrepancies) != 1 || decoded.Discrepancies[0].Class != recon.ClassMissingInB {
		t.Errorf("discrepancies = %+v", decoded.Discrepancies)
	}
}
