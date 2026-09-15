package appliance

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	management "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	steward "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// These executable regressions are derived from the PR #2 external review;
// the review's linked test attachment was not available through read_thread.
func TestPR2ReviewCorrectedChangeRebuildsIntervals(t *testing.T) {
	for _, days := range [][]int{{20}, {5}, {20, 5, 25}} {
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
			now := day(15)
			dir := t.TempDir()
			s, auth := newGoldenStore(t, dir, func() time.Time { return now })
			defer func() { _ = s.Close() }()
			start := day(1)
			base := submitFact(t, s, auth, factRequest("base", "coffee", &start))
			start = day(10)
			edit := factRequest("change", "tea", &start)
			edit.Mutations[0].Transition = facts.TransitionChange
			edit.Mutations[0].TargetRecordID = base.Facts[0].RecordID
			edit.Mutations[0].ExpectedRevision = 1
			submitFact(t, s, auth, edit)
			for i, d := range days {
				start = day(d)
				edit = factRequest(fmt.Sprintf("correct-%d", i), "tea", &start)
				edit.Mutations[0].Transition = facts.TransitionCorrect
				edit.Mutations[0].TargetRecordID = base.Facts[0].RecordID
				edit.Mutations[0].ExpectedRevision = uint64(i + 2)
				submitFact(t, s, auth, edit)
			}
			boundary := day(days[len(days)-1])
			for _, stage := range []string{"live", "reopened"} {
				if stage == "reopened" {
					if err := s.Close(); err != nil {
						t.Fatal(err)
					}
					var err error
					s, err = Open(t.Context(), Options{DataDir: dir, Clock: func() time.Time { return now }})
					if err != nil {
						t.Fatal(err)
					}
				}
				want := "coffee"
				if !now.Before(boundary) {
					want = "tea"
				}
				wantFact(t, s, auth, factRead(), want)
				for _, at := range []time.Time{boundary.Add(-time.Second), boundary} {
					q := factRead()
					q.AsOf = &at
					want = "coffee"
					if !at.Before(boundary) {
						want = "tea"
					}
					wantFact(t, s, auth, q, want)
				}
				h, err := s.FactHistory(t.Context(), auth, facts.HistoryRequest{Subject: "user:a", RecordID: base.Facts[0].RecordID, Budget: factRead().Budget})
				if err != nil {
					t.Fatal(err)
				}
				if len(h.Facts) != len(days)+2 || h.Facts[0].Metadata.ValidUntil == nil || !h.Facts[0].Metadata.ValidUntil.Equal(boundary) || h.Facts[0].HistoricalState != "changed" {
					t.Fatalf("%s: wrong predecessor interval: %+v", stage, h.Facts)
				}
				for _, f := range h.Facts[1 : len(h.Facts)-1] {
					if f.HistoricalState != "corrected" {
						t.Fatalf("error retained as effective: %+v", f)
					}
				}
			}
		})
	}
}

