package appliance

import (
	"errors"
	"fmt"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	management "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	steward "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestGovernanceSettlesDependentReceiptProcessing(t *testing.T) {
	for _, action := range []string{"delete", "correct", "delete-interrupted"} {
		for _, pending := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/pending=%v", action, pending), func(t *testing.T) {
				now := time.Now()
				clock := func() time.Time { return now }
				dir := t.TempDir()
				s, auth := newGoldenStore(t, dir, clock)
				defer func() { _ = s.Close() }()
				base := submitFact(t, s, auth, factRequest("base", "coffee is preferred", nil))
				putAndBindSteward(t, s, 1)
				completed, err := s.Remember(t.Context(), auth, memory.RememberRequest{Text: "completed evidence", IdempotencyKey: "completed"})
				if err != nil {
					t.Fatal(err)
				}
				work, found, err := s.ClaimStewardJob(t.Context(), time.Minute)
				if err != nil || !found {
					t.Fatalf("claim completed: %v %v", found, err)
				}
				if _, err := s.ApplyStewardProposal(t.Context(), work.Lease, steward.Proposal{Operation: steward.OperationIgnore}); err != nil {
					t.Fatal(err)
				}

				req := factRequest("dependent", "coffee remains preferred", nil)
				req.Source.Role, req.Mutations = facts.RoleUserQuote, nil
				dependent, err := s.SubmitEvidence(t.Context(), auth, req)
				if err != nil {
					t.Fatal(err)
				}
				work, found, err = s.ClaimStewardJob(t.Context(), time.Minute)
				if err != nil || !found || len(work.Request.Records) != 1 || string(work.Request.Records[0].RecordID) != base.Facts[0].RecordID {
					t.Fatalf("dependent claim: %+v %v %v", work, found, err)
				}
				if pending {
					if err := s.FailStewardJob(t.Context(), work.Lease, StewardFailure{Code: "retry", RetryAfter: time.Minute}); err != nil {
						t.Fatal(err)
					}
				}
				unrelated, err := s.Remember(t.Context(), auth, memory.RememberRequest{Text: "unclaimed unrelated evidence", IdempotencyKey: "unrelated"})
				if err != nil {
					t.Fatal(err)
				}
				if action == "delete-interrupted" {
					s.faults.AfterForgettingBarrier = func() error { return errors.New("stop before cleanup") }
				}
				reason := "receipt_deleted"
				if action == "correct" {
					reason = "receipt_corrected"
					_, err = s.CorrectReceipt(t.Context(), management.CorrectReceiptRequest{ReceiptID: base.ReceiptID, ReplacementText: "water instead", Reason: "correction", IdempotencyKey: "govern"})
				} else {
					_, err = s.DeleteReceipt(t.Context(), management.DeleteReceiptRequest{ReceiptID: base.ReceiptID, Reason: "forget", IdempotencyKey: "govern"})
				}
				if action == "delete-interrupted" {
					if !memory.IsCode(err, memory.ErrorCodeUnknownOutcome) {
						t.Fatalf("interruption: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				for _, reopen := range []bool{false, true} {
					if reopen {
						if err := s.Close(); err != nil {
							t.Fatal(err)
						}
						s, err = Open(t.Context(), Options{DataDir: dir, Clock: clock})
						if err != nil {
							t.Fatal(err)
						}
					}
					status, err := s.GetReceiptStatus(t.Context(), auth, memory.GetReceiptStatusRequest{ReceiptID: dependent.ReceiptID})
					if err != nil || status.State != memory.ProcessingStateFailed || string(status.TerminalErrorCode) != reason {
						t.Fatalf("reopen=%v dependent status=%+v error=%v", reopen, status, err)
					}
					replay, err := s.SubmitEvidence(t.Context(), auth, req)
					if err != nil || replay.Organization != facts.OrganizationFailed {
						t.Fatalf("evidence retry=%+v %v", replay, err)
					}
					remembered, err := s.Remember(t.Context(), auth, memory.RememberRequest{Text: req.Text, IdempotencyKey: req.IdempotencyKey, SourceContext: memory.SourceContext{SourceType: "trusted_evidence"}})
					if err != nil || remembered.ProcessingState != memory.ProcessingStateFailed {
						t.Fatalf("Remember retry=%+v %v", remembered, err)
					}
					for id, expected := range map[memory.ReceiptID]memory.ProcessingState{completed.ReceiptID: memory.ProcessingStateOrganized, unrelated.ReceiptID: memory.ProcessingStateAccepted} {
						status, err := s.GetReceiptStatus(t.Context(), auth, memory.GetReceiptStatusRequest{ReceiptID: id})
						if err != nil || status.State != expected {
							t.Fatalf("unaffected status=%+v expected=%s error=%v", status, expected, err)
						}
					}
					if _, err := s.ApplyStewardProposal(t.Context(), work.Lease, steward.Proposal{Operation: steward.OperationIgnore}); !errors.Is(err, ErrStewardLeaseLost) {
						t.Fatalf("cancelled lease: %v", err)
					}
				}
			})
		}
	}
}

