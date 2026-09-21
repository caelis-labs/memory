package appliance_test

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	management "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	steward "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
	"github.com/caelis-labs/memory/sdk/go/memory/stewardworker"
)

// This file is also compiled in the external-module consumer gate. It must
// use public APIs only, including the real Runner and ModelGenerator boundary.
//
//go:embed testdata/upgrades/v*/memory.db testdata/upgrades/v*/*.json testdata/upgrades/v*/*.txt
var releasedUpgradeFixtures embed.FS

type upgradeReceipt struct {
	Request  memory.RememberRequest  `json:"request"`
	Response memory.RememberResponse `json:"response"`
}

type upgradeManifest struct {
	SourceTag        string                        `json:"source_tag"`
	SourceCommit     string                        `json:"source_commit"`
	SchemaVersion    int                           `json:"schema_version"`
	FilesSHA256      map[string]string             `json:"files_sha256"`
	Profile          steward.ProfileSpec           `json:"profile"`
	CustomProfile    steward.ProfileSpec           `json:"custom_profile"`
	Labels           memory.LabelSet               `json:"labels"`
	IssuerCredential string                        `json:"test_only_issuer_credential"`
	Receipts         []upgradeReceipt              `json:"receipts"`
	LeasedWork       struct{ Lease steward.Lease } `json:"leased_work"`
}

