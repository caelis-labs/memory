package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

const soakReportFormat = "memory.ga-soak.v1"

type soakReport struct {
	Format                     string           `json:"format"`
	Passed                     bool             `json:"passed"`
	GOOS                       string           `json:"goos"`
	GOARCH                     string           `json:"goarch"`
	GoVersion                  string           `json:"go_version"`
	Spaces                     int              `json:"spaces"`
	Receipts                   int              `json:"receipts"`
	Records                    int              `json:"records"`
	ReceiptStatusReads         int              `json:"receipt_status_reads"`
	RecallSamples              int              `json:"recall_samples"`
	ProvenanceChecks           int              `json:"provenance_checks"`
	PrivateLeakChecks          int              `json:"private_leak_checks"`
	PrivateLeaks               int              `json:"private_leaks"`
	RestoredReceiptStatusReads int              `json:"restored_receipt_status_reads"`
	RestoredRecallSamples      int              `json:"restored_recall_samples"`
	RestoredProvenanceChecks   int              `json:"restored_provenance_checks"`
	RestoredPrivateLeakChecks  int              `json:"restored_private_leak_checks"`
	RestoredPrivateLeaks       int              `json:"restored_private_leaks"`
	DurationsMS                map[string]int64 `json:"durations_ms"`
	Source                     soakStoreReport  `json:"source"`
	Restored                   soakStoreReport  `json:"restored"`
}

type soakStoreReport struct {
	SchemaVersion       int    `json:"schema_version"`
	StoredReceipts      int64  `json:"stored_receipts"`
	ActiveRecords       int64  `json:"active_records"`
	CompletedJobs       int64  `json:"completed_jobs"`
	PendingJobs         int64  `json:"pending_jobs"`
	ProjectionHealthy   bool   `json:"projection_healthy"`
	SemanticIndexHealth bool   `json:"semantic_index_healthy"`
	DatabaseBytes       uint64 `json:"database_bytes"`
	WALBytes            uint64 `json:"wal_bytes"`
}

type soakBinding struct {
	auth                 v1alpha1.CallAuthorization
	firstQuery           string
	firstReceipt         v1alpha1.ReceiptID
	firstSemanticQuery   string
	firstSemanticReceipt v1alpha1.ReceiptID
	firstSemanticRecord  stewardv1alpha1.RecordID
}

type receiptCheck struct {
	binding int
	id      v1alpha1.ReceiptID
}

type soakVerification struct {
	receiptStatusReads int
	recallSamples      int
	provenanceChecks   int
	privateLeakChecks  int
	privateLeaks       int
}

