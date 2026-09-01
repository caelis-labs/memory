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
	"syscall"
	"testing"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/conformance"
	"github.com/caelis-labs/memory/internal/appliance"
	"github.com/caelis-labs/memory/internal/localtransport"
	localclient "github.com/caelis-labs/memory/sdk/go/memory/local"
	"github.com/caelis-labs/memory/sdk/go/memory/sidecar"
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
	admin := localtransport.NewAdminClient(process.socket, strings.TrimSpace(string(credentialBytes)))
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
		capability, err := localclient.NewIssuerClient(process.socket, bootstrap.IssuerCredentials[principal]).IssueCapability(t.Context(), v1alpha1.CapabilityIssueRequest{
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
	client := localclient.NewClient(process.socket)
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
	client = localclient.NewClient(process.socket)
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
		"-socket", process.socket,
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
		"-socket", process.socket,
		"-issuer-credential", issuerPath,
		"issue", "-file", issuePath, "-authorization-output", authorizationPath,
	)
	rememberOutput := runCommand(t, memoryctl,
		"-socket", process.socket,
		"remember", "-authorization", authorizationPath,
		"-text", "memoryctl standalone fact", "-idempotency-key", "memoryctl-effect",
	)
	if !bytes.Contains(rememberOutput, []byte(`"accepted": true`)) {
		t.Fatalf("memoryctl Remember output = %s", rememberOutput)
	}
	recallOutput := runCommand(t, memoryctl,
		"-socket", process.socket,
		"recall", "-authorization", authorizationPath, "-query", "standalone fact",
	)
	if !bytes.Contains(recallOutput, []byte("memoryctl standalone fact")) {
		t.Fatalf("memoryctl Recall output = %s", recallOutput)
	}
	inspectOutput := runCommand(t, memoryctl,
		"-socket", process.socket,
		"-management-credential", managementPath, "inspect",
	)
	if !bytes.Contains(inspectOutput, []byte(`"receipts": 1`)) {
		t.Fatalf("memoryctl Inspect output = %s", inspectOutput)
	}
}

func TestPackagedSidecarIdentityHandshakeAndIssuerPlane(t *testing.T) {
	root := shortTempDir(t)
	const serviceVersion = "0.2.0-alpha.1-test"
	const buildRevision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	binary := filepath.Join(root, "memoryd-"+runtime.GOOS+"-"+runtime.GOARCH)
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
	client := localclient.NewClient(process.socket)
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
	admin := localtransport.NewAdminClient(process.socket, strings.TrimSpace(string(credentialBytes)))
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
	issuer := localclient.NewIssuerClient(process.socket, bootstrap.IssuerCredentials["principal:m2"])
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
	admin := localtransport.NewAdminClient(process.socket, strings.TrimSpace(string(credentialBytes)))
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
	capability, err := localclient.NewIssuerClient(process.socket, bootstrap.IssuerCredentials["principal:system"]).IssueCapability(t.Context(), v1alpha1.CapabilityIssueRequest{
		PrincipalRef: "principal:system", GrantRef: "grant-system", ActorRef: "actor-system", Audience: v1alpha1.AudiencePrivate,
		Operations: []v1alpha1.Operation{v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus},
		TTLSeconds: 1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: "actor-system", Audience: v1alpha1.AudiencePrivate}
	return conformance.DurableFixture{
		Service:       localclient.NewClient(process.socket),
		Authorization: auth,
		CrashAndRestart: func(t *testing.T) v1alpha1.DataPlane {
			process.kill(t)
			process = startMemoryd(t, binary, dataDir)
			return localclient.NewClient(process.socket)
		},
	}
}

type memorydProcess struct {
	command *exec.Cmd
	socket  string
	stderr  bytes.Buffer
	waited  bool
}

func startMemoryd(t *testing.T, binary, dataDir string) *memorydProcess {
	t.Helper()
	command := exec.Command(binary, "-data-dir", dataDir)
	process := &memorydProcess{command: command, socket: filepath.Join(dataDir, appliance.SocketFilename)}
	command.Stderr = &process.stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	client := localclient.NewClient(process.socket)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		healthErr := client.Health(ctx)
		readyErr := client.Ready(ctx)
		cancel()
		if healthErr == nil && readyErr == nil {
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
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("stop memoryd: %v", err)
		return
	}
	done := make(chan error, 1)
	go func() { done <- p.command.Wait() }()
	select {
	case err := <-done:
		p.waited = true
		if err != nil {
			t.Errorf("memoryd graceful exit: %v\n%s", err, p.stderr.String())
		}
		if _, statErr := os.Lstat(p.socket); !os.IsNotExist(statErr) {
			t.Errorf("memoryd graceful exit left socket behind: %v", statErr)
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
	binary := filepath.Join(root, name)
	build := exec.Command("go", "build", "-o", binary, "../../cmd/"+name)
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
	root, err := os.MkdirTemp("/tmp", "memoryd-systemtest-")
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
