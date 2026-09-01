package appliance

import (
	"errors"
	"strings"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestStewardADDIsEvidenceBoundImmutableAndIdempotent(t *testing.T) {
	var failResponse bool
	store, auth := newGoldenStoreWithOptions(t, Options{
		DataDir: t.TempDir(), Clock: time.Now,
		Faults: Faults{AfterStewardCommit: func() error {
			if failResponse {
				return errors.New("lost response")
			}
			return nil
		}},
	})
	t.Cleanup(func() { _ = store.Close() })
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the service uses Go", IdempotencyKey: "steward-add",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseStewardReceipt(t, store, receipt.ReceiptID, "job-add")
	proposal := stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "The service uses Go.",
		EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
	}
	failResponse = true
	if _, err := store.ApplyStewardProposal(t.Context(), lease, proposal); !errors.Is(err, ErrStewardUnknownOutcome) {
		t.Fatalf("lost Apply response error = %v, want unknown outcome", err)
	}
	failResponse = false
	result, err := store.ApplyStewardProposal(t.Context(), lease, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DeduplicatedRetry || result.RecordID == "" || result.Revision != 1 {
		t.Fatalf("Apply retry = %+v", result)
	}
	record, revision, err := store.GetSemanticRecord(t.Context(), result.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != stewardv1alpha1.RecordStatusActive || record.SpaceID != "space-bot-a" || record.CurrentRevision != 1 {
		t.Fatalf("Record = %+v", record)
	}
	if revision.Text != proposal.Text || revision.JobID != lease.JobID || len(revision.Evidence) != 1 || revision.Evidence[0].ReceiptID != receipt.ReceiptID {
		t.Fatalf("Revision = %+v", revision)
	}
	changed := proposal
	changed.Text = "changed retry"
	if _, err := store.ApplyStewardProposal(t.Context(), lease, changed); !errors.Is(err, ErrStewardConflict) {
		t.Fatalf("changed Apply retry error = %v, want conflict", err)
	}
	wrongLease := lease
	wrongLease.Token = "wrong-lease-token"
	if _, err := store.ApplyStewardProposal(t.Context(), wrongLease, proposal); !errors.Is(err, ErrStewardLeaseLost) {
		t.Fatalf("wrong completed lease error = %v, want lost lease", err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`UPDATE semantic_revisions SET text = 'rewritten' WHERE record_id = ?`, result.RecordID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("semantic Revision update error = %v", err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`DELETE FROM semantic_evidence WHERE record_id = ?`, result.RecordID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("semantic Evidence delete error = %v", err)
	}
	status, err := store.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: receipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != v1alpha1.ProcessingStateOrganized || status.SemanticGeneration != "profile-test@1" {
		t.Fatalf("ReceiptStatus = %+v", status)
	}
	ignoredReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "social filler to ignore", IdempotencyKey: "steward-ignore",
	})
	if err != nil {
		t.Fatal(err)
	}
	ignoredLease := leaseStewardReceipt(t, store, ignoredReceipt.ReceiptID, "job-ignore")
	ignored, err := store.ApplyStewardProposal(t.Context(), ignoredLease, stewardv1alpha1.Proposal{Operation: stewardv1alpha1.OperationIgnore})
	if err != nil {
		t.Fatal(err)
	}
	if ignored.Operation != stewardv1alpha1.OperationIgnore || ignored.RecordID != "" || ignored.Revision != 0 {
		t.Fatalf("IGNORE result = %+v", ignored)
	}
	var recordCount int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM semantic_records`).Scan(&recordCount); err != nil {
		t.Fatal(err)
	}
	if recordCount != 1 {
		t.Fatalf("IGNORE changed Record count to %d", recordCount)
	}
}

func TestStewardProposalRejectsCrossSpaceAndMissingJobEvidence(t *testing.T) {
	store, credentials := bootstrapFixture(t, Options{DataDir: t.TempDir(), Clock: time.Now})
	t.Cleanup(func() { _ = store.Close() })
	authA := callAuth(mustIssue(t, store, credentials["principal:actor-bot-a"], issueRequest(
		"grant-bot-a", "actor-bot-a", v1alpha1.AudiencePrivate,
		[]v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus},
	)), "actor-bot-a", v1alpha1.AudiencePrivate)
	authB := callAuth(mustIssue(t, store, credentials["principal:actor-bot-b"], issueRequest(
		"grant-bot-b", "actor-bot-b", v1alpha1.AudiencePrivate,
		[]v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus},
	)), "actor-bot-b", v1alpha1.AudiencePrivate)
	receiptA, err := store.Remember(t.Context(), authA, v1alpha1.RememberRequest{Text: "private A", IdempotencyKey: "private-a"})
	if err != nil {
		t.Fatal(err)
	}
	receiptB, err := store.Remember(t.Context(), authB, v1alpha1.RememberRequest{Text: "private B", IdempotencyKey: "private-b"})
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseStewardReceipt(t, store, receiptA.ReceiptID, "job-cross-space")
	for name, evidence := range map[string][]v1alpha1.ReceiptID{
		"cross Space":         {receiptA.ReceiptID, receiptB.ReceiptID},
		"missing job receipt": {receiptB.ReceiptID},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
				Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "must not apply", EvidenceRefs: evidence,
			})
			if !errors.Is(err, ErrStewardProposalInvalid) {
				t.Fatalf("Apply error = %v, want invalid proposal", err)
			}
		})
	}
	var records int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM semantic_records`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("invalid proposals created %d Records", records)
	}
}

func TestStewardMergeAndSupersedeUseOptimisticRevisions(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	remember := func(text, key, jobID string) (v1alpha1.ReceiptID, StewardLease) {
		t.Helper()
		response, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: text, IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		return response.ReceiptID, leaseStewardReceipt(t, store, response.ReceiptID, stewardv1alpha1.JobID(jobID))
	}
	receiptA, leaseA := remember("project language is Go", "merge-a", "job-merge-a")
	added, err := store.ApplyStewardProposal(t.Context(), leaseA, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "The project uses Go.", EvidenceRefs: []v1alpha1.ReceiptID{receiptA},
	})
	if err != nil {
		t.Fatal(err)
	}
	receiptB, leaseB := remember("project targets Go 1.25", "merge-b", "job-merge-b")
	merged, err := store.ApplyStewardProposal(t.Context(), leaseB, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationMerge, TargetRecordID: added.RecordID, ExpectedRevision: 1,
		Kind: "claim", Text: "The project uses Go and targets Go 1.25.", EvidenceRefs: []v1alpha1.ReceiptID{receiptB, receiptA},
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.RecordID != added.RecordID || merged.Revision != 2 {
		t.Fatalf("MERGE result = %+v", merged)
	}
	receiptC, leaseC := remember("project now targets Go 1.26", "merge-c", "job-merge-c")
	stale := stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationSupersede, TargetRecordID: added.RecordID, ExpectedRevision: 1,
		Kind: "claim", Text: "The project targets Go 1.26.", EvidenceRefs: []v1alpha1.ReceiptID{receiptC},
	}
	if _, err := store.ApplyStewardProposal(t.Context(), leaseC, stale); !errors.Is(err, ErrStewardConflict) {
		t.Fatalf("stale SUPERSEDE error = %v, want conflict", err)
	}
	stale.ExpectedRevision = 2
	superseded, err := store.ApplyStewardProposal(t.Context(), leaseC, stale)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Revision != 3 {
		t.Fatalf("SUPERSEDE result = %+v", superseded)
	}
	_, revision, err := store.GetSemanticRecord(t.Context(), added.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if revision.Revision != 3 || revision.Text != stale.Text || len(revision.Evidence) != 1 || revision.Evidence[0].ReceiptID != receiptC {
		t.Fatalf("current Revision = %+v", revision)
	}
}

func TestReceiptGovernanceInvalidatesCurrentSemanticRecord(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{Text: "obsolete semantic fact", IdempotencyKey: "semantic-governance"})
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseStewardReceipt(t, store, receipt.ReceiptID, "job-semantic-governance")
	result, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "Obsolete semantic fact.", EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CorrectReceipt(t.Context(), managementv1alpha1.CorrectReceiptRequest{
		ReceiptID: receipt.ReceiptID, ReplacementText: "current semantic fact", Reason: "verified",
		IdempotencyKey: "correct-semantic-governance",
	}); err != nil {
		t.Fatal(err)
	}
	record, revision, err := store.GetSemanticRecord(t.Context(), result.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != stewardv1alpha1.RecordStatusInvalidated || record.InvalidatedReason != "receipt_corrected" {
		t.Fatalf("governed Record = %+v", record)
	}
	if revision.Text != "Obsolete semantic fact." || len(revision.Evidence) != 1 {
		t.Fatalf("governance rewrote immutable Revision: %+v", revision)
	}
	var projected int
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM `+semanticSpaceIndexTable("space-bot-a")+` WHERE record_id = ?`, result.RecordID).Scan(&projected); err != nil {
		t.Fatal(err)
	}
	if projected != 0 {
		t.Fatalf("invalidated Record projection count = %d", projected)
	}
	deletedReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "semantic fact to delete", IdempotencyKey: "semantic-delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	deletedLease := leaseStewardReceipt(t, store, deletedReceipt.ReceiptID, "job-semantic-delete")
	deletedRecord, err := store.ApplyStewardProposal(t.Context(), deletedLease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "Semantic fact to delete.",
		EvidenceRefs: []v1alpha1.ReceiptID{deletedReceipt.ReceiptID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: deletedReceipt.ReceiptID, Reason: "approved", IdempotencyKey: "delete-semantic-record",
	}); err != nil {
		t.Fatal(err)
	}
	record, revision, err = store.GetSemanticRecord(t.Context(), deletedRecord.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != stewardv1alpha1.RecordStatusInvalidated || record.InvalidatedReason != "receipt_deleted" || revision.Text != "Semantic fact to delete." {
		t.Fatalf("deleted-evidence Record = %+v, Revision = %+v", record, revision)
	}
	pendingReceipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "pending fact corrected before apply", IdempotencyKey: "semantic-pending-correction",
	})
	if err != nil {
		t.Fatal(err)
	}
	pendingLease := leaseStewardReceipt(t, store, pendingReceipt.ReceiptID, "job-semantic-pending")
	if _, err := store.CorrectReceipt(t.Context(), managementv1alpha1.CorrectReceiptRequest{
		ReceiptID: pendingReceipt.ReceiptID, ReplacementText: "corrected before apply", Reason: "verified",
		IdempotencyKey: "correct-semantic-pending",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyStewardProposal(t.Context(), pendingLease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "must not apply",
		EvidenceRefs: []v1alpha1.ReceiptID{pendingReceipt.ReceiptID},
	}); !errors.Is(err, ErrStewardLeaseLost) {
		t.Fatalf("governed pending job Apply error = %v, want lost lease", err)
	}
	status, err := store.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: pendingReceipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != v1alpha1.ProcessingStateFailed || status.TerminalErrorCode != "receipt_corrected" {
		t.Fatalf("governed pending ReceiptStatus = %+v", status)
	}
}

func leaseStewardReceipt(
	t *testing.T,
	store *Store,
	receiptID v1alpha1.ReceiptID,
	jobID stewardv1alpha1.JobID,
) StewardLease {
	t.Helper()
	now := store.now().UTC()
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO steward_profiles(
		 profile_id, version, provider_ref, model, system_prompt, max_context_records,
		 max_input_bytes, max_output_bytes, created_at)
		 VALUES ('profile-test', 1, 'provider-test', 'model-test', 'organize evidence', 8, 65536, 16384, ?)
		 ON CONFLICT(profile_id, version) DO NOTHING`, formatTime(now)); err != nil {
		t.Fatal(err)
	}
	var spaceID v1alpha1.SpaceID
	if err := store.db.QueryRowContext(t.Context(), `SELECT space_id FROM receipts WHERE receipt_id = ?`, receiptID).Scan(&spaceID); err != nil {
		t.Fatal(err)
	}
	token := "lease-token-" + string(jobID)
	if _, err := store.db.ExecContext(t.Context(),
		`INSERT INTO steward_jobs(
		 job_id, receipt_id, space_id, profile_id, profile_version, state, attempts,
		 available_at, lease_expires_at, lease_token_digest, created_at, updated_at)
		 VALUES (?, ?, ?, 'profile-test', 1, 'leased', 1, ?, ?, ?, ?, ?)`,
		jobID, receiptID, spaceID, formatTime(now), formatTime(now.Add(time.Hour)), digestString(token), formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`UPDATE receipt_processing SET state = 'processing', attempts = 1, last_attempt_at = ? WHERE receipt_id = ?`,
		formatTime(now), receiptID); err != nil {
		t.Fatal(err)
	}
	return StewardLease{JobID: jobID, Token: token}
}