func executeSoak(ctx context.Context, sourceDir, restoredDir string, opts options) (report soakReport, err error) {
	report = soakReport{
		Format: soakReportFormat, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GoVersion: runtime.Version(),
		Spaces: opts.spaces, Receipts: opts.receipts, Records: opts.records,
		DurationsMS: make(map[string]int64, 8),
	}
	store, err := appliance.Open(ctx, appliance.Options{DataDir: sourceDir})
	if err != nil {
		return report, fmt.Errorf("open source appliance: %w", err)
	}
	defer func() {
		if store != nil {
			err = joinError(err, store.Close())
		}
	}()

	started := time.Now()
	bindings, err := bootstrapSoak(ctx, store, opts.spaces)
	if err != nil {
		return report, err
	}
	report.DurationsMS["bootstrap"] = elapsedMillis(started)

	checks := make([]receiptCheck, 0, opts.receipts)
	baselineReceipts := opts.receipts - opts.records
	started = time.Now()
	if err := rememberSoakBatch(ctx, store, bindings, 0, baselineReceipts, "baseline", &checks); err != nil {
		return report, err
	}
	report.DurationsMS["remember_baseline"] = elapsedMillis(started)

	if err := configureSoakSteward(ctx, store, opts.spaces); err != nil {
		return report, err
	}
	started = time.Now()
	if err := rememberSoakBatch(ctx, store, bindings, baselineReceipts, opts.records, "semantic", &checks); err != nil {
		return report, err
	}
	report.DurationsMS["remember_semantic"] = elapsedMillis(started)

	bindingByReceipt := make(map[v1alpha1.ReceiptID]int, opts.records)
	for _, check := range checks[baselineReceipts:] {
		bindingByReceipt[check.id] = check.binding
	}
	started = time.Now()
	for index := 0; index < opts.records; index++ {
		work, found, claimErr := store.ClaimStewardJob(ctx, time.Minute)
		if claimErr != nil || !found {
			return report, fmt.Errorf("claim semantic Job %d: found=%v: %w", index, found, claimErr)
		}
		applied, applyErr := store.ApplyStewardProposal(ctx, work.Lease, stewardv1alpha1.Proposal{
			Operation:    stewardv1alpha1.OperationAdd,
			Kind:         "ga-soak-claim",
			Text:         "organized " + work.Request.Receipt.Text,
			EvidenceRefs: []v1alpha1.ReceiptID{work.Request.Receipt.ReceiptID},
		})
		if applyErr != nil {
			return report, fmt.Errorf("apply semantic Job %d: %w", index, applyErr)
		}
		bindingIndex, exists := bindingByReceipt[work.Request.Receipt.ReceiptID]
		if !exists {
			return report, fmt.Errorf("semantic Job %d references an unexpected receipt", index)
		}
		if bindings[bindingIndex].firstSemanticRecord == "" {
			bindings[bindingIndex].firstSemanticRecord = applied.RecordID
		}
	}
	if _, found, claimErr := store.ClaimStewardJob(ctx, time.Minute); claimErr != nil || found {
		return report, fmt.Errorf("unexpected semantic Job after fixed backlog: found=%v: %w", found, claimErr)
	}
	report.DurationsMS["organize_records"] = elapsedMillis(started)

	if err := assertInspection(ctx, store, opts.receipts, opts.records); err != nil {
		return report, err
	}
	if err := store.Close(); err != nil {
		return report, fmt.Errorf("close before restart: %w", err)
	}
	store = nil
	started = time.Now()
	store, err = appliance.Open(ctx, appliance.Options{DataDir: sourceDir})
	if err != nil {
		return report, fmt.Errorf("restart source appliance: %w", err)
	}
	report.DurationsMS["restart"] = elapsedMillis(started)

	started = time.Now()
	verified, err := verifySoakDataPlane(ctx, store, bindings, checks)
	if err != nil {
		return report, fmt.Errorf("verify source after restart: %w", err)
	}
	report.ReceiptStatusReads = verified.receiptStatusReads
	report.RecallSamples = verified.recallSamples
	report.ProvenanceChecks = verified.provenanceChecks
	report.PrivateLeakChecks = verified.privateLeakChecks
	report.PrivateLeaks = verified.privateLeaks
	report.DurationsMS["restart_data_plane"] = elapsedMillis(started)

	started = time.Now()
	if err := store.RebuildFTS(ctx); err != nil {
		return report, fmt.Errorf("rebuild source projections: %w", err)
	}
	report.DurationsMS["reindex"] = elapsedMillis(started)
	sourceInspection, err := store.Inspect(ctx)
	if err != nil {
		return report, fmt.Errorf("inspect source: %w", err)
	}
	report.Source = projectInspection(sourceInspection)

	credentialBytes, err := os.ReadFile(store.ManagementCredentialPath())
	if err != nil {
		return report, fmt.Errorf("read source management credential: %w", err)
	}
	started = time.Now()
	snapshot, err := store.CreateBackupSnapshot(ctx)
	if err != nil {
		return report, fmt.Errorf("create source backup: %w", err)
	}
	snapshotBytes, readErr := io.ReadAll(snapshot)
	closeSnapshotErr := snapshot.Close()
	if readErr != nil || closeSnapshotErr != nil {
		return report, fmt.Errorf("read source backup: %w", joinError(readErr, closeSnapshotErr))
	}
	report.DurationsMS["backup"] = elapsedMillis(started)
	if err := store.Close(); err != nil {
		return report, fmt.Errorf("close source before restore: %w", err)
	}
	store = nil

	started = time.Now()
	if _, err := appliance.Restore(ctx, appliance.RestoreOptions{
		DataDir: restoredDir, Snapshot: bytes.NewReader(snapshotBytes),
		ManagementCredential: strings.TrimSpace(string(credentialBytes)),
	}); err != nil {
		return report, fmt.Errorf("restore soak snapshot: %w", err)
	}
	restored, err := appliance.Open(ctx, appliance.Options{DataDir: restoredDir})
	if err != nil {
		return report, fmt.Errorf("open restored appliance: %w", err)
	}
	if err := restored.CommitRestore(ctx); err != nil {
		_ = restored.Close()
		return report, fmt.Errorf("commit restored appliance: %w", err)
	}
	if err := restored.RebuildFTS(ctx); err != nil {
		_ = restored.Close()
		return report, fmt.Errorf("rebuild restored projections: %w", err)
	}
	if err := assertInspection(ctx, restored, opts.receipts, opts.records); err != nil {
		_ = restored.Close()
		return report, err
	}
	restoredVerified, err := verifySoakDataPlane(ctx, restored, bindings, checks)
	if err != nil {
		_ = restored.Close()
		return report, fmt.Errorf("verify restored data plane: %w", err)
	}
	report.RestoredReceiptStatusReads = restoredVerified.receiptStatusReads
	report.RestoredRecallSamples = restoredVerified.recallSamples
	report.RestoredProvenanceChecks = restoredVerified.provenanceChecks
	report.RestoredPrivateLeakChecks = restoredVerified.privateLeakChecks
	report.RestoredPrivateLeaks = restoredVerified.privateLeaks
	restoredInspection, err := restored.Inspect(ctx)
	if err != nil {
		_ = restored.Close()
		return report, fmt.Errorf("inspect restored appliance: %w", err)
	}
	report.Restored = projectInspection(restoredInspection)
	if err := restored.Close(); err != nil {
		return report, fmt.Errorf("close restored appliance: %w", err)
	}
	report.DurationsMS["restore_rebuild"] = elapsedMillis(started)
	report.Passed = true
	return report, nil
}

