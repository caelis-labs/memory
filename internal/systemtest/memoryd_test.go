package systemtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/conformance"
	"github.com/caelis-labs/memory/internal/appliance"
	localclient "github.com/caelis-labs/memory/sdk/go/memory/local"
	managementclient "github.com/caelis-labs/memory/sdk/go/memory/management"
	"github.com/caelis-labs/memory/sdk/go/memory/sidecar"
	"github.com/caelis-labs/memory/sdk/go/memory/stewardworker"
)

func TestDurableConformanceSeparateProcess(t *testing.T) {
	conformance.RunDurable(t, newDurableProcessFixture)
}

func TestGoldenPathPrivateAndSharedSurvivesProcessRestart(t *testing.T) {
	root := shortTempDir(t)
	binary := buildMemoryd(t, root)
	dataDir := filepath.Join(root, "data")
	process := startMemoryd(t, binary, dataDir)
	t.Cleanup(func() { process.stop(t) })
	credentialBytes, err := os.ReadFile(filepath.Join(dataDir, appliance.ManagementCredentialFile))
	if err != nil {
		t.Fatal(err)
	}
	admin := managementclient.NewClientForEndpoint(process.endpoint, strings.TrimSpace(string(credentialBytes)))
	now := time.Now().UTC()
	operations := []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus}
	grant := func(id, principal, actor string, view v1alpha1.ViewID, audience v1alpha1.Audience) appliance.Grant {
		return appliance.Grant{
			ID: v1alpha1.GrantID(id), PrincipalRef: principal, ActorRef: actor, ViewRef: view,
			AllowedOperations: operations, AllowedAudiences: []v1alpha1.Audience{audience},
			ExpiresAt: now.Add(time.Hour), Version: 1,
		}
	}
	bootstrap, err := admin.Bootstrap(t.Context(), appliance.BootstrapRequest{
		Realms: []appliance.Realm{{ID: "realm-golden"}},
		Identities: []appliance.Identity{
			{ID: "identity-a", RealmID: "realm-golden"},
			{ID: "identity-b", RealmID: "realm-golden"},
		},
		Spaces: []appliance.Space{
			{ID: "space-shared", RealmID: "realm-golden", Class: v1alpha1.SpaceClassShared},
			{ID: "space-a", RealmID: "realm-golden", IdentityID: "identity-a", Class: v1alpha1.SpaceClassPrivate},
			{ID: "space-b", RealmID: "realm-golden", IdentityID: "identity-b", Class: v1alpha1.SpaceClassPrivate},
		},
		Views: []appliance.ViewDefinition{
			{ID: "view-a", RealmID: "realm-golden", ReadSpaceIDs: []v1alpha1.SpaceID{"space-shared", "space-a"}, WriteSpaceID: "space-a", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1},
			{ID: "view-b", RealmID: "realm-golden", ReadSpaceIDs: []v1alpha1.SpaceID{"space-shared", "space-b"}, WriteSpaceID: "space-b", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1},
			{ID: "view-shared", RealmID: "realm-golden", ReadSpaceIDs: []v1alpha1.SpaceID{"space-shared"}, WriteSpaceID: "space-shared", MaxDisclosureClass: v1alpha1.SpaceClassShared, Version: 1},
		},
		Grants: []appliance.Grant{
			grant("grant-a", "principal:a", "actor-a", "view-a", v1alpha1.AudiencePrivate),
			grant("grant-b", "principal:b", "actor-b", "view-b", v1alpha1.AudiencePrivate),
			grant("grant-shared", "principal:shared", "actor-shared", "view-shared", v1alpha1.AudienceShared),
		},
		IssuerPrincipals: []string{"principal:a", "principal:b", "principal:shared"},
	})
	if err != nil {
		t.Fatal(err)
	}
	issue := func(principal string, grantID v1alpha1.GrantID, actor string, audience v1alpha1.Audience) v1alpha1.CallAuthorization {
		t.Helper()
		capability, err := localclient.NewIssuerClientForEndpoint(process.endpoint, bootstrap.IssuerCredentials[principal]).IssueCapability(t.Context(), v1alpha1.CapabilityIssueRequest{
			PrincipalRef: principal, GrantRef: grantID, ActorRef: actor, Audience: audience,
			Operations: operations, TTLSeconds: 1800,
		})
		if err != nil {
			t.Fatal(err)
		}
		return v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: actor, Audience: audience}
	}
	authA := issue("principal:a", "grant-a", "actor-a", v1alpha1.AudiencePrivate)
	authB := issue("principal:b", "grant-b", "actor-b", v1alpha1.AudiencePrivate)
	authShared := issue("principal:shared", "grant-shared", "actor-shared", v1alpha1.AudienceShared)
	client := localclient.NewClientForEndpoint(process.endpoint)
	private, err := client.Remember(t.Context(), authA, v1alpha1.RememberRequest{
		Text: "commit does not authorize push", IdempotencyKey: "golden-private",
	})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := client.Remember(t.Context(), authShared, v1alpha1.RememberRequest{
		Text: "the project uses Go", IdempotencyKey: "golden-shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	process.kill(t)
	process = startMemoryd(t, binary, dataDir)
	client = localclient.NewClientForEndpoint(process.endpoint)
	recall := func(auth v1alpha1.CallAuthorization, query string, token v1alpha1.ConsistencyToken) v1alpha1.RecallResponse {
		t.Helper()
		response, err := client.Recall(t.Context(), auth, v1alpha1.RecallRequest{
			Query: query, MinConsistencyToken: token,
			Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
		})
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	assertFragment(t, recall(authA, "commit push", private.ConsistencyToken), "commit does not authorize push")
	if response := recall(authB, "commit push", ""); len(response.Fragments) != 0 {
		t.Fatalf("Bot-B recalled Bot-A private receipt after restart: %+v", response.Fragments)
	}
	assertFragment(t, recall(authA, "project Go", shared.ConsistencyToken), "the project uses Go")
	assertFragment(t, recall(authB, "project Go", shared.ConsistencyToken), "the project uses Go")
}

func TestMemoryctlStandaloneWorkflow(t *testing.T) {
	root := shortTempDir(t)
	memoryd := buildCommand(t, root, "memoryd")
	memoryctl := buildCommand(t, root, "memoryctl")
	dataDir := filepath.Join(root, "data")
	process := startMemoryd(t, memoryd, dataDir)
	t.Cleanup(func() { process.stop(t) })
	managementPath := filepath.Join(dataDir, appliance.ManagementCredentialFile)
	now := time.Now().UTC()
	bootstrapPath := filepath.Join(root, "bootstrap.json")
	writeJSONFile(t, bootstrapPath, appliance.BootstrapRequest{
		Realms:     []appliance.Realm{{ID: "realm-cli"}},
		Identities: []appliance.Identity{{ID: "identity-cli", RealmID: "realm-cli"}},
		Spaces: []appliance.Space{{
			ID: "space-cli", RealmID: "realm-cli", IdentityID: "identity-cli", Class: v1alpha1.SpaceClassPrivate,
		}},
		Views: []appliance.ViewDefinition{{
			ID: "view-cli", RealmID: "realm-cli", ReadSpaceIDs: []v1alpha1.SpaceID{"space-cli"},
			WriteSpaceID: "space-cli", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1,
		}},
		Grants: []appliance.Grant{{
			ID: "grant-cli", PrincipalRef: "principal:cli", ActorRef: "actor-cli", ViewRef: "view-cli",
			AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall},
			AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: now.Add(time.Hour), Version: 1,
		}},
		IssuerPrincipals: []string{"principal:cli"},
	})
	issuerPath := filepath.Join(root, "issuer.json")
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath,
		"bootstrap", "-file", bootstrapPath, "-issuer-output", issuerPath,
	)
	issuePath := filepath.Join(root, "issue.json")
	writeJSONFile(t, issuePath, v1alpha1.CapabilityIssueRequest{
		PrincipalRef: "principal:cli", GrantRef: "grant-cli", ActorRef: "actor-cli", Audience: v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall}, TTLSeconds: 600,
	})
	authorizationPath := filepath.Join(root, "authorization.json")
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-issuer-credential", issuerPath,
		"issue", "-file", issuePath, "-authorization-output", authorizationPath,
	)
	rememberOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"remember", "-authorization", authorizationPath,
		"-text", "memoryctl standalone fact", "-idempotency-key", "memoryctl-effect",
	)
	if !bytes.Contains(rememberOutput, []byte(`"accepted": true`)) {
		t.Fatalf("memoryctl Remember output = %s", rememberOutput)
	}
	var remembered v1alpha1.RememberResponse
	if err := json.Unmarshal(rememberOutput, &remembered); err != nil {
		t.Fatal(err)
	}
	recallOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"recall", "-authorization", authorizationPath, "-query", "standalone fact",
	)
	if !bytes.Contains(recallOutput, []byte("memoryctl standalone fact")) {
		t.Fatalf("memoryctl Recall output = %s", recallOutput)
	}
	inspectOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath, "inspect",
	)
	if !bytes.Contains(inspectOutput, []byte(`"receipts": 1`)) {
		t.Fatalf("memoryctl Inspect output = %s", inspectOutput)
	}
	searchOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath,
		"search", "-query", "standalone fact",
	)
	if !bytes.Contains(searchOutput, []byte(remembered.ReceiptID)) || !bytes.Contains(searchOutput, []byte("memoryctl standalone fact")) {
		t.Fatalf("memoryctl Search output = %s", searchOutput)
	}
	traceOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath,
		"trace-receipt", "-id", string(remembered.ReceiptID),
	)
	if !bytes.Contains(traceOutput, []byte(`"state": "active"`)) {
		t.Fatalf("memoryctl Trace output = %s", traceOutput)
	}
	correctOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath,
		"correct-receipt", "-id", string(remembered.ReceiptID),
		"-text", "memoryctl corrected fact", "-reason", "system test correction",
		"-idempotency-key", "memoryctl-correction",
	)
	var corrected managementv1alpha1.CorrectReceiptResponse
	if err := json.Unmarshal(correctOutput, &corrected); err != nil {
		t.Fatal(err)
	}
	if corrected.ReplacementReceiptID == "" {
		t.Fatalf("memoryctl Correct output = %s", correctOutput)
	}
	oldRecallOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"recall", "-authorization", authorizationPath, "-query", "standalone",
	)
	if bytes.Contains(oldRecallOutput, []byte("memoryctl standalone fact")) {
		t.Fatalf("corrected original remained in Recall output = %s", oldRecallOutput)
	}
	correctedRecallOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"recall", "-authorization", authorizationPath, "-query", "corrected",
	)
	if !bytes.Contains(correctedRecallOutput, []byte("memoryctl corrected fact")) {
		t.Fatalf("replacement missing from Recall output = %s", correctedRecallOutput)
	}
	deleteOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath,
		"delete-receipt", "-id", string(corrected.ReplacementReceiptID),
		"-reason", "system test erasure", "-idempotency-key", "memoryctl-deletion",
	)
	if !bytes.Contains(deleteOutput, []byte(`"deleted": true`)) || !bytes.Contains(deleteOutput, []byte("Session history")) {
		t.Fatalf("memoryctl Delete output = %s", deleteOutput)
	}
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath, "rebuild-fts",
	)
	process.kill(t)
	process = startMemoryd(t, memoryd, dataDir)
	afterRestart := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"recall", "-authorization", authorizationPath, "-query", "corrected",
	)
	if bytes.Contains(afterRestart, []byte("memoryctl corrected fact")) {
		t.Fatalf("deleted replacement reappeared after rebuild/restart = %s", afterRestart)
	}
	oldCredentialBytes, err := os.ReadFile(managementPath)
	if err != nil {
		t.Fatal(err)
	}
	oldCredential := strings.TrimSpace(string(oldCredentialBytes))
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath, "rotate-management",
	)
	newCredentialBytes, err := os.ReadFile(managementPath)
	if err != nil {
		t.Fatal(err)
	}
	newCredential := strings.TrimSpace(string(newCredentialBytes))
	if newCredential == "" || newCredential == oldCredential {
		t.Fatal("memoryctl did not replace the management credential file")
	}
	if _, err := managementclient.NewClientForEndpoint(process.endpoint, oldCredential).Inspect(t.Context()); err == nil {
		t.Fatal("old management credential remained authorized after rotation")
	}
	rotatedInspection := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"-management-credential", managementPath, "inspect",
	)
	if !bytes.Contains(rotatedInspection, []byte(`"protocol_version": "memory.management.v1alpha1"`)) {
		t.Fatalf("rotated management credential could not inspect: %s", rotatedInspection)
	}
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"remember", "-authorization", authorizationPath,
		"-text", "latest stopped upgrade snapshot fact", "-idempotency-key", "upgrade-snapshot-latest",
	)
	process.stop(t)
	preparedOutput := runCommand(t, memoryctl,
		"-management-credential", managementPath,
		"prepare-upgrade", "-data-dir", dataDir,
	)
	if !bytes.Contains(preparedOutput, []byte(`"rollback_available": true`)) {
		t.Fatalf("prepare-upgrade output = %s", preparedOutput)
	}
	process = startMemorydPendingRestore(t, memoryd, dataDir)
	if _, err := localclient.NewClientForEndpoint(process.endpoint).Recall(t.Context(), v1alpha1.CallAuthorization{}, v1alpha1.RecallRequest{}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnavailable) {
		t.Fatalf("pending upgrade Recall error = %v, want unavailable", err)
	}
	process.stop(t)
	runCommand(t, memoryctl,
		"-management-credential", managementPath,
		"restore-rollback", "-data-dir", dataDir,
	)
	process = startMemoryd(t, memoryd, dataDir)
	afterUpgradeRollback := runCommand(t, memoryctl,
		"-data-dir", process.dataDir,
		"recall", "-authorization", authorizationPath, "-query", "latest stopped upgrade snapshot",
	)
	if !bytes.Contains(afterUpgradeRollback, []byte("latest stopped upgrade snapshot fact")) {
		t.Fatalf("upgrade rollback lost the latest acknowledged fact: %s", afterUpgradeRollback)
	}
}

