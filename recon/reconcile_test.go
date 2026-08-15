package recon

import (
	"testing"
	"time"
)

var (
	asOf     = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	occurred = asOf.Add(-time.Hour)
)

func rec(key string, amount int64, opts ...func(*Record)) Record {
	r := Record{
		ID:         key + "-id",
		Key:        key,
		Amount:     amount,
		Currency:   "USD",
		OccurredAt: occurred,
		Status:     "settled",
	}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func withStatus(s string) func(*Record) { return func(r *Record) { r.Status = s } }
func withCurrency(c string) func(*Record) {
	return func(r *Record) { r.Currency = c }
}
func withOccurredAt(t time.Time) func(*Record) {
	return func(r *Record) { r.OccurredAt = t }
}
func withID(id string) func(*Record) { return func(r *Record) { r.ID = id } }

func itemRule() Rule {
	return Rule{
		Name:  "ledger-vs-provider",
		Sides: [2]string{"ledger", "provider"},
		Mode:  ModeItem,
	}
}

func TestReconcileItem(t *testing.T) {
	tests := []struct {
		name    string
		rule    Rule
		a, b    []Record
		matched int
		want    []Discrepancy
	}{
		{
			name:    "identical sides match",
			rule:    itemRule(),
			a:       []Record{rec("k1", 100), rec("k2", 250)},
			b:       []Record{rec("k2", 250), rec("k1", 100)},
			matched: 2,
		},
		{
			name: "missing on each side",
			rule: itemRule(),
			a:    []Record{rec("only-a", 500)},
			b:    []Record{rec("only-b", 700)},
			want: []Discrepancy{
				{Key: "only-a", Class: ClassMissingInB, Delta: 500},
				{Key: "only-b", Class: ClassMissingInA, Delta: 700},
			},
		},
		{
			name: "amount difference beyond tolerance",
			rule: itemRule(),
			a:    []Record{rec("k1", 1000)},
			b:    []Record{rec("k1", 940)},
			want: []Discrepancy{{Key: "k1", Class: ClassAmountMismatch, Delta: 60}},
		},
		{
			name: "amount difference within tolerance matches",
			rule: func() Rule { r := itemRule(); r.AmountTolerance = 100; return r }(),
			a:    []Record{rec("k1", 1000)},
			b:    []Record{rec("k1", 940)},

			matched: 1,
		},
		{
			name: "currency difference outranks amount",
			rule: itemRule(),
			a:    []Record{rec("k1", 1000)},
			b:    []Record{rec("k1", 1000, withCurrency("EUR"))},
			want: []Discrepancy{{Key: "k1", Class: ClassCurrencyMismatch, Delta: 1000}},
		},
		{
			name: "status conflict reported only when required",
			rule: func() Rule { r := itemRule(); r.RequireStatusMatch = true; return r }(),
			a:    []Record{rec("k1", 1000)},
			b:    []Record{rec("k1", 1000, withStatus("pending"))},
			want: []Discrepancy{{Key: "k1", Class: ClassStatusMismatch, Delta: 0}},
		},
		{
			name:    "status conflict ignored by default",
			rule:    itemRule(),
			a:       []Record{rec("k1", 1000)},
			b:       []Record{rec("k1", 1000, withStatus("pending"))},
			matched: 1,
		},
		{
			name: "duplicate key on one side",
			rule: itemRule(),
			a:    []Record{rec("k1", 300), rec("k1", 300, withID("dup"))},
			b:    []Record{rec("k1", 300)},
			want: []Discrepancy{{Key: "k1", Class: ClassDuplicate, Delta: 600}},
		},
		{
			name:    "in-flight records excluded from both sides",
			rule:    func() Rule { r := itemRule(); r.InFlightWindow = 30 * time.Minute; return r }(),
			a:       []Record{rec("k1", 100), rec("fresh", 900, withOccurredAt(asOf.Add(-time.Minute)))},
			b:       []Record{rec("k1", 100)},
			matched: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Reconcile(tt.rule, tt.a, tt.b, Window{From: occurred, To: asOf}, asOf)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if got.Matched != tt.matched {
				t.Errorf("matched = %d, want %d", got.Matched, tt.matched)
			}
			if len(got.Discrepancies) != len(tt.want) {
				t.Fatalf("discrepancies = %d, want %d (%+v)", len(got.Discrepancies), len(tt.want), got.Discrepancies)
			}
			for i, want := range tt.want {
				d := got.Discrepancies[i]
				if d.Key != want.Key || d.Class != want.Class || d.Delta != want.Delta {
					t.Errorf("discrepancy[%d] = {%s %s %d}, want {%s %s %d}",
						i, d.Key, d.Class, d.Delta, want.Key, want.Class, want.Delta)
				}
			}
		})
	}
}