func bootstrapSoak(ctx context.Context, store *appliance.Store, spaces int) ([]soakBinding, error) {
	request := managementv1alpha1.BootstrapRequest{Realms: []managementv1alpha1.Realm{{ID: "realm-ga-soak"}}}
	request.Identities = make([]managementv1alpha1.Identity, 0, spaces)
	request.Spaces = make([]managementv1alpha1.Space, 0, spaces)
	request.Views = make([]managementv1alpha1.ViewDefinition, 0, spaces)
	request.Grants = make([]managementv1alpha1.Grant, 0, spaces)
	request.IssuerPrincipals = make([]string, 0, spaces)
	for index := 0; index < spaces; index++ {
		identityID := v1alpha1.IdentityID(fmt.Sprintf("identity-ga-%04d", index))
		spaceID := v1alpha1.SpaceID(fmt.Sprintf("space-ga-%04d", index))
		viewID := v1alpha1.ViewID(fmt.Sprintf("view-ga-%04d", index))
		grantID := v1alpha1.GrantID(fmt.Sprintf("grant-ga-%04d", index))
		actor := fmt.Sprintf("actor-ga-%04d", index)
		principal := fmt.Sprintf("principal:ga:%04d", index)
		request.Identities = append(request.Identities, managementv1alpha1.Identity{ID: identityID, RealmID: "realm-ga-soak"})
		request.Spaces = append(request.Spaces, managementv1alpha1.Space{
			ID: spaceID, RealmID: "realm-ga-soak", IdentityID: identityID, Class: v1alpha1.SpaceClassPrivate,
		})
		request.Views = append(request.Views, managementv1alpha1.ViewDefinition{
			ID: viewID, RealmID: "realm-ga-soak", ReadSpaceIDs: []v1alpha1.SpaceID{spaceID}, WriteSpaceID: spaceID,
			MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1,
		})
		request.Grants = append(request.Grants, managementv1alpha1.Grant{
			ID: grantID, PrincipalRef: principal, ActorRef: actor, ViewRef: viewID,
			AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus},
			AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: time.Now().Add(48 * time.Hour), Version: 1,
		})
		request.IssuerPrincipals = append(request.IssuerPrincipals, principal)
	}
	bootstrapped, err := store.Bootstrap(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("bootstrap soak topology: %w", err)
	}
	bindings := make([]soakBinding, spaces)
	for index := range spaces {
		principal := request.IssuerPrincipals[index]
		actor := request.Grants[index].ActorRef
		capability, err := store.IssueCapability(ctx, appliance.IssueCapabilityRequest{
			Authorization: appliance.IssuerAuthorization{PrincipalRef: principal, Credential: bootstrapped.IssuerCredentials[principal]},
			GrantRef:      request.Grants[index].ID, ActorRef: actor, Audience: v1alpha1.AudiencePrivate,
			Operations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus}, TTL: 24 * time.Hour,
		})
		if err != nil {
			return nil, fmt.Errorf("issue soak capability %d: %w", index, err)
		}
		bindings[index].auth = v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: actor, Audience: v1alpha1.AudiencePrivate}
	}
	return bindings, nil
}