func TestPR2ReviewCorrectCannotForkConfirmedFact(t *testing.T) {
	for _, value := range []string{"coffee", "water"} {
		t.Run(value, func(t *testing.T) {
			s, auth := newGoldenStore(t, t.TempDir(), time.Now)
			defer s.Close()
			submitFact(t, s, auth, factRequest("base", "coffee", nil))
			quote := factRequest("quote", "cola", nil)
			quote.Source.Role = facts.RoleUserQuote
			quote.Mutations = nil
			receipt, err := s.SubmitEvidence(t.Context(), auth, quote)
			if err != nil {
				t.Fatal(err)
			}
			added := applyStewardAddDirect(t, s, receipt.ReceiptID, "pending-job", "cola")
			edit := factRequest("correct", "corrected to "+value, nil)
			edit.Mutations[0].Text = value
			edit.Mutations[0].Transition = facts.TransitionCorrect
			edit.Mutations[0].TargetRecordID = string(added.RecordID)
			edit.Mutations[0].ExpectedRevision = 1
			before, err := s.ReadFacts(t.Context(), auth, factRead())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SubmitEvidence(t.Context(), auth, edit); !memory.IsCode(err, memory.ErrorCodeConflict) {
				t.Fatalf("correction fork accepted: %v", err)
			}
			f, err := readFactRevision(t.Context(), s.db, string(added.RecordID), 1)
			if err != nil || f.Metadata.Adoption != facts.AdoptionPending {
				t.Fatalf("pending head changed: %+v %v", f, err)
			}
			var count int
			if err = s.db.QueryRow(`SELECT COUNT(*) FROM receipts WHERE idempotency_key='correct'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial receipt: %d %v", count, err)
			}
			var rev uint64
			if err = s.db.QueryRow(`SELECT current_revision FROM semantic_records WHERE record_id=?`, added.RecordID).Scan(&rev); err != nil || rev != 1 {
				t.Fatalf("partial revision: %d %v", rev, err)
			}
			after := wantFact(t, s, auth, factRead(), "coffee")
			if before.Cursor != after.Cursor {
				t.Fatal("failed correction advanced change cursor")
			}
		})
	}
}

func TestPR2ReviewGovernanceReadsUseOneConnection(t *testing.T) {
	for _, operation := range []string{"list", "trace"} {
		t.Run(operation, func(t *testing.T) {
			s, auth := newGoldenStore(t, t.TempDir(), time.Now)
			defer s.Close()
			base := submitFact(t, s, auth, factRequest("base", "coffee", nil))
			edit := factRequest("correct", "tea", nil)
			edit.Mutations[0].Transition = facts.TransitionCorrect
			edit.Mutations[0].TargetRecordID = base.Facts[0].RecordID
			edit.Mutations[0].ExpectedRevision = 1
			submitFact(t, s, auth, edit)
			// Preserve the production pool size. Seven checked-out connections force
			// the entire read through the remaining connection, with no timing race.
			if s.db.Stats().MaxOpenConnections != 8 {
				t.Fatal("unexpected production pool size")
			}
			held := make([]*sql.Conn, 0, 7)
			defer func() {
				for _, c := range held {
					_ = c.Close()
				}
			}()
			for range 7 {
				c, err := s.db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				held = append(held, c)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if operation == "list" {
				out, err := s.ListRecords(ctx, management.ListRecordsRequest{SpaceID: base.Facts[0].SpaceID, Limit: 10})
				if err != nil || len(out.Records) != 1 || out.Records[0].Record.CurrentRevision != 2 {
					t.Fatalf("list with one available connection: %+v %v", out, err)
				}
			} else {
				out, err := s.TraceRecord(ctx, management.TraceRecordRequest{RecordID: steward.RecordID(base.Facts[0].RecordID)})
				if err != nil || len(out.Revisions) != 2 {
					t.Fatalf("trace with one available connection: %+v %v", out, err)
				}
				for _, r := range out.Revisions {
					if r.Text == "" || len(r.Evidence) == 0 {
						t.Fatalf("missing audit payload: %+v", r)
					}
				}
			}
		})
	}
}

func TestPR2ReviewCorrectionCannotCrossPreviousEffectiveOnset(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	day := func(d int) *time.Time { v := time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC); return &v }
	base := submitFact(t, s, auth, factRequest("base", "coffee", day(1)))
	for i, d := range []int{10, 20} {
		edit := factRequest(fmt.Sprintf("change-%d", i), fmt.Sprintf("drink-%d", i), day(d))
		edit.Mutations[0].Transition = facts.TransitionChange
		edit.Mutations[0].TargetRecordID = base.Facts[0].RecordID
		edit.Mutations[0].ExpectedRevision = uint64(i + 1)
		submitFact(t, s, auth, edit)
	}
	for _, d := range []int{0, 5, 10} {
		edit := factRequest(fmt.Sprintf("invalid-%d", d), "water", day(d))
		edit.Mutations[0].Transition = facts.TransitionCorrect
		edit.Mutations[0].TargetRecordID = base.Facts[0].RecordID
		edit.Mutations[0].ExpectedRevision = 3
		if d == 0 {
			edit.Mutations[0].ValidFrom = nil
		}
		if _, err := s.SubmitEvidence(t.Context(), auth, edit); !memory.IsCode(err, memory.ErrorCodeConflict) {
			t.Fatalf("onset crossing previous effective interval accepted: day=%d err=%v", d, err)
		}
	}
}

func TestPR2ReviewConfirmedCorrectionChainThenChange(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	day := func(d int) *time.Time { v := time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC); return &v }
	r := factRequest("pending", "coffee", day(1))
	r.Source.Role = facts.RoleUserQuote
	base := submitFact(t, s, auth, r)
	edits := []struct {
		transition facts.Transition
		day        int
		text       string
	}{
		{facts.TransitionConfirm, 1, "coffee"},
		{facts.TransitionCorrect, 2, "coffee"},
		{facts.TransitionChange, 10, "tea"},
		{facts.TransitionCorrect, 20, "tea"},
		{facts.TransitionChange, 25, "water"},
	}
	for i, e := range edits {
		r = factRequest(fmt.Sprintf("edit-%d", i), e.text, day(e.day))
		r.Mutations[0].Transition = e.transition
		r.Mutations[0].TargetRecordID = base.Facts[0].RecordID
		r.Mutations[0].ExpectedRevision = uint64(i + 1)
		submitFact(t, s, auth, r)
	}
	for _, e := range []struct {
		day  int
		text string
	}{{1, ""}, {2, "coffee"}, {19, "coffee"}, {20, "tea"}, {24, "tea"}, {25, "water"}} {
		q := factRead()
		q.AsOf = day(e.day)
		wantFact(t, s, auth, q, e.text)
	}
}

func TestPR2ReviewFiniteChangeCorrectionCannotEraseOnset(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	s, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	defer s.Close()
	start, end := now.AddDate(0, 0, -15), now.AddDate(0, 0, -5)
	base := submitFact(t, s, auth, factRequest("base", "coffee", nil))
	r := factRequest("change", "tea", &start)
	r.Mutations[0].Transition = facts.TransitionChange
	r.Mutations[0].TargetRecordID = base.Facts[0].RecordID
	r.Mutations[0].ExpectedRevision = 1
	r.Mutations[0].ValidUntil = &end
	submitFact(t, s, auth, r)
	wantFact(t, s, auth, factRead(), "")
	r = factRequest("correct", "tea", nil)
	r.Mutations[0].Transition = facts.TransitionCorrect
	r.Mutations[0].TargetRecordID = base.Facts[0].RecordID
	r.Mutations[0].ExpectedRevision = 2
	r.Mutations[0].ValidUntil = &end
	if _, err := s.SubmitEvidence(t.Context(), auth, r); !memory.IsCode(err, memory.ErrorCodeConflict) {
		t.Fatalf("finite change lost onset: %v", err)
	}
	wantFact(t, s, auth, factRead(), "")
}