func TestReconcileBalance(t *testing.T) {
	rule := Rule{
		Name:            "totals",
		Sides:           [2]string{"ledger", "provider"},
		Mode:            ModeBalance,
		AmountTolerance: 10,
	}

	a := []Record{rec("k1", 1000), rec("k2", 500), rec("k3", 700, withCurrency("EUR"))}
	b := []Record{rec("x1", 1495), rec("x2", 700, withCurrency("EUR"))}

	got, err := Reconcile(rule, a, b, Window{From: occurred, To: asOf}, asOf)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got.Matched != 2 {
		t.Errorf("matched = %d, want 2 (EUR balanced, USD within tolerance)", got.Matched)
	}
	if !got.Clean() {
		t.Errorf("discrepancies = %+v, want none", got.Discrepancies)
	}

	b[0].Amount = 900
	got, err = Reconcile(rule, a, b, Window{From: occurred, To: asOf}, asOf)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(got.Discrepancies) != 1 {
		t.Fatalf("discrepancies = %d, want 1", len(got.Discrepancies))
	}
	if d := got.Discrepancies[0]; d.Key != "USD" || d.Delta != 600 {
		t.Errorf("discrepancy = {%s %d}, want {USD 600}", d.Key, d.Delta)
	}
}

func TestReconcileOrderIsStable(t *testing.T) {
	rule := itemRule()
	a := []Record{rec("c", 1), rec("a", 1), rec("b", 1)}

	first, err := Reconcile(rule, a, nil, Window{}, asOf)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	shuffled := []Record{a[1], a[2], a[0]}
	second, err := Reconcile(rule, shuffled, nil, Window{}, asOf)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for i := range first.Discrepancies {
		if first.Discrepancies[i].Key != second.Discrepancies[i].Key {
			t.Fatalf("order differs at %d: %q vs %q", i, first.Discrepancies[i].Key, second.Discrepancies[i].Key)
		}
	}
	if first.Discrepancies[0].Key != "a" {
		t.Errorf("first key = %q, want %q", first.Discrepancies[0].Key, "a")
	}
}

func TestSeverityFromThresholds(t *testing.T) {
	rule := itemRule()
	rule.Thresholds = SeverityThresholds{Medium: 100, High: 1_000, Critical: 10_000}

	tests := []struct {
		amount int64
		want   Severity
	}{
		{amount: 99, want: SeverityLow},
		{amount: 100, want: SeverityMedium},
		{amount: 5_000, want: SeverityHigh},
		{amount: 10_000, want: SeverityCritical},
		{amount: -10_000, want: SeverityCritical},
	}
	for _, tt := range tests {
		got, err := Reconcile(rule, []Record{rec("k", tt.amount)}, nil, Window{}, asOf)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if s := got.Discrepancies[0].Severity; s != tt.want {
			t.Errorf("severity(%d) = %s, want %s", tt.amount, s, tt.want)
		}
		if got.MaxSeverity() != tt.want {
			t.Errorf("MaxSeverity(%d) = %s, want %s", tt.amount, got.MaxSeverity(), tt.want)
		}
	}
}

func TestRuleValidation(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
	}{
		{name: "no name", rule: Rule{Sides: [2]string{"a", "b"}, Mode: ModeItem}},
		{name: "same sides", rule: Rule{Name: "r", Sides: [2]string{"a", "a"}, Mode: ModeItem}},
		{name: "unknown mode", rule: Rule{Name: "r", Sides: [2]string{"a", "b"}, Mode: "guess"}},
		{name: "negative tolerance", rule: Rule{Name: "r", Sides: [2]string{"a", "b"}, Mode: ModeItem, AmountTolerance: -1}},
		{
			name: "thresholds out of order",
			rule: Rule{Name: "r", Sides: [2]string{"a", "b"}, Mode: ModeItem,
				Thresholds: SeverityThresholds{Medium: 10, High: 5, Critical: 100}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.rule.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if _, err := Reconcile(tt.rule, nil, nil, Window{}, asOf); err == nil {
				t.Fatal("Reconcile() = nil error, want error")
			}
		})
	}
}
