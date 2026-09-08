package appliance_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
	memorysdk "github.com/caelis-labs/memory/sdk/go/memory"
)

func TestOwnedOfflineRestoreLifecycleKeepsCredentialInsideAppliance(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	runtime, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot bytes.Buffer
	if err := runtime.Backup(ctx, &snapshot); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	prepared, err := appliance.PrepareUpgradeOwned(ctx, dataDir)
	if err != nil {
		t.Fatalf("PrepareUpgradeOwned() error = %v", err)
	}
	if !prepared.RollbackAvailable {
		t.Fatal("PrepareUpgradeOwned() did not retain rollback")
	}
	if _, err := appliance.RollbackRestoreOwned(ctx, dataDir, nil); err != nil {
		t.Fatalf("RollbackRestoreOwned() error = %v", err)
	}

	if _, err := appliance.RestoreOwned(ctx, appliance.OfflineRestoreOptions{
		DataDir: dataDir, Snapshot: bytes.NewReader(snapshot.Bytes()),
	}); err != nil {
		t.Fatalf("RestoreOwned() error = %v", err)
	}
	if err := appliance.CommitRestoreOwned(dataDir); err != nil {
		t.Fatalf("CommitRestoreOwned() error = %v", err)
	}
	if reopened, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir}); err != nil {
		t.Fatalf("Open() after owned restore error = %v", err)
	} else if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedOfflineRestoreRoundTripPreservesReceiptAndRejectsLiveOwner(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	runtime, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}

	const (
		principal = "principal:backup-roundtrip"
		actor     = "actor:backup-roundtrip"
	)
	bootstrap, err := runtime.Management().Bootstrap(ctx, managementv1alpha1.BootstrapRequest{
		Realms:     []managementv1alpha1.Realm{{ID: "realm:backup-roundtrip"}},
		Identities: []managementv1alpha1.Identity{{ID: "identity:backup-roundtrip", RealmID: "realm:backup-roundtrip"}},
		Spaces: []managementv1alpha1.Space{{
			ID: "space:backup-roundtrip", RealmID: "realm:backup-roundtrip", IdentityID: "identity:backup-roundtrip", Class: memoryv1alpha1.SpaceClassPrivate,
		}},
		Views: []managementv1alpha1.ViewDefinition{{
			ID: "view:backup-roundtrip", RealmID: "realm:backup-roundtrip", ReadSpaceIDs: []memoryv1alpha1.SpaceID{"space:backup-roundtrip"},
			WriteSpaceID: "space:backup-roundtrip", MaxDisclosureClass: memoryv1alpha1.SpaceClassPrivate, Version: 1,
		}},
		Grants: []managementv1alpha1.Grant{{
			ID: "grant:backup-roundtrip", PrincipalRef: principal, ActorRef: actor, ViewRef: "view:backup-roundtrip",
			AllowedOperations: []memoryv1alpha1.Operation{
				memoryv1alpha1.OperationRemember,
				memoryv1alpha1.OperationRecall,
				memoryv1alpha1.OperationReceiptStatus,
			},
			AllowedAudiences: []memoryv1alpha1.Audience{memoryv1alpha1.AudiencePrivate},
			ExpiresAt:        time.Now().Add(time.Hour),
			Version:          1,
		}},
		IssuerPrincipals: []string{principal},
	})
	if err != nil {
		t.Fatal(err)
	}
	issuerCredential := bootstrap.IssuerCredentials[principal]
	issued, err := runtime.IssueCapability(ctx, issuerCredential, memoryv1alpha1.CapabilityIssueRequest{
		PrincipalRef: principal, GrantRef: "grant:backup-roundtrip", ActorRef: actor,
		Audience: memoryv1alpha1.AudiencePrivate,
		Operations: []memoryv1alpha1.Operation{
			memoryv1alpha1.OperationRemember,
			memoryv1alpha1.OperationRecall,
			memoryv1alpha1.OperationReceiptStatus,
		},
		TTLSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := memorysdk.NewClient(
		runtime.DataPlane(),
		memorysdk.StaticCapabilitySource{AuthorizationValue: memoryv1alpha1.CallAuthorization{
			Capability: issued.Token, ActorRef: actor, Audience: memoryv1alpha1.AudiencePrivate,
		}},
		memoryv1alpha1.SourceContext{ActorRef: actor, SessionRef: "session:backup-roundtrip", SourceType: "test"},
		memoryv1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 16 << 10, DeadlineMS: 1_000},
	)
	remembered, err := client.Remember(ctx, "receipt survives an owned backup restore", "backup-roundtrip:remember", nil)
	if err != nil || !remembered.Accepted || remembered.ReceiptID == "" {
		t.Fatalf("Remember() = %#v, %v", remembered, err)
	}

	var snapshot bytes.Buffer
	if err := runtime.Backup(ctx, &snapshot); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	backup := append([]byte(nil), snapshot.Bytes()...)
	if _, err := appliance.RestoreOwned(ctx, appliance.OfflineRestoreOptions{
		DataDir: dataDir, Snapshot: bytes.NewReader(backup),
	}); err == nil {
		t.Fatal("RestoreOwned() succeeded while the live runtime still owned the data directory")
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := appliance.RestoreOwned(ctx, appliance.OfflineRestoreOptions{
		DataDir: dataDir, Snapshot: bytes.NewReader(backup),
	}); err != nil {
		t.Fatalf("RestoreOwned() error = %v", err)
	}
	if err := appliance.CommitRestoreOwned(dataDir); err != nil {
		t.Fatalf("CommitRestoreOwned() error = %v", err)
	}
	reopened, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Open() after owned restore error = %v", err)
	}
	defer reopened.Close()

	reissued, err := reopened.IssueCapability(ctx, issuerCredential, memoryv1alpha1.CapabilityIssueRequest{
		PrincipalRef: principal, GrantRef: "grant:backup-roundtrip", ActorRef: actor,
		Audience: memoryv1alpha1.AudiencePrivate,
		Operations: []memoryv1alpha1.Operation{
			memoryv1alpha1.OperationRemember,
			memoryv1alpha1.OperationRecall,
			memoryv1alpha1.OperationReceiptStatus,
		},
		TTLSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	restoredClient := memorysdk.NewClient(
		reopened.DataPlane(),
		memorysdk.StaticCapabilitySource{AuthorizationValue: memoryv1alpha1.CallAuthorization{
			Capability: reissued.Token, ActorRef: actor, Audience: memoryv1alpha1.AudiencePrivate,
		}},
		memoryv1alpha1.SourceContext{ActorRef: actor, SessionRef: "session:backup-roundtrip", SourceType: "test"},
		memoryv1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 16 << 10, DeadlineMS: 1_000},
	)
	recalled, err := restoredClient.Recall(ctx, "owned backup restore", "")
	if err != nil {
		t.Fatalf("Recall() after owned restore error = %v", err)
	}
	if len(recalled.Fragments) == 0 || recalled.Fragments[0].Text != "receipt survives an owned backup restore" {
		t.Fatalf("Recall() after owned restore = %#v", recalled)
	}
	status, err := restoredClient.GetReceiptStatus(ctx, remembered.ReceiptID)
	if err != nil {
		t.Fatalf("GetReceiptStatus() after owned restore error = %v", err)
	}
	if status.ReceiptID != remembered.ReceiptID || status.State != memoryv1alpha1.ProcessingStateAccepted {
		t.Fatalf("GetReceiptStatus() after owned restore = %#v", status)
	}
}
