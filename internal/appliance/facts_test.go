package appliance

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	management "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func factRequest(event, text string, from *time.Time) facts.SubmitEvidenceRequest {
	return facts.SubmitEvidenceRequest{
		Source: facts.Source{Producer: "host", EventID: event, Revision: "1", Fragment: "0:full", Subject: "user:a", FactKey: "preference.drink", Role: facts.RoleConfirmation}, Text: text, IdempotencyKey: event,
		Mutations: []facts.Mutation{{Transition: facts.TransitionEstablish, Subject: "user:a", Key: "preference.drink", Text: text, ValidFrom: from}},
	}
}
func factRead() facts.ReadRequest {
	return facts.ReadRequest{Subject: "user:a", Budget: facts.Budget{MaxFacts: 8, MaxBytes: 8192}}
}
func submitFact(t *testing.T, s *Store, auth memory.CallAuthorization, r facts.SubmitEvidenceRequest) facts.SubmitEvidenceResponse {
	t.Helper()
	out, err := s.SubmitEvidence(t.Context(), auth, r)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted || len(out.Facts) == 0 {
		t.Fatalf("submit=%+v", out)
	}
	return out
}
func wantFact(t *testing.T, s *Store, auth memory.CallAuthorization, r facts.ReadRequest, text string) facts.ReadResponse {
	t.Helper()
	out, err := s.ReadFacts(t.Context(), auth, r)
	if err != nil {
		t.Fatal(err)
	}
	if text == "" {
		if len(out.Facts) != 0 || out.Background != "" {
			t.Fatalf("expected unknown, got %+v", out)
		}
	} else if len(out.Facts) != 1 || out.Facts[0].Text != text {
		t.Fatalf("want %q, got %+v", text, out)
	}
	return out
}

func TestFactsLifecycleCurrentPastCorrectionAndException(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	s, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	defer s.Close()
	start := now.AddDate(-1, 0, 0)
	base := submitFact(t, s, auth, factRequest("coffee", "I usually drink coffee", &start))
	old := wantFact(t, s, auth, factRead(), "I usually drink coffee")
	until := now.Add(time.Hour)
	r := factRequest("exception", "No coffee this hour", &now)
	r.Mutations[0].Transition = facts.TransitionException
	r.Mutations[0].TargetRecordID = base.Facts[0].RecordID
	r.Mutations[0].ExpectedRevision = 1
	r.Mutations[0].ValidUntil = &until
	exception := submitFact(t, s, auth, r)
	out := wantFact(t, s, auth, factRead(), "No coffee this hour")
	if out.RefreshAt == nil || !out.RefreshAt.Equal(until) {
		t.Fatalf("refresh=%+v", out.RefreshAt)
	}
	later := until.Add(time.Second)
	q := factRead()
	q.AsOf = &later
	wantFact(t, s, auth, q, "I usually drink coffee")
	// A same-key permanent change keeps the past instead of pretending correction.
	r = factRequest("tea", "I now drink tea", &until)
	r.Mutations[0].Transition = facts.TransitionChange
	r.Mutations[0].TargetRecordID = base.Facts[0].RecordID
	r.Mutations[0].ExpectedRevision = 1
	submitFact(t, s, auth, r)
	wantFact(t, s, auth, q, "I now drink tea")
	wantFact(t, s, auth, factRead(), "No coffee this hour")
	past := now.Add(-time.Hour)
	q.AsOf = &past
	wantFact(t, s, auth, q, "I usually drink coffee")
	history, err := s.FactHistory(t.Context(), auth, facts.HistoryRequest{Subject: "user:a", RecordID: base.Facts[0].RecordID, Budget: factRead().Budget})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Facts) != 2 || history.Facts[0].HistoricalState != "changed" || history.Background != "" {
		t.Fatalf("history=%+v", history)
	}
	r = factRequest("correct", "Actually I drink water, tea was a misunderstanding", &until)
	r.Mutations[0].Transition = facts.TransitionCorrect
	r.Mutations[0].TargetRecordID = base.Facts[0].RecordID
	r.Mutations[0].ExpectedRevision = 2
	submitFact(t, s, auth, r)
	history, err = s.FactHistory(t.Context(), auth, facts.HistoryRequest{Subject: "user:a", RecordID: base.Facts[0].RecordID, Budget: factRead().Budget})
	if err != nil {
		t.Fatal(err)
	}
	if history.Facts[1].HistoricalState != "corrected" {
		t.Fatalf("correction masquerades as change: %+v", history)
	}
	changes, err := s.Changes(t.Context(), auth, facts.ChangesRequest{After: old.Cursor, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 2 || !changes.HasMore || changes.ResetRequired {
		t.Fatalf("changes=%+v", changes)
	}
	// Legacy Recall still owns raw evidence semantics after a fact revision changes.
	raw, err := s.Recall(t.Context(), auth, testRecall("coffee", base.ConsistencyToken))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, raw, "I usually drink coffee")
	if _, err = s.DeleteReceipt(t.Context(), management.DeleteReceiptRequest{ReceiptID: exception.ReceiptID, Reason: "forget exception", IdempotencyKey: "forget-exception"}); err != nil {
		t.Fatal(err)
	}
	q.AsOf = &later
	wantFact(t, s, auth, q, "Actually I drink water, tea was a misunderstanding")
}

