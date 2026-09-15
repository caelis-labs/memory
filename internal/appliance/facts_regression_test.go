package appliance

import (
	"errors"
	"fmt"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	management "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestFactsSourceRetryRespectsPendingAndRecoveredForgetBarrier(t *testing.T) {
	dir := t.TempDir()
	s, auth := newGoldenStore(t, dir, time.Now)
	defer func() { _ = s.Close() }()
	r := factRequest("quote", "sensitive coffee preference", nil)
	r.Source.Role = facts.RoleUserQuote
	original := submitFact(t, s, auth, r)
	confirm := factRequest("confirmation", "sensitive coffee preference", nil)
	confirm.Mutations[0].Transition = facts.TransitionConfirm
	confirm.Mutations[0].TargetRecordID = original.Facts[0].RecordID
	confirm.Mutations[0].ExpectedRevision = 1
	// The same retained source also establishes an independent fact. Filtering
	// the whole retry would hide valid results rather than fence the Record.
	confirm.Mutations = append(confirm.Mutations, facts.Mutation{
		Transition: facts.TransitionEstablish, Subject: "user:a", Key: "language", Text: "English",
	})
	confirmed := submitFact(t, s, auth, confirm)
	s.faults.AfterForgettingBarrier = func() error { return errors.New("pause after committed barrier") }
	deleted, err := s.DeleteReceipt(t.Context(), management.DeleteReceiptRequest{
		ReceiptID: original.ReceiptID, Reason: "forget", IdempotencyKey: "forget",
	})
	if !memory.IsCode(err, memory.ErrorCodeUnknownOutcome) || deleted.Cleanup != management.CleanupStatePending {
		t.Fatalf("expected committed pending barrier: %+v %v", deleted, err)
	}
	for _, stage := range []string{"pending", "cleaned", "reopened"} {
		if stage == "cleaned" {
			if err := s.RecoverGovernanceCleanup(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		if stage == "reopened" {
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(t.Context(), Options{DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
		}
		confirm.IdempotencyKey = "retry-" + stage
		retry, err := s.SubmitEvidence(t.Context(), auth, confirm)
		if err != nil || !retry.Accepted || !retry.Deduplicated || retry.ReceiptID != confirmed.ReceiptID {
			t.Fatalf("%s retry: %+v %v", stage, retry, err)
		}
		if len(retry.Facts) != 1 || retry.Facts[0].Text != "English" {
			t.Fatalf("%s retry must expose only unaffected facts: %+v", stage, retry.Facts)
		}
		wantFact(t, s, auth, factRead(), "English")
	}
}

func TestFactsRepeatedExceptionCorrectionsKeepRelationAndConstraints(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	s, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	defer s.Close()
	base := submitFact(t, s, auth, factRequest("base", "coffee", nil))
	until := now.Add(30 * time.Minute)
	r := factRequest("exception", "no coffee this half hour", &now)
	r.Mutations[0].Transition = facts.TransitionException
	r.Mutations[0].TargetRecordID = base.Facts[0].RecordID
	r.Mutations[0].ExpectedRevision = 1
	r.Mutations[0].ValidUntil = &until
	ex := submitFact(t, s, auth, r)
	for i, event := range []string{"correct-one", "correct-two"} {
		r = factRequest(event, event, &now)
		r.Mutations[0].Transition = facts.TransitionCorrect
		r.Mutations[0].TargetRecordID = ex.Facts[0].RecordID
		r.Mutations[0].ExpectedRevision = uint64(i + 1)
		r.Mutations[0].ValidUntil = &until
		out := submitFact(t, s, auth, r)
		if out.Facts[0].Metadata.RelatedRecordID != base.Facts[0].RecordID {
			t.Fatalf("correction %d lost exception relation: %+v", i+1, out.Facts[0].Metadata)
		}
		wantFact(t, s, auth, factRead(), event)
	}
	// A separate future exception makes interval-overlap validation observable.
	futureStart, futureEnd := until.Add(time.Hour), until.Add(2*time.Hour)
	next := factRequest("future-exception", "future exception", &futureStart)
	next.Mutations[0].Transition = facts.TransitionException
	next.Mutations[0].TargetRecordID = base.Facts[0].RecordID
	next.Mutations[0].ExpectedRevision = 1
	next.Mutations[0].ValidUntil = &futureEnd
	submitFact(t, s, auth, next)
	for _, invalid := range []string{"change", "unbounded", "overlapping"} {
		edit := factRequest("invalid-"+invalid, "invalid edit", &now)
		edit.Mutations[0].Transition = facts.TransitionCorrect
		edit.Mutations[0].TargetRecordID = ex.Facts[0].RecordID
		edit.Mutations[0].ExpectedRevision = 3
		edit.Mutations[0].ValidUntil = &until
		switch invalid {
		case "change":
			edit.Mutations[0].Transition = facts.TransitionChange
		case "unbounded":
			edit.Mutations[0].ValidUntil = nil
		case "overlapping":
			edit.Mutations[0].ValidUntil = &futureEnd
		}
		if _, err := s.SubmitEvidence(t.Context(), auth, edit); !memory.IsCode(err, memory.ErrorCodeConflict) {
			t.Fatalf("%s exception edit: %v", invalid, err)
		}
		var receipts int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM receipts WHERE idempotency_key=?`, edit.IdempotencyKey).Scan(&receipts); err != nil || receipts != 0 {
			t.Fatalf("%s edit partially committed: receipts=%d err=%v", invalid, receipts, err)
		}
		wantFact(t, s, auth, factRead(), "correct-two")
	}
	now = until
	// The corrected exception expires; the original long-term fact resumes.
	gap := wantFact(t, s, auth, factRead(), "coffee")
	if gap.RefreshAt == nil || !gap.RefreshAt.Equal(futureStart) {
		t.Fatalf("future exception refresh: %+v", gap.RefreshAt)
	}
	now = until.Add(-time.Minute)
	deny := factRequest("deny-corrected-exception", "reject exception", &now)
	deny.Mutations[0].Transition = facts.TransitionDeny
	deny.Mutations[0].TargetRecordID = ex.Facts[0].RecordID
	deny.Mutations[0].ExpectedRevision = 3
	deny.Mutations[0].ValidUntil = &until
	submitFact(t, s, auth, deny)
	wantFact(t, s, auth, factRead(), "coffee")
}

func TestFactsQueryCannotOverrideConditionsOrResolveAmbiguity(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	a := factRequest("base", "Tokyo", nil)
	a.Source.FactKey, a.Mutations[0].Key = "location", "location"
	submitFact(t, s, auth, a)
	b := factRequest("condition", "Kyoto", nil)
	b.Source.FactKey, b.Mutations[0].Key = "location", "location"
	b.Mutations[0].Conditions = []facts.Condition{{Key: "trip", Value: "yes"}}
	submitFact(t, s, auth, b)
	q := factRead()
	q.Context = map[string]string{"trip": "yes"}
	for _, query := range []string{"", "Kyoto", "Tokyo"} {
		q.Query = query
		want := "Kyoto"
		if query == "Tokyo" {
			want = ""
		}
		wantFact(t, s, auth, q, want)
	}
	c := factRequest("conflict", "Berlin", nil)
	c.Source.FactKey, c.Mutations[0].Key = "location", "location"
	c.Mutations[0].Conditions = []facts.Condition{{Key: "meeting", Value: "yes"}}
	submitFact(t, s, auth, c)
	q.Context["meeting"] = "yes"
	for _, query := range []string{"", "Tokyo", "Kyoto", "Berlin"} {
		q.Query = query
		wantFact(t, s, auth, q, "")
	}
}

func TestStewardSubjectWithoutKeyPreservesLexicalContext(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	s, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	defer s.Close()
	old := submitFact(t, s, auth, factRequest("old-coffee", "I drink coffee", nil))
	for i := range 12 {
		now = now.Add(time.Second)
		r := factRequest(fmt.Sprintf("noise-%d", i), fmt.Sprintf("unrelated number %d", i), nil)
		r.Source.FactKey = fmt.Sprintf("noise.%d", i)
		r.Mutations[0].Key = r.Source.FactKey
		submitFact(t, s, auth, r)
	}
	putAndBindSteward(t, s, 1)
	now = now.Add(time.Second)
	r := factRequest("coffee-probe", "I no longer drink coffee", nil)
	r.Source.FactKey, r.Source.Role, r.Mutations = "", facts.RoleUserQuote, nil
	if _, err := s.SubmitEvidence(t.Context(), auth, r); err != nil {
		t.Fatal(err)
	}
	work, found, err := s.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("claim: found=%v err=%v", found, err)
	}
	if len(work.Request.Records) == 0 || string(work.Request.Records[0].RecordID) != old.Facts[0].RecordID {
		t.Fatalf("missing relevant old fact at rank 1: %+v", work.Request.Records)
	}
	if len(work.Request.Records) > work.Request.Profile.MaxContextRecords {
		t.Fatal("context exceeds profile budget")
	}
	deps, err := readStewardReadSet(t.Context(), s.db, work.Lease.JobID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, dep := range deps {
		if string(dep.recordID) == old.Facts[0].RecordID && dep.revision == 1 && dep.receiptID == old.ReceiptID {
			return
		}
	}
	t.Fatal("lexically selected fact missing from actual read dependencies")
}

func TestGovernanceClearedRevisionCountRequiresCompletedDeletion(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	receipt, err := s.Remember(t.Context(), auth, memory.RememberRequest{Text: "ordinary evidence", IdempotencyKey: "ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	applyStewardAddDirect(t, s, receipt.ReceiptID, "job-count", "ordinary retained fact")
	check := func(want int64) {
		t.Helper()
		inspection, err := s.Inspect(t.Context())
		if err != nil || inspection.Governance.ClearedRevisions != want {
			t.Fatalf("cleared=%+v want=%d err=%v", inspection.Governance, want, err)
		}
	}
	check(0)
	s.faults.AfterForgettingBarrier = func() error { return errors.New("pause") }
	if _, err := s.DeleteReceipt(t.Context(), management.DeleteReceiptRequest{ReceiptID: receipt.ReceiptID, Reason: "forget", IdempotencyKey: "forget"}); !memory.IsCode(err, memory.ErrorCodeUnknownOutcome) {
		t.Fatal(err)
	}
	check(0)
	if err := s.RecoverGovernanceCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	check(1)
	if err := s.RecoverGovernanceCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	check(1)
}