func TestGovernanceProcessingMigrationRequiresBarrierAndRetriesAtomically(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	base := submitFact(t, s, auth, factRequest("base", "coffee", nil))
	putAndBindSteward(t, s, 1)
	dependent, err := s.Remember(t.Context(), auth, memory.RememberRequest{Text: "coffee", IdempotencyKey: "dependent"})
	if err != nil {
		t.Fatal(err)
	}
	work, found, err := s.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("claim %v %v", found, err)
	}
	if _, err := s.DeleteReceipt(t.Context(), management.DeleteReceiptRequest{ReceiptID: base.ReceiptID, Reason: "forget", IdempotencyKey: "forget"}); err != nil {
		t.Fatal(err)
	}
	// Recreate only the mutable v0.6.0 inconsistency; immutable evidence and
	// actual persisted dependencies/barriers come from the real lifecycle above.
	if _, err := s.db.Exec(`UPDATE receipt_processing SET state='processing',terminal_error_code='' WHERE receipt_id=?`, dependent.ReceiptID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM metadata WHERE key=?`, governanceProcessingMigration); err != nil {
		t.Fatal(err)
	}
	unrelated, err := s.Remember(t.Context(), auth, memory.RememberRequest{Text: "unrelated", IdempotencyKey: "unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE steward_jobs SET state='failed',terminal_error_code='receipt_deleted' WHERE receipt_id=?`, unrelated.ReceiptID); err != nil {
		t.Fatal(err)
	}
	// Wrong partition is also insufficient even with a matching read-set edge.
	if _, err := s.db.Exec(`UPDATE steward_jobs SET label_set_digest='different-partition' WHERE job_id=?`, work.Lease.JobID); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateGovernanceReceiptProcessing(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := governanceText(t, s, `SELECT state FROM receipt_processing WHERE receipt_id=?`, dependent.ReceiptID); got != "processing" {
		t.Fatalf("cross-partition repair=%s", got)
	}
	if _, err := s.db.Exec(`UPDATE steward_jobs SET label_set_digest=? WHERE job_id=?`, emptyLabelSetDigest, work.Lease.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM metadata WHERE key=?`, governanceProcessingMigration); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_processing_repair BEFORE UPDATE ON receipt_processing BEGIN SELECT RAISE(ABORT,'repair interruption'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateGovernanceReceiptProcessing(t.Context()); err == nil {
		t.Fatal("repair should fail")
	}
	if n := governanceCount(t, s, `SELECT COUNT(*) FROM metadata WHERE key=?`, governanceProcessingMigration); n != 0 {
		t.Fatal("failed repair published marker")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_processing_repair`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.migrateGovernanceReceiptProcessing(t.Context()); err != nil {
			t.Fatal(err)
		}
		status, err := s.GetReceiptStatus(t.Context(), auth, memory.GetReceiptStatusRequest{ReceiptID: dependent.ReceiptID})
		if err != nil || status.State != memory.ProcessingStateFailed || status.TerminalErrorCode != "receipt_deleted" {
			t.Fatalf("repair status=%+v %v", status, err)
		}
		status, err = s.GetReceiptStatus(t.Context(), auth, memory.GetReceiptStatusRequest{ReceiptID: unrelated.ReceiptID})
		if err != nil || status.State != memory.ProcessingStateAccepted {
			t.Fatalf("unattributed failure was repaired: %+v %v", status, err)
		}
	}
}