func configureSoakSteward(ctx context.Context, store *appliance.Store, spaces int) error {
	profile := stewardv1alpha1.ProfileSpec{
		ProfileID: "profile-ga-soak", Version: 1, SystemPrompt: "Organize synthetic same-Space GA soak evidence.",
		MaxContextRecords: 8, MaxInputBytes: 128 << 10, MaxOutputBytes: 16 << 10,
	}
	if _, err := store.PutStewardProfile(ctx, managementv1alpha1.PutStewardProfileRequest{Profile: profile}); err != nil {
		return fmt.Errorf("create soak Steward profile: %w", err)
	}
	spaceIDs := make([]v1alpha1.SpaceID, spaces)
	for index := range spaces {
		spaceIDs[index] = v1alpha1.SpaceID(fmt.Sprintf("space-ga-%04d", index))
	}
	if _, err := store.BindStewardProfile(ctx, managementv1alpha1.BindStewardProfileRequest{
		ProfileID: profile.ProfileID, Version: profile.Version, SpaceIDs: spaceIDs,
	}); err != nil {
		return fmt.Errorf("bind soak Steward profile: %w", err)
	}
	return nil
}

func rememberSoakBatch(
	ctx context.Context,
	store *appliance.Store,
	bindings []soakBinding,
	offset, count int,
	cohort string,
	checks *[]receiptCheck,
) error {
	for relative := 0; relative < count; relative++ {
		index := offset + relative
		bindingIndex := index % len(bindings)
		query := fmt.Sprintf("ga%04dentry%08d", bindingIndex, index)
		remembered, err := store.Remember(ctx, bindings[bindingIndex].auth, v1alpha1.RememberRequest{
			Text:           fmt.Sprintf("synthetic %s receipt %s", cohort, query),
			IdempotencyKey: fmt.Sprintf("ga-soak-%08d", index),
			SourceContext:  v1alpha1.SourceContext{SourceType: "ga_soak"},
		})
		if err != nil {
			return fmt.Errorf("remember soak receipt %d: %w", index, err)
		}
		if bindings[bindingIndex].firstReceipt == "" {
			bindings[bindingIndex].firstQuery = query
			bindings[bindingIndex].firstReceipt = remembered.ReceiptID
		}
		if cohort == "semantic" && bindings[bindingIndex].firstSemanticReceipt == "" {
			bindings[bindingIndex].firstSemanticQuery = query
			bindings[bindingIndex].firstSemanticReceipt = remembered.ReceiptID
		}
		*checks = append(*checks, receiptCheck{binding: bindingIndex, id: remembered.ReceiptID})
	}
	return nil
}