func TestFactsSourceAdmissionAndSuppression(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	r := factRequest("source", "coffee", nil)
	accepted := submitFact(t, s, auth, r)
	r.IdempotencyKey = "new-call"
	duplicate, err := s.SubmitEvidence(t.Context(), auth, r)
	if err != nil || !duplicate.Deduplicated || duplicate.ReceiptID != accepted.ReceiptID {
		t.Fatalf("dedup=%+v %v", duplicate, err)
	}
	r.Text = "different"
	if _, err = s.SubmitEvidence(t.Context(), auth, r); !memory.IsCode(err, memory.ErrorCodeConflict) {
		t.Fatalf("source conflict=%v", err)
	}
	r.Text = "coffee"
	if _, err = s.DeleteReceipt(t.Context(), management.DeleteReceiptRequest{ReceiptID: accepted.ReceiptID, Reason: "forget", IdempotencyKey: "forget"}); err != nil {
		t.Fatal(err)
	}
	r.IdempotencyKey = "third-call"
	suppressed, err := s.SubmitEvidence(t.Context(), auth, r)
	if err != nil || suppressed.Accepted || suppressed.RejectionReason != "source_suppressed" {
		t.Fatalf("suppression=%+v %v", suppressed, err)
	}
	if err = s.SetIngestionPolicy(t.Context(), facts.IngestionPolicy{Scope: facts.Scope{SpaceID: "space-bot-a"}, Producer: "host", Subject: "user:a", Deny: true, Reason: "user opted out"}); err != nil {
		t.Fatal(err)
	}
	r.Source.EventID = "new-source"
	r.IdempotencyKey = "new-source"
	blocked, err := s.SubmitEvidence(t.Context(), auth, r)
	if err != nil || blocked.Accepted || !strings.Contains(blocked.RejectionReason, "user opted out") {
		t.Fatalf("policy=%+v %v", blocked, err)
	}
	var receipts, jobs int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM steward_jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 || jobs != 0 {
		t.Fatalf("blocked ingestion created receipts=%d jobs=%d", receipts, jobs)
	}
	wantFact(t, s, auth, factRead(), "")
}

func TestFactsPendingSubjectIsolationAtomicityAndDenial(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	r := factRequest("inference", "The user is vegetarian", nil)
	r.Source.Role = facts.RoleInference
	r.Mutations[0].Key = "diet"
	pending := submitFact(t, s, auth, r)
	if pending.Facts[0].Metadata.Adoption != facts.AdoptionPending {
		t.Fatal("inference adopted")
	}
	wantFact(t, s, auth, factRead(), "")
	confirm := factRequest("confirm", "The user is vegetarian", nil)
	confirm.Mutations[0].Key = "diet"
	confirm.Mutations[0].Transition = facts.TransitionConfirm
	confirm.Mutations[0].TargetRecordID = pending.Facts[0].RecordID
	confirm.Mutations[0].ExpectedRevision = 1
	confirmed := submitFact(t, s, auth, confirm)
	wantFact(t, s, auth, factRead(), "The user is vegetarian")
	if len(confirmed.Facts[0].Evidence) != 2 {
		t.Fatal("confirmation lost original evidence")
	}
	deny := confirm
	deny.Source.EventID = "deny"
	deny.IdempotencyKey = "deny"
	deny.Mutations = append([]facts.Mutation(nil), confirm.Mutations...)
	deny.Mutations[0].Transition = facts.TransitionDeny
	deny.Mutations[0].ExpectedRevision = 2
	submitFact(t, s, auth, deny)
	wantFact(t, s, auth, factRead(), "")
	history, err := s.FactHistory(t.Context(), auth, facts.HistoryRequest{Subject: "user:a", RecordID: pending.Facts[0].RecordID, Budget: factRead().Budget})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range history.Facts {
		if f.Metadata.Adoption != facts.AdoptionDenied {
			t.Fatalf("denial retained historical adoption: %+v", f)
		}
	}
	wrong := factRequest("friend", "Find a vegetarian restaurant for a friend", nil)
	wrong.Mutations[0].Subject = "friend"
	if _, err = s.SubmitEvidence(t.Context(), auth, wrong); !memory.IsCode(err, memory.ErrorCodeInvalidArgument) {
		t.Fatalf("wrong subject=%v", err)
	}
	wrong.Source.Subject = "friend"
	wrong.Source.Role = facts.RoleUserQuote
	friend := submitFact(t, s, auth, wrong)
	if friend.Facts[0].Metadata.Adoption != facts.AdoptionPending {
		t.Fatal("quote elevated")
	}
	wantFact(t, s, auth, factRead(), "")
	batch := factRequest("batch", "two edits", nil)
	batch.Mutations = append(batch.Mutations, facts.Mutation{Transition: facts.TransitionChange, TargetRecordID: "absent", ExpectedRevision: 1, Subject: "user:a", Key: "other", Text: "bad", ValidFrom: func() *time.Time { v := time.Now(); return &v }()})
	if _, err = s.SubmitEvidence(t.Context(), auth, batch); !memory.IsCode(err, memory.ErrorCodeConflict) {
		t.Fatalf("batch error=%v", err)
	}
	var n int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM receipts WHERE idempotency_key='batch'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("partial batch receipt committed")
	}
}