func copyReleasedUpgradeFixture(t *testing.T, release, commit string, schema int) (string, upgradeManifest) {
	t.Helper()
	prefix := "testdata/upgrades/" + release + "/"
	raw, err := releasedUpgradeFixtures.ReadFile(prefix + "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest upgradeManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SourceTag != release || manifest.SourceCommit != commit || manifest.SchemaVersion != schema || manifest.Profile.Version != 1 || len(manifest.Receipts) != 2 || len(manifest.Labels) == 0 {
		t.Fatal("released fixture provenance or coverage changed")
	}
	dir := t.TempDir()
	for _, name := range []string{"memory.db", "management.token.txt", "steward-worker.token.txt"} {
		raw, err := releasedUpgradeFixtures.ReadFile(prefix + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != manifest.FilesSHA256[name] {
			t.Fatalf("fixture digest mismatch: %s", name)
		}
		if err := os.WriteFile(filepath.Join(dir, strings.TrimSuffix(name, ".txt")), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, manifest
}

func upgradeAuthorization(t *testing.T, runtime *appliance.Runtime, manifest upgradeManifest, labels memory.LabelSet) memory.CallAuthorization {
	t.Helper()
	issued, err := runtime.IssueCapability(t.Context(), manifest.IssuerCredential, memory.CapabilityIssueRequest{
		PrincipalRef: "principal:upgrade", GrantRef: "grant:upgrade", ActorRef: "actor:upgrade", Audience: memory.AudiencePrivate,
		Operations: []memory.Operation{memory.OperationRemember, memory.OperationRecall, memory.OperationReceiptStatus}, Labels: labels, TTLSeconds: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	return memory.CallAuthorization{Capability: issued.Token, ActorRef: "actor:upgrade", Audience: memory.AudiencePrivate}
}

type upgradeWorker struct {
	stewardworker.Worker
	profiles map[memory.ReceiptID]steward.ProfileSpec
	attempts map[memory.ReceiptID]int
}

func (w *upgradeWorker) Claim(ctx context.Context, duration time.Duration) (steward.ClaimResponse, error) {
	claim, err := w.Worker.Claim(ctx, duration)
	if err == nil && claim.Found && claim.Work != nil {
		w.profiles[claim.Work.Receipt.ReceiptID] = claim.Work.Profile
		w.attempts[claim.Work.Receipt.ReceiptID] = claim.Attempt
	}
	return claim, err
}

type upgradeModel struct{}

func (upgradeModel) Generate(_ context.Context, request stewardworker.GenerationRequest) (stewardworker.GenerationResponse, error) {
	var input struct {
		Receipt steward.ReceiptInput `json:"receipt"`
	}
	if err := json.Unmarshal([]byte(request.Input), &input); err != nil {
		return stewardworker.GenerationResponse{}, err
	}
	raw, err := json.Marshal(steward.Proposal{Operation: steward.OperationAdd, Kind: "fact", Text: input.Receipt.Text + " organized", EvidenceRefs: []memory.ReceiptID{input.Receipt.ReceiptID}})
	return stewardworker.GenerationResponse{Text: string(raw), ParseMode: stewardworker.ParseModeStrict}, err
}

func TestReleasedStewardUpgrade(t *testing.T) {
	for _, input := range []struct {
		release, commit string
		schema          int
	}{
		{"v0.5.2", "51693ff135be8c4149c15117980290aaad6d90da", 1},
		{"v0.6.0", "f17b0293597dcdf9fad4fcba9ea19e20d1d91674", 2},
	} {
		t.Run(input.release, func(t *testing.T) {
			dir, manifest := copyReleasedUpgradeFixture(t, input.release, input.commit, input.schema)
			runtime, err := appliance.Open(t.Context(), appliance.Options{DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = runtime.Close() }()
			auth := upgradeAuthorization(t, runtime, manifest, manifest.Labels)
			other := upgradeAuthorization(t, runtime, manifest, memory.LabelSet{"workspace:other"})
			configuration, err := runtime.Management().GetStewardConfiguration(t.Context())
			if err != nil || len(configuration.Profiles) != 2 || len(configuration.Bindings) != 1 || configuration.Bindings[0].ProfileVersion != 1 || configuration.Bindings[0].SpaceID != "space:upgrade" {
				t.Fatalf("preserved configuration: %+v %v", configuration, err)
			}
			for _, expected := range []steward.ProfileSpec{manifest.Profile, manifest.CustomProfile} {
				put, err := runtime.Management().PutStewardProfile(t.Context(), management.PutStewardProfileRequest{Profile: expected})
				if err != nil || put.Created {
					t.Fatalf("old profile changed: %+v %v", put, err)
				}
			}
			current := stewardworker.BuiltInProfile()
			if current.Version <= manifest.Profile.Version {
				t.Fatal("changed built-in policy must use a new version")
			}
			for i := 0; i < 2; i++ {
				put, err := runtime.Management().PutStewardProfile(t.Context(), management.PutStewardProfileRequest{Profile: current})
				if err != nil || put.Created != (i == 0) {
					t.Fatalf("register current profile: %+v %v", put, err)
				}
			}
			collision := current
			collision.SystemPrompt += " different"
			if _, err := runtime.Management().PutStewardProfile(t.Context(), management.PutStewardProfileRequest{Profile: collision}); !memory.IsCode(err, memory.ErrorCodeConflict) {
				t.Fatalf("immutable conflict=%v", err)
			}
			if _, err := runtime.Management().BindStewardProfile(t.Context(), management.BindStewardProfileRequest{ProfileID: current.ProfileID, Version: current.Version, SpaceIDs: []memory.SpaceID{"space:upgrade"}}); err != nil {
				t.Fatal(err)
			}
			freshRequest := memory.RememberRequest{Text: "new release evidence", IdempotencyKey: "new release evidence"}
			fresh, err := runtime.DataPlane().Remember(t.Context(), auth, freshRequest)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			runtime, err = appliance.Open(t.Context(), appliance.Options{DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			put, err := runtime.Management().PutStewardProfile(t.Context(), management.PutStewardProfileRequest{Profile: current})
			if err != nil || put.Created {
				t.Fatalf("restart registration: %+v %v", put, err)
			}
			configuration, err = runtime.Management().GetStewardConfiguration(t.Context())
			if err != nil || len(configuration.Profiles) != 3 || len(configuration.Bindings) != 1 || configuration.Bindings[0].ProfileVersion != current.Version {
				t.Fatalf("restart configuration: %+v %v", configuration, err)
			}
			if _, err := runtime.StewardWorker().Apply(t.Context(), steward.ApplyRequest{Lease: manifest.LeasedWork.Lease, Proposal: steward.Proposal{Operation: steward.OperationIgnore}}); err == nil {
				t.Fatal("pre-upgrade lease retained authority")
			}
			worker := &upgradeWorker{Worker: runtime.StewardWorker(), profiles: map[memory.ReceiptID]steward.ProfileSpec{}, attempts: map[memory.ReceiptID]int{}}
			runner := stewardworker.Runner{Client: worker, ModelGenerator: upgradeModel{}, Options: stewardworker.RunnerOptions{LeaseDuration: time.Minute, PollInterval: 10 * time.Millisecond}}
			for i := 0; i < 3; i++ {
				if found, err := runner.RunOnce(t.Context()); err != nil || !found {
					t.Fatalf("run %d: %v %v", i, found, err)
				}
			}
			if found, err := runner.RunOnce(t.Context()); err != nil || found {
				t.Fatalf("queue not drained: %v %v", found, err)
			}
			if worker.profiles[fresh.ReceiptID] != current {
				t.Fatal("new work did not capture current profile")
			}
			if worker.attempts[manifest.Receipts[0].Response.ReceiptID] != 2 {
				t.Fatal("leased work was not reclaimed with a new attempt")
			}
			for _, receipt := range manifest.Receipts {
				if worker.profiles[receipt.Response.ReceiptID] != manifest.Profile {
					t.Fatal("old job profile snapshot changed")
				}
			}
			for _, receipt := range append(manifest.Receipts, upgradeReceipt{freshRequest, fresh}) {
				status, err := runtime.DataPlane().GetReceiptStatus(t.Context(), auth, memory.GetReceiptStatusRequest{ReceiptID: receipt.Response.ReceiptID})
				if err != nil || status.State != memory.ProcessingStateOrganized {
					t.Fatalf("receipt status=%+v %v", status, err)
				}
				retry, err := runtime.DataPlane().Remember(t.Context(), auth, receipt.Request)
				if err != nil || !retry.DeduplicatedRetry || retry.ReceiptID != receipt.Response.ReceiptID || retry.ConsistencyToken != receipt.Response.ConsistencyToken {
					t.Fatalf("receipt identity changed: %+v %v", retry, err)
				}
				trace, err := runtime.Management().TraceReceipt(t.Context(), management.TraceReceiptRequest{ReceiptID: receipt.Response.ReceiptID})
				if err != nil || trace.Receipt == nil || trace.Receipt.Text != receipt.Request.Text {
					t.Fatalf("receipt evidence changed: %+v %v", trace, err)
				}
				if _, err := runtime.DataPlane().GetReceiptStatus(t.Context(), other, memory.GetReceiptStatusRequest{ReceiptID: receipt.Response.ReceiptID}); !memory.IsCode(err, memory.ErrorCodeNotFound) {
					t.Fatalf("label authority widened: %v", err)
				}
			}
			records, err := runtime.Management().ListRecords(t.Context(), management.ListRecordsRequest{SpaceID: "space:upgrade", Limit: 10})
			if err != nil || len(records.Records) != 3 {
				t.Fatalf("organized records=%+v %v", records, err)
			}
		})
	}
}