func TestMemorydStewardWorkerAndMemoryctlConfiguration(t *testing.T) {
	root := shortTempDir(t)
	memoryd := buildCommand(t, root, "memoryd")
	memoryctl := buildCommand(t, root, "memoryctl")
	dataDir := filepath.Join(root, "data")
	process := startMemoryd(t, memoryd, dataDir)
	t.Cleanup(func() { process.stop(t) })
	managementPath := filepath.Join(dataDir, appliance.ManagementCredentialFile)
	credentialBytes, err := os.ReadFile(managementPath)
	if err != nil {
		t.Fatal(err)
	}
	admin := managementclient.NewClientForEndpoint(process.endpoint, strings.TrimSpace(string(credentialBytes)))
	bootstrap, err := admin.Bootstrap(t.Context(), appliance.BootstrapRequest{
		Realms:     []appliance.Realm{{ID: "realm-steward"}},
		Identities: []appliance.Identity{{ID: "identity-steward", RealmID: "realm-steward"}},
		Spaces: []appliance.Space{{
			ID: "space-steward", RealmID: "realm-steward", IdentityID: "identity-steward", Class: v1alpha1.SpaceClassPrivate,
		}},
		Views: []appliance.ViewDefinition{{
			ID: "view-steward", RealmID: "realm-steward", ReadSpaceIDs: []v1alpha1.SpaceID{"space-steward"},
			WriteSpaceID: "space-steward", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1,
		}},
		Grants: []appliance.Grant{{
			ID: "grant-steward", PrincipalRef: "principal:steward", ActorRef: "actor-steward", ViewRef: "view-steward",
			AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus},
			AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: time.Now().Add(time.Hour), Version: 1,
		}},
		IssuerPrincipals: []string{"principal:steward"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(root, "profile.json")
	writeJSONFile(t, profilePath, managementv1alpha1.PutStewardProfileRequest{Profile: stewardv1alpha1.ProfileSpec{
		ProfileID: "profile-system", Version: 1,
		SystemPrompt: "organize same-Space evidence", MaxContextRecords: 8,
		MaxInputBytes: 128 << 10, MaxOutputBytes: 16 << 10,
	}})
	profileOutput := runCommand(t, memoryctl,
		"-data-dir", process.dataDir, "-management-credential", managementPath,
		"put-steward-profile", "-file", profilePath,
	)
	if !bytes.Contains(profileOutput, []byte(`"created": true`)) {
		t.Fatalf("put-steward-profile output = %s", profileOutput)
	}
	bindingPath := filepath.Join(root, "binding.json")
	writeJSONFile(t, bindingPath, managementv1alpha1.BindStewardProfileRequest{
		ProfileID: "profile-system", Version: 1, SpaceIDs: []v1alpha1.SpaceID{"space-steward"},
	})
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir, "-management-credential", managementPath,
		"bind-steward-profile", "-file", bindingPath,
	)
	configuration := runCommand(t, memoryctl,
		"-data-dir", process.dataDir, "-management-credential", managementPath, "steward-configuration",
	)
	if !bytes.Contains(configuration, []byte(`"profile_id": "profile-system"`)) ||
		!bytes.Contains(configuration, []byte(`"space_id": "space-steward"`)) {
		t.Fatalf("steward-configuration output = %s", configuration)
	}
	capability, err := localclient.NewIssuerClientForEndpoint(process.endpoint, bootstrap.IssuerCredentials["principal:steward"]).IssueCapability(t.Context(), v1alpha1.CapabilityIssueRequest{
		PrincipalRef: "principal:steward", GrantRef: "grant-steward", ActorRef: "actor-steward", Audience: v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus}, TTLSeconds: 1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: "actor-steward", Audience: v1alpha1.AudiencePrivate}
	client := localclient.NewClientForEndpoint(process.endpoint)
	remembered, err := client.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "the project uses Go", SourceContext: v1alpha1.SourceContext{ActorRef: "actor-steward"},
		IdempotencyKey: "steward-system-job",
	})
	if err != nil {
		t.Fatal(err)
	}
	workerCredentialBytes, err := os.ReadFile(filepath.Join(dataDir, appliance.StewardWorkerCredentialFile))
	if err != nil {
		t.Fatal(err)
	}
	generator := &systemStewardGenerator{}
	runner := stewardworker.Runner{
		Client:         stewardworker.NewClientForEndpoint(process.endpoint, strings.TrimSpace(string(workerCredentialBytes))),
		ModelGenerator: generator,
		Options: stewardworker.RunnerOptions{
			LeaseDuration: 30 * time.Second,
			PollInterval:  20 * time.Millisecond,
		},
	}
	found, err := runner.RunOnce(t.Context())
	if err != nil || !found {
		t.Fatalf("external Steward Worker RunOnce found=%v err=%v", found, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, err := client.GetReceiptStatus(t.Context(), auth, v1alpha1.GetReceiptStatusRequest{ReceiptID: remembered.ReceiptID})
		if err != nil {
			t.Fatal(err)
		}
		if status.State == v1alpha1.ProcessingStateOrganized {
			if status.SemanticGeneration != "profile-system@1" {
				t.Fatalf("organized ReceiptStatus = %+v", status)
			}
			break
		}
		if status.State == v1alpha1.ProcessingStateFailed || time.Now().After(deadline) {
			t.Fatalf("Steward did not organize receipt: %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	recalled, err := client.Recall(t.Context(), auth, v1alpha1.RecallRequest{
		Query: "uses", MinConsistencyToken: remembered.ConsistencyToken,
		Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	var semanticFragment bool
	for _, fragment := range recalled.Fragments {
		if fragment.Text == "The project uses Go." && len(fragment.RecordRefs) == 1 &&
			len(fragment.EvidenceRefs) == 1 && fragment.EvidenceRefs[0] == remembered.ReceiptID {
			semanticFragment = true
		}
	}
	if recalled.Degraded || !semanticFragment {
		t.Fatalf("system semantic Recall = %+v", recalled)
	}
	inspection, err := admin.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Steward.CompletedJobs != 1 || inspection.Steward.ActiveRecords != 1 ||
		!inspection.Steward.ProjectionHealthy {
		t.Fatalf("system Steward diagnostics = %+v", inspection.Steward)
	}
	requestJSON, err := json.Marshal(generator.request)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"space-steward", "actor-steward", "job-", "lease", "provider", "model"} {
		if bytes.Contains(requestJSON, []byte(forbidden)) {
			t.Fatalf("Generator request contains hidden value %q: %s", forbidden, requestJSON)
		}
	}
}

type systemStewardGenerator struct {
	request stewardworker.GenerationRequest
}

func (g *systemStewardGenerator) Generate(_ context.Context, request stewardworker.GenerationRequest) (stewardworker.GenerationResponse, error) {
	g.request = request
	var input struct {
		Receipt struct {
			ReceiptID v1alpha1.ReceiptID `json:"receipt_id"`
		} `json:"receipt"`
	}
	if err := json.Unmarshal([]byte(request.Input), &input); err != nil {
		return stewardworker.GenerationResponse{}, err
	}
	encoded, err := json.Marshal(stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: "The project uses Go.",
		EvidenceRefs: []v1alpha1.ReceiptID{input.Receipt.ReceiptID},
	})
	if err != nil {
		return stewardworker.GenerationResponse{}, err
	}
	return stewardworker.GenerationResponse{Text: string(encoded), ParseMode: stewardworker.ParseModeStrict}, nil
}

func TestMemoryctlEncryptedBackupRestoreAndRollback(t *testing.T) {
	root := shortTempDir(t)
	memoryd := buildCommand(t, root, "memoryd")
	memoryctl := buildCommand(t, root, "memoryctl")
	dataDir := filepath.Join(root, "data")
	process := startMemoryd(t, memoryd, dataDir)
	t.Cleanup(func() { process.stop(t) })
	managementPath := filepath.Join(dataDir, appliance.ManagementCredentialFile)
	credentialBytes, err := os.ReadFile(managementPath)
	if err != nil {
		t.Fatal(err)
	}
	admin := managementclient.NewClientForEndpoint(process.endpoint, strings.TrimSpace(string(credentialBytes)))
	bootstrap, err := admin.Bootstrap(t.Context(), appliance.BootstrapRequest{
		Realms:     []appliance.Realm{{ID: "realm-backup"}},
		Identities: []appliance.Identity{{ID: "identity-backup", RealmID: "realm-backup"}},
		Spaces: []appliance.Space{{
			ID: "space-backup", RealmID: "realm-backup", IdentityID: "identity-backup", Class: v1alpha1.SpaceClassPrivate,
		}},
		Views: []appliance.ViewDefinition{{
			ID: "view-backup", RealmID: "realm-backup", ReadSpaceIDs: []v1alpha1.SpaceID{"space-backup"},
			WriteSpaceID: "space-backup", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1,
		}},
		Grants: []appliance.Grant{{
			ID: "grant-backup", PrincipalRef: "principal:backup", ActorRef: "actor-backup", ViewRef: "view-backup",
			AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall},
			AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: time.Now().Add(time.Hour), Version: 1,
		}},
		IssuerPrincipals: []string{"principal:backup"},
	})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := localclient.NewIssuerClientForEndpoint(process.endpoint, bootstrap.IssuerCredentials["principal:backup"]).IssueCapability(t.Context(), v1alpha1.CapabilityIssueRequest{
		PrincipalRef: "principal:backup", GrantRef: "grant-backup", ActorRef: "actor-backup", Audience: v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall}, TTLSeconds: 1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: "actor-backup", Audience: v1alpha1.AudiencePrivate}
	client := localclient.NewClientForEndpoint(process.endpoint)
	before, err := client.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "encrypted backup private sentinel", IdempotencyKey: "backup-before",
	})
	if err != nil {
		t.Fatal(err)
	}
	exportPath := filepath.Join(root, "memory.ndjson")
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir, "-management-credential", managementPath,
		"export", "-output", exportPath, "-include-deleted",
	)
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exported, []byte(managementv1alpha1.ExportFormat)) || !bytes.Contains(exported, []byte("encrypted backup private sentinel")) {
		t.Fatalf("management export = %s", exported)
	}
	backupPath := filepath.Join(root, "memory.backup")
	keyPath := filepath.Join(root, "memory.backup.key")
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir, "-management-credential", managementPath,
		"backup", "-output", backupPath, "-key-output", keyPath,
	)
	for _, path := range []string{exportPath, backupPath, keyPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
	encrypted, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("encrypted backup private sentinel")) {
		t.Fatal("encrypted backup exposed receipt text")
	}
	after, err := client.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "acknowledged after encrypted backup", IdempotencyKey: "backup-after",
	})
	if err != nil {
		t.Fatal(err)
	}
	process.stop(t)
	restoreOutput := runCommand(t, memoryctl,
		"-management-credential", managementPath,
		"restore", "-data-dir", dataDir, "-backup", backupPath, "-key", keyPath,
	)
	if !bytes.Contains(restoreOutput, []byte(`"rollback_available": true`)) {
		t.Fatalf("restore output = %s", restoreOutput)
	}
	process = startMemorydPendingRestore(t, memoryd, dataDir)
	admin = managementclient.NewClientForEndpoint(process.endpoint, strings.TrimSpace(string(credentialBytes)))
	search, err := admin.SearchReceipts(t.Context(), managementv1alpha1.SearchReceiptsRequest{Query: "private sentinel", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Receipts) != 1 || search.Receipts[0].Text != "encrypted backup private sentinel" {
		t.Fatalf("pending restore search = %+v", search.Receipts)
	}
	search, err = admin.SearchReceipts(t.Context(), managementv1alpha1.SearchReceiptsRequest{Query: "acknowledged", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Receipts) != 0 {
		t.Fatalf("post-backup receipt appeared in pending restore: %+v", search.Receipts)
	}
	client = localclient.NewClientForEndpoint(process.endpoint)
	if _, err := client.Recall(t.Context(), auth, v1alpha1.RecallRequest{
		Query: "private", Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
	}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeUnavailable) {
		t.Fatalf("pending restored Recall error = %v, want unavailable", err)
	}
	process.stop(t)
	runCommand(t, memoryctl,
		"-management-credential", managementPath,
		"restore-rollback", "-data-dir", dataDir,
	)
	process = startMemoryd(t, memoryd, dataDir)
	client = localclient.NewClientForEndpoint(process.endpoint)
	response, err := client.Recall(t.Context(), auth, v1alpha1.RecallRequest{
		Query: "acknowledged", Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFragment(t, response, "acknowledged after encrypted backup")
	if _, err := client.Recall(t.Context(), auth, v1alpha1.RecallRequest{
		Query: "acknowledged", MinConsistencyToken: after.ConsistencyToken,
		Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
	}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeStaleConsistencyToken) {
		t.Fatalf("rollback cursor error = %v, want stale_consistency_token", err)
	}

	process.stop(t)
	runCommand(t, memoryctl,
		"-management-credential", managementPath,
		"restore", "-data-dir", dataDir, "-backup", backupPath, "-key", keyPath,
	)
	process = startMemorydPendingRestore(t, memoryd, dataDir)
	runCommand(t, memoryctl,
		"-data-dir", process.dataDir, "-management-credential", managementPath,
		"restore-commit",
	)
	client = localclient.NewClientForEndpoint(process.endpoint)
	if err := client.Ready(t.Context()); err != nil {
		t.Fatalf("committed restored generation is not ready: %v", err)
	}
	response, err = client.Recall(t.Context(), auth, v1alpha1.RecallRequest{
		Query: "private sentinel", Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFragment(t, response, "encrypted backup private sentinel")
	if _, err := client.Recall(t.Context(), auth, v1alpha1.RecallRequest{
		Query: "private", MinConsistencyToken: before.ConsistencyToken,
		Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
	}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeStaleConsistencyToken) {
		t.Fatalf("committed restored cursor error = %v, want stale_consistency_token", err)
	}
	process.stop(t)
	corruptedPath := filepath.Join(root, "memory-corrupted.backup")
	corrupted := append([]byte(nil), encrypted...)
	corrupted[len(corrupted)/2] ^= 0x40
	if err := os.WriteFile(corruptedPath, corrupted, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), memoryctl,
		"-management-credential", managementPath,
		"restore", "-data-dir", dataDir, "-backup", corruptedPath, "-key", keyPath,
	)
	if output, err := command.CombinedOutput(); err == nil || !bytes.Contains(output, []byte("authenticate backup chunk")) {
		t.Fatalf("corrupt restore = %v, output %s", err, output)
	}
	process = startMemoryd(t, memoryd, dataDir)
	client = localclient.NewClientForEndpoint(process.endpoint)
	response, err = client.Recall(t.Context(), auth, v1alpha1.RecallRequest{
		Query: "private sentinel", Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFragment(t, response, "encrypted backup private sentinel")
}

func TestPackagedSidecarIdentityHandshakeAndIssuerPlane(t *testing.T) {
	root := shortTempDir(t)
	const serviceVersion = "0.2.0-alpha.1-test"
	const buildRevision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	binaryName := "memoryd-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(root, binaryName)
	build := exec.Command("go", "build", "-trimpath",
		"-ldflags", "-X github.com/caelis-labs/memory/internal/buildinfo.ServiceVersion="+serviceVersion+
			" -X github.com/caelis-labs/memory/internal/buildinfo.BuildRevision="+buildRevision,
		"-o", binary, "../../cmd/memoryd")
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build packaged memoryd: %v\n%s", err, output)
	}
	manifest, err := sidecar.CreateManifest(binary, serviceVersion, buildRevision, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := manifest.VerifyNative(root)
	if err != nil {
		t.Fatal(err)
	}
	if verified != binary {
		t.Fatalf("verified executable = %q, want %q", verified, binary)
	}

	dataDir := filepath.Join(root, "data")
	process := startMemoryd(t, verified, dataDir)
	t.Cleanup(func() { process.stop(t) })
	client := localclient.NewClientForEndpoint(process.endpoint)
	compatibility, err := client.CheckCompatibility(t.Context(), localclient.CompatibilityExpectation{
		ServiceVersion: serviceVersion, BuildRevision: buildRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if compatibility.ServiceVersion != manifest.ServiceVersion || compatibility.BuildRevision != manifest.BuildRevision {
		t.Fatalf("handshake identity = %+v, manifest = %+v", compatibility, manifest)
	}
	if _, err := client.CheckCompatibility(t.Context(), localclient.CompatibilityExpectation{
		ServiceVersion: serviceVersion, BuildRevision: "wrong-revision",
	}); !v1alpha1.IsCode(err, v1alpha1.ErrorCodeIncompatible) {
		t.Fatalf("wrong pinned revision error = %v, want incompatible", err)
	}

	credentialBytes, err := os.ReadFile(filepath.Join(dataDir, appliance.ManagementCredentialFile))
	if err != nil {
		t.Fatal(err)
	}
	admin := managementclient.NewClientForEndpoint(process.endpoint, strings.TrimSpace(string(credentialBytes)))
	bootstrap, err := admin.Bootstrap(t.Context(), appliance.BootstrapRequest{
		Realms:     []appliance.Realm{{ID: "realm-m2"}},
		Identities: []appliance.Identity{{ID: "identity-m2", RealmID: "realm-m2"}},
		Spaces: []appliance.Space{{
			ID: "space-m2", RealmID: "realm-m2", IdentityID: "identity-m2", Class: v1alpha1.SpaceClassPrivate,
		}},
		Views: []appliance.ViewDefinition{{
			ID: "view-m2", RealmID: "realm-m2", ReadSpaceIDs: []v1alpha1.SpaceID{"space-m2"},
			WriteSpaceID: "space-m2", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1,
		}},
		Grants: []appliance.Grant{{
			ID: "grant-m2", PrincipalRef: "principal:m2", ActorRef: "actor-m2", ViewRef: "view-m2",
			AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall},
			AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: time.Now().Add(time.Hour), Version: 1,
		}},
		IssuerPrincipals: []string{"principal:m2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	issuer := localclient.NewIssuerClientForEndpoint(process.endpoint, bootstrap.IssuerCredentials["principal:m2"])
	issueRequest := v1alpha1.CapabilityIssueRequest{
		PrincipalRef: "principal:m2", GrantRef: "grant-m2", ActorRef: "actor-m2",
		Audience:   v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall}, TTLSeconds: 600,
	}
	capability, err := issuer.IssueCapability(t.Context(), issueRequest)
	if err != nil {
		t.Fatal(err)
	}
	auth := v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: "actor-m2", Audience: v1alpha1.AudiencePrivate}
	remembered, err := client.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "M2 uses a pinned sidecar", IdempotencyKey: "m2-pinned-sidecar",
	})
	if err != nil || !remembered.Accepted {
		t.Fatalf("Remember() = %+v, %v", remembered, err)
	}
	renewed, err := issuer.IssueCapability(t.Context(), issueRequest)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Token == capability.Token {
		t.Fatal("capability renewal reused the prior bearer")
	}
	retryAuth := v1alpha1.CallAuthorization{Capability: renewed.Token, ActorRef: "actor-m2", Audience: v1alpha1.AudiencePrivate}
	retried, err := client.Remember(t.Context(), retryAuth, v1alpha1.RememberRequest{
		Text: "M2 uses a pinned sidecar", IdempotencyKey: "m2-pinned-sidecar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !retried.DeduplicatedRetry || retried.ReceiptID != remembered.ReceiptID {
		t.Fatalf("renewed capability changed effect identity: first=%+v retry=%+v", remembered, retried)
	}
}

func newDurableProcessFixture(t *testing.T) conformance.DurableFixture {
	t.Helper()
	root := shortTempDir(t)
	binary := buildMemoryd(t, root)
	dataDir := filepath.Join(root, "data")
	process := startMemoryd(t, binary, dataDir)
	t.Cleanup(func() { process.stop(t) })
	credentialBytes, err := os.ReadFile(filepath.Join(dataDir, appliance.ManagementCredentialFile))
	if err != nil {
		t.Fatal(err)
	}
	admin := managementclient.NewClientForEndpoint(process.endpoint, strings.TrimSpace(string(credentialBytes)))
	now := time.Now().UTC()
	bootstrap, err := admin.Bootstrap(t.Context(), appliance.BootstrapRequest{
		Realms:     []appliance.Realm{{ID: "realm-system"}},
		Identities: []appliance.Identity{{ID: "identity-system", RealmID: "realm-system"}},
		Spaces: []appliance.Space{{
			ID: "space-system", RealmID: "realm-system", IdentityID: "identity-system", Class: v1alpha1.SpaceClassPrivate,
		}},
		Views: []appliance.ViewDefinition{{
			ID: "view-system", RealmID: "realm-system", ReadSpaceIDs: []v1alpha1.SpaceID{"space-system"},
			WriteSpaceID: "space-system", MaxDisclosureClass: v1alpha1.SpaceClassPrivate, Version: 1,
		}},
		Grants: []appliance.Grant{{
			ID: "grant-system", PrincipalRef: "principal:system", ActorRef: "actor-system", ViewRef: "view-system",
			AllowedOperations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus},
			AllowedAudiences:  []v1alpha1.Audience{v1alpha1.AudiencePrivate}, ExpiresAt: now.Add(time.Hour), Version: 1,
		}},
		IssuerPrincipals: []string{"principal:system"},
	})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := localclient.NewIssuerClientForEndpoint(process.endpoint, bootstrap.IssuerCredentials["principal:system"]).IssueCapability(t.Context(), v1alpha1.CapabilityIssueRequest{
		PrincipalRef: "principal:system", GrantRef: "grant-system", ActorRef: "actor-system", Audience: v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus},
		TTLSeconds: 1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: "actor-system", Audience: v1alpha1.AudiencePrivate}
	return conformance.DurableFixture{
		Service:       localclient.NewClientForEndpoint(process.endpoint),
		Authorization: auth,
		CrashAndRestart: func(t *testing.T) v1alpha1.DataPlane {
			process.kill(t)
			process = startMemoryd(t, binary, dataDir)
			return localclient.NewClientForEndpoint(process.endpoint)
		},
	}
}

type memorydProcess struct {
	command  *exec.Cmd
	dataDir  string
	endpoint v1alpha1.LocalEndpoint
	stderr   bytes.Buffer
	waited   bool
}

func startMemoryd(t *testing.T, binary, dataDir string) *memorydProcess {
	return startMemorydWithState(t, binary, dataDir, true)
}

func startMemorydPendingRestore(t *testing.T, binary, dataDir string) *memorydProcess {
	return startMemorydWithState(t, binary, dataDir, false)
}

func startMemorydWithState(t *testing.T, binary, dataDir string, requireReady bool) *memorydProcess {
	return startMemorydCommand(t, binary, dataDir, requireReady)
}

func startMemorydCommand(t *testing.T, binary, dataDir string, requireReady bool, extraArguments ...string) *memorydProcess {
	t.Helper()
	arguments := []string{"-data-dir", dataDir}
	arguments = append(arguments, extraArguments...)
	command := exec.Command(binary, arguments...)
	process := &memorydProcess{command: command, dataDir: dataDir, endpoint: v1alpha1.DefaultLocalEndpoint(dataDir)}
	command.Stderr = &process.stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	client := localclient.NewClientForEndpoint(process.endpoint)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		healthErr := client.Health(ctx)
		readyErr := client.Ready(ctx)
		cancel()
		if healthErr == nil && (readyErr == nil) == requireReady {
			return process
		}
		time.Sleep(20 * time.Millisecond)
	}
	process.kill(t)
	t.Fatalf("memoryd did not become ready: %s", process.stderr.String())
	return nil
}

func (p *memorydProcess) kill(t *testing.T) {
	t.Helper()
	if p == nil || p.waited {
		return
	}
	if err := p.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("kill memoryd: %v", err)
	}
	if err := p.command.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("wait for killed memoryd: %v", err)
		}
	}
	p.waited = true
}

func (p *memorydProcess) stop(t *testing.T) {
	t.Helper()
	if p == nil || p.waited {
		return
	}
	forced := false
	if err := p.command.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		if killErr := p.command.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Errorf("stop memoryd: interrupt=%v kill=%v", err, killErr)
			return
		}
		forced = true
	}
	done := make(chan error, 1)
	go func() { done <- p.command.Wait() }()
	select {
	case err := <-done:
		p.waited = true
		if err != nil && !forced {
			t.Errorf("memoryd graceful exit: %v\n%s", err, p.stderr.String())
		}
		client := localclient.NewClientForEndpoint(p.endpoint)
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		if healthErr := client.Health(ctx); healthErr == nil {
			t.Errorf("memoryd local endpoint remained live after exit")
		}
	case <-time.After(10 * time.Second):
		_ = p.command.Process.Kill()
		p.waited = true
		t.Errorf("memoryd did not stop gracefully: %s", p.stderr.String())
	}
}