func TestFactsConditionsBudgetRestartAndCursor(t *testing.T) {
	dir := t.TempDir()
	s, auth := newGoldenStore(t, dir, time.Now)
	r := factRequest("conditional", "Coffee only at home", nil)
	r.Mutations[0].Conditions = []facts.Condition{{Key: "location", Value: "home"}}
	submitFact(t, s, auth, r)
	wantFact(t, s, auth, factRead(), "")
	q := factRead()
	q.Context = map[string]string{"location": "home"}
	q.Query = "preferred beverage"
	first := wantFact(t, s, auth, q, "Coffee only at home")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	s, err = Open(t.Context(), Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next := wantFact(t, s, auth, q, "Coffee only at home")
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(next)
	if string(a) != string(b) {
		t.Fatalf("restart changed stable background: %s / %s", a, b)
	}
	q.Budget.MaxBytes = 512
	small, err := s.ReadFacts(t.Context(), auth, q)
	if err != nil {
		t.Fatal(err)
	}
	if !small.Truncated || small.BytesUsed > 512 {
		t.Fatalf("budget=%+v", small)
	}
	// Nil onset remains unknown when asked about the past.
	now := time.Now()
	q = factRead()
	q.Context = map[string]string{"location": "home"}
	q.AsOf = &now
	wantFact(t, s, auth, q, "")
	reset, err := s.Changes(t.Context(), auth, facts.ChangesRequest{After: facts.Cursor{}, Limit: 10})
	if err != nil || !reset.ResetRequired {
		t.Fatalf("cursor reset=%+v %v", reset, err)
	}
	other := first.Cursor
	other.Generation = "restored-generation"
	reset, err = s.Changes(t.Context(), auth, facts.ChangesRequest{After: other, Limit: 10})
	if err != nil || !reset.ResetRequired {
		t.Fatalf("generation reset=%+v %v", reset, err)
	}
}

func TestFactsAuthorizationBeforeCandidateGeneration(t *testing.T) {
	fixture := newConformanceFixture(t)
	s := fixture.Service.(*Store)
	submitFact(t, s, fixture.BotAPrivate, factRequest("private", "private coffee", nil))
	before := fixture.CandidateReads("space-bot-a")
	wantFact(t, s, fixture.SharedA, factRead(), "")
	if fixture.CandidateReads("space-bot-a") != before {
		t.Fatal("queried unauthorized private Space")
	}
	for i, auth := range []memory.CallAuthorization{fixture.BotBPrivate, fixture.BotAPrivateLabeled, fixture.BotAPrivateOther} {
		t.Run(fmt.Sprint(i), func(t *testing.T) { wantFact(t, s, auth, factRead(), "") })
	}
	if _, err := s.SubmitEvidence(t.Context(), fixture.RecallOnly, factRequest("no-write", "coffee", nil)); !memory.IsCode(err, memory.ErrorCodeUnauthorized) {
		t.Fatalf("read-only write=%v", err)
	}
	wantFact(t, s, fixture.RecallOnly, factRead(), "private coffee")
}
