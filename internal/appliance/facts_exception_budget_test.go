package appliance

import (
	"strings"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
)

// TestFactsExceptionBudgetOmitsRatherThanFallingBackToBase pins the facts rule
// that an applicable finite exception is the truth for its interval: if the
// exception cannot fit the read budget, the read abstains/truncates. It must
// never fall back to the base it suppresses, because that base is known to be
// inapplicable at the requested time and returning it would stale-adopt.
func TestFactsExceptionBudgetOmitsRatherThanFallingBackToBase(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	s, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	defer s.Close()

	start := now.AddDate(-1, 0, 0)
	base := submitFact(t, s, auth, factRequest("base", "I usually drink coffee", &start))

	until := now.Add(time.Hour)
	// A long exception is deliberately unable to fit a 512-byte read budget on
	// its own, so the exception is the only candidate for its key.
	long := "No coffee this hour: " + strings.Repeat("x", 400)
	r := factRequest("exception", long, &now)
	r.Mutations[0].Transition = facts.TransitionException
	r.Mutations[0].TargetRecordID = base.Facts[0].RecordID
	r.Mutations[0].ExpectedRevision = 1
	r.Mutations[0].ValidUntil = &until
	submitFact(t, s, auth, r)

	// Positive control: with room, the exception is the only fact returned.
	full := factRead()
	wantFact(t, s, auth, full, long)

	// The exception alone exceeds MaxBytes: abstain/truncate, never the base.
	tight := factRead()
	tight.Budget = facts.Budget{MaxFacts: full.Budget.MaxFacts, MaxBytes: 512}
	out, err := s.ReadFacts(t.Context(), auth, tight)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Facts) != 0 || !out.Truncated || out.Background != "" {
		t.Fatalf("exception budget omission = %+v, want abstain/truncate", out)
	}
	assertNoBaseFallback(t, out, "I usually drink coffee", long)

	// A lower-sorting competing key that takes the only MaxFacts slot must not
	// cause the suppressed base to be substituted for the omitted exception.
	diet := factRequest("diet", "The user is vegetarian", nil)
	diet.Mutations[0].Key = "diet"
	submitFact(t, s, auth, diet)
	one := factRead()
	one.Budget = facts.Budget{MaxFacts: 1, MaxBytes: full.Budget.MaxBytes}
	out, err = s.ReadFacts(t.Context(), auth, one)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Facts) != 1 || out.Facts[0].Text != "The user is vegetarian" || !out.Truncated {
		t.Fatalf("MaxFacts omission = %+v, want the competing fact plus truncation", out)
	}
	assertNoBaseFallback(t, out, "I usually drink coffee", long)
}

func assertNoBaseFallback(t *testing.T, out facts.ReadResponse, baseText, exceptionText string) {
	t.Helper()
	for _, f := range out.Facts {
		if f.Text == baseText {
			t.Fatalf("suppressed base leaked after exception budget omission: %+v", out.Facts)
		}
		if f.Text == exceptionText {
			t.Fatalf("omitted exception unexpectedly returned: %+v", out.Facts)
		}
	}
	if strings.Contains(out.Background, baseText) {
		t.Fatalf("suppressed base leaked into background: %q", out.Background)
	}
}
