package appliance_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	memoryv1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
	core "github.com/caelis-labs/memory/internal/appliance"
	memorysdk "github.com/caelis-labs/memory/sdk/go/memory"
)

func TestEmbeddedRuntimeRememberRecall(t *testing.T) {
	ctx := context.Background()
	runtime, err := appliance.Open(ctx, appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	const (
		principal = "principal:test"
		actor     = "actor:test"
	)
	bootstrap, err := runtime.Management().Bootstrap(ctx, managementv1alpha1.BootstrapRequest{
		Realms:     []managementv1alpha1.Realm{{ID: "realm:test"}},
		Identities: []managementv1alpha1.Identity{{ID: "identity:test", RealmID: "realm:test"}},
		Spaces: []managementv1alpha1.Space{{
			ID: "space:test", RealmID: "realm:test", IdentityID: "identity:test", Class: memoryv1alpha1.SpaceClassPrivate,
		}},
		Views: []managementv1alpha1.ViewDefinition{{
			ID: "view:test", RealmID: "realm:test", ReadSpaceIDs: []memoryv1alpha1.SpaceID{"space:test"},
			WriteSpaceID: "space:test", MaxDisclosureClass: memoryv1alpha1.SpaceClassPrivate, Version: 1,
		}},
		Grants: []managementv1alpha1.Grant{{
			ID: "grant:test", PrincipalRef: principal, ActorRef: actor, ViewRef: "view:test",
			AllowedOperations: []memoryv1alpha1.Operation{memoryv1alpha1.OperationRemember, memoryv1alpha1.OperationRecall},
			AllowedAudiences:  []memoryv1alpha1.Audience{memoryv1alpha1.AudiencePrivate},
			ExpiresAt:         time.Now().Add(time.Hour), Version: 1,
		}},
		IssuerPrincipals: []string{principal},
	})
	if err != nil {
		t.Fatal(err)
	}
	authority := memoryv1alpha1.CapabilityAuthorityRequest{
		PrincipalRef: principal, GrantRef: "grant:test", ViewRef: "view:test", ActorRef: actor,
		Audience:   memoryv1alpha1.AudiencePrivate,
		Operations: []memoryv1alpha1.Operation{memoryv1alpha1.OperationRemember, memoryv1alpha1.OperationRecall},
	}
	beforeValidation, err := runtime.Management().Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateCapabilityAuthority(ctx, bootstrap.IssuerCredentials[principal], authority); err != nil {
		t.Fatalf("ValidateCapabilityAuthority() = %v", err)
	}
	wrongView := authority
	wrongView.ViewRef = "view:other"
	if err := runtime.ValidateCapabilityAuthority(ctx, bootstrap.IssuerCredentials[principal], wrongView); !memoryv1alpha1.IsCode(err, memoryv1alpha1.ErrorCodeUnauthorized) {
		t.Fatalf("ValidateCapabilityAuthority(wrong View) error = %v, want unauthorized", err)
	}
	afterValidation, err := runtime.Management().Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterValidation.Capabilities.Stored != beforeValidation.Capabilities.Stored {
		t.Fatalf("authority validation stored capabilities: before=%+v after=%+v", beforeValidation.Capabilities, afterValidation.Capabilities)
	}

	issued, err := runtime.IssueCapability(ctx, bootstrap.IssuerCredentials[principal], memoryv1alpha1.CapabilityIssueRequest{
		PrincipalRef: principal, GrantRef: "grant:test", ActorRef: actor,
		Audience:   memoryv1alpha1.AudiencePrivate,
		Operations: []memoryv1alpha1.Operation{memoryv1alpha1.OperationRemember, memoryv1alpha1.OperationRecall},
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
		memoryv1alpha1.SourceContext{ActorRef: actor, SessionRef: "session:test", SourceType: "test"},
		memoryv1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 16 << 10, DeadlineMS: 1_000},
	)
	remembered, err := client.Remember(ctx, "the embedded runtime remembers", "remember:test", nil)
	if err != nil || !remembered.Accepted {
		t.Fatalf("Remember() = %#v, %v", remembered, err)
	}
	recalled, err := client.Recall(ctx, "embedded remembers", remembered.ConsistencyToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled.Fragments) != 1 || recalled.Fragments[0].Text != "the embedded runtime remembers" {
		t.Fatalf("Recall() = %#v", recalled)
	}
}

func TestEmbeddedRuntimeRejectsPendingRestoreGeneration(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	runtime, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	credential, err := os.ReadFile(filepath.Join(dataDir, core.ManagementCredentialFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.PrepareUpgrade(ctx, dataDir, strings.TrimSpace(string(credential))); err != nil {
		t.Fatal(err)
	}
	if runtime, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir}); err == nil {
		_ = runtime.Close()
		t.Fatal("Open() accepted a generation that still permits rollback")
	}
}

func TestEmbeddedRuntimeCloseIsSafeWithConcurrentAccess(t *testing.T) {
	runtime, err := appliance.Open(t.Context(), appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range 100 {
				_ = runtime.DataPlane()
				_ = runtime.Management()
				_ = runtime.StewardWorker()
				_ = runtime.ValidateCapabilityAuthority(t.Context(), "invalid", memoryv1alpha1.CapabilityAuthorityRequest{})
				_, _ = runtime.IssueCapability(t.Context(), "invalid", memoryv1alpha1.CapabilityIssueRequest{})
			}
		}()
	}
	close(start)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	if runtime.DataPlane() != nil || runtime.Management() != nil || runtime.StewardWorker() != nil {
		t.Fatal("closed runtime still exposed service planes")
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
}