func verifySoakDataPlane(
	ctx context.Context,
	store *appliance.Store,
	bindings []soakBinding,
	checks []receiptCheck,
) (soakVerification, error) {
	var result soakVerification
	for index, check := range checks {
		status, err := store.GetReceiptStatus(ctx, bindings[check.binding].auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: check.id})
		if err != nil || status.ReceiptID != check.id {
			return result, fmt.Errorf("verify receipt %d: %w", index, err)
		}
		result.receiptStatusReads++
	}
	for index, binding := range bindings {
		recalled, err := store.Recall(ctx, binding.auth, soakRecallRequest(binding.firstQuery))
		if err != nil || !containsEvidence(recalled, binding.firstReceipt) {
			return result, fmt.Errorf("verify Space %d receipt Recall: %w", index, err)
		}
		result.recallSamples++
		semantic, err := store.Recall(ctx, binding.auth, soakRecallRequest(binding.firstSemanticQuery))
		if err != nil || !containsRecordEvidence(semantic, binding.firstSemanticReceipt, binding.firstSemanticRecord) {
			return result, fmt.Errorf("verify Space %d semantic provenance: %w", index, err)
		}
		result.recallSamples++
		result.provenanceChecks++
		if len(bindings) <= 1 {
			continue
		}
		other := bindings[(index+1)%len(bindings)]
		unauthorized, err := store.Recall(ctx, binding.auth, soakRecallRequest(other.firstQuery))
		if err != nil {
			return result, fmt.Errorf("verify Space %d isolation: %w", index, err)
		}
		result.privateLeakChecks++
		if containsEvidence(unauthorized, other.firstReceipt) {
			result.privateLeaks++
			return result, fmt.Errorf("private evidence leaked into Space %d", index)
		}
	}
	return result, nil
}

func soakRecallRequest(query string) v1alpha1.RecallRequest {
	return v1alpha1.RecallRequest{
		Query:         query,
		SourceContext: v1alpha1.SourceContext{SourceType: "ga_soak"},
		Budget:        v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 32 << 10, DeadlineMS: 5_000},
	}
}

func assertInspection(ctx context.Context, store *appliance.Store, receipts, records int) error {
	inspection, err := store.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("inspect soak appliance: %w", err)
	}
	if inspection.Receipts.Stored != int64(receipts) || inspection.Steward.ActiveRecords != int64(records) ||
		inspection.Steward.CompletedJobs != int64(records) || inspection.Steward.PendingJobs != 0 ||
		!inspection.Projection.Healthy || !inspection.Steward.ProjectionHealthy {
		return fmt.Errorf("soak inspection mismatch: receipts=%d records=%d completed=%d pending=%d receipt_projection=%v semantic_projection=%v",
			inspection.Receipts.Stored, inspection.Steward.ActiveRecords, inspection.Steward.CompletedJobs,
			inspection.Steward.PendingJobs, inspection.Projection.Healthy, inspection.Steward.ProjectionHealthy)
	}
	return nil
}

func projectInspection(inspection managementv1alpha1.Inspection) soakStoreReport {
	return soakStoreReport{
		SchemaVersion: inspection.SchemaVersion, StoredReceipts: inspection.Receipts.Stored,
		ActiveRecords: inspection.Steward.ActiveRecords, CompletedJobs: inspection.Steward.CompletedJobs,
		PendingJobs: inspection.Steward.PendingJobs, ProjectionHealthy: inspection.Projection.Healthy,
		SemanticIndexHealth: inspection.Steward.ProjectionHealthy,
		DatabaseBytes:       inspection.Storage.DatabaseBytes, WALBytes: inspection.Storage.WALBytes,
	}
}

func containsEvidence(response v1alpha1.RecallResponse, receiptID v1alpha1.ReceiptID) bool {
	for _, fragment := range response.Fragments {
		for _, evidence := range fragment.EvidenceRefs {
			if evidence == receiptID {
				return true
			}
		}
	}
	return false
}

func containsRecordEvidence(response v1alpha1.RecallResponse, receiptID v1alpha1.ReceiptID, recordID stewardv1alpha1.RecordID) bool {
	for _, fragment := range response.Fragments {
		if !containsString(fragment.RecordRefs, string(recordID)) {
			continue
		}
		for _, evidence := range fragment.EvidenceRefs {
			if evidence == receiptID {
				return true
			}
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func elapsedMillis(started time.Time) int64 { return time.Since(started).Milliseconds() }

func joinError(left, right error) error {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return fmt.Errorf("%v; %w", left, right)
}