func TestMemorydOwnerLockFailureIsExplicit(t *testing.T) {
	root := shortTempDir(t)
	binary := buildMemoryd(t, root)
	dataDir := filepath.Join(root, "data")
	first := startMemoryd(t, binary, dataDir)
	t.Cleanup(func() { first.stop(t) })
	second := exec.Command(binary, "-data-dir", dataDir)
	output, err := second.CombinedOutput()
	if err == nil || !strings.Contains(string(output), appliance.ErrOwnerLocked.Error()) {
		t.Fatalf("second memoryd = %v, output %q", err, output)
	}
}

func buildMemoryd(t *testing.T, root string) string {
	t.Helper()
	return buildCommand(t, root, "memoryd")
}

func buildCommand(t *testing.T, root, name string) string {
	t.Helper()
	packageName := name
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(root, name)
	build := exec.Command("go", "build", "-o", binary, "../../cmd/"+packageName)
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build memoryd: %v\n%s", err, output)
	}
	return binary
}

func runCommand(t *testing.T, binary string, arguments ...string) []byte {
	t.Helper()
	command := exec.CommandContext(t.Context(), binary, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", binary, arguments, err, output)
	}
	return output
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, value); err != nil {
		t.Fatal(err)
	}
}

func assertFragment(t *testing.T, response v1alpha1.RecallResponse, text string) {
	t.Helper()
	for _, fragment := range response.Fragments {
		if fragment.Text == text {
			return
		}
	}
	t.Fatalf("Recall fragments %+v do not contain %q", response.Fragments, text)
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "memoryd-systemtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove system-test directory: %v", err)
		}
	})
	return root
}
