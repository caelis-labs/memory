// facts_consumer_gate runs an external-module smoke test for the public facts
// and owner-management boundaries. The generated module is never written to
// this repository.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const memoryModule = "github.com/caelis-labs/memory"

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "facts_consumer_gate: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := memoryRoot()
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "memory-facts-consumer-gate-")
	if err != nil {
		return fmt.Errorf("create system temporary module: %w", err)
	}
	defer os.RemoveAll(temporary)

	goMod, err := temporaryGoMod(root)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(temporary, "go.mod"), goMod, 0o600); err != nil {
		return fmt.Errorf("write temporary go.mod: %w", err)
	}
	if sums, readErr := os.ReadFile(filepath.Join(root, "go.sum")); readErr == nil {
		if err := os.WriteFile(filepath.Join(temporary, "go.sum"), sums, 0o600); err != nil {
			return fmt.Errorf("write temporary go.sum: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(temporary, "facts_consumer_gate_test.go"), []byte(harness), 0o600); err != nil {
		return fmt.Errorf("write temporary harness: %w", err)
	}

	fmt.Fprintf(os.Stdout, "facts_consumer_gate: GOWORK=off go test ./... (temporary module %s)\n", temporary)
	command := exec.CommandContext(ctx, "go", "test", "./...")
	command.Dir = temporary
	command.Env = setEnv(os.Environ(), "GOWORK", "off")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("external consumer test: %w", err)
	}
	fmt.Fprintln(os.Stdout, "facts_consumer_gate: passed")
	return nil
}

func memoryRoot() (string, error) {
	starts := make([]string, 0, 2)
	if working, err := os.Getwd(); err == nil {
		starts = append(starts, working)
	}
	if _, source, _, ok := runtime.Caller(0); ok {
		starts = append(starts, filepath.Dir(source))
	}
	for _, start := range starts {
		current, err := filepath.Abs(start)
		if err != nil {
			continue
		}
		for {
			path := filepath.Join(current, "go.mod")
			contents, err := os.ReadFile(path)
			if err == nil && strings.Contains(string(contents), "module "+memoryModule+"\n") {
				return current, nil
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return "", fmt.Errorf("could not locate the %s module root", memoryModule)
}

func temporaryGoMod(root string) ([]byte, error) {
	contents, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("read source go.mod: %w", err)
	}
	lines := strings.Split(string(contents), "\n")
	moduleLine := false
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			lines[index] = "module example.com/memory-facts-consumer-gate"
			moduleLine = true
			break
		}
	}
	if !moduleLine {
		return nil, fmt.Errorf("source go.mod has no module declaration")
	}
	result := strings.Join(lines, "\n")
	result += fmt.Sprintf("\nrequire %s v0.0.0\nreplace %s => %s\n", memoryModule, memoryModule, goModPath(root))
	return []byte(result), nil
}

func goModPath(path string) string {
	path = filepath.ToSlash(path)
	if strings.ContainsAny(path, " \t\r\n\"") {
		return fmt.Sprintf("%q", path)
	}
	return path
}

func setEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	found := false
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			if !found {
				result = append(result, prefix+value)
				found = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !found {
		result = append(result, prefix+value)
	}
	return result
}

const harness = `package factsconsumer_test

import (
	"context"
	"strings"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	management "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
	steward "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
)

const (
	realmID       = "realm:m07"
	identityA     = "identity:m07-a"
	identityB     = "identity:m07-b"
	spaceA        = "space:m07-a"
	spaceB        = "space:m07-b"
	viewA         = "view:m07-a"
	viewB         = "view:m07-b"
	grantA        = "grant:m07-a"
	grantB        = "grant:m07-b"
	principalA    = "principal:m07-a"
	principalB    = "principal:m07-b"
	actorA        = "actor:m07-a"
	actorB        = "actor:m07-b"
	subjectA      = "subject:m07-a"
	subjectB      = "subject:m07-b"
	producer      = "caelis:m07"
	factKey       = "preference.drink"
)

type fixture struct {
	ctx       context.Context
	runtime   *appliance.Runtime
	reader    facts.Reader
	evidence  facts.EvidenceService
	management appliance.Management
	authA     memory.CallAuthorization
	authB     memory.CallAuthorization
	authOther memory.CallAuthorization
	readOnly  memory.CallAuthorization
	labelsA   memory.LabelSet
	labelsB   memory.LabelSet
	labelsOther memory.LabelSet
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	runtime, err := appliance.Open(ctx, appliance.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close Runtime: %v", err)
		}
	})

	managementPlane := runtime.Management()
	bootstrap, err := managementPlane.Bootstrap(ctx, management.BootstrapRequest{
		Realms: []management.Realm{{ID: realmID}},
		Identities: []management.Identity{
			{ID: identityA, RealmID: realmID},
			{ID: identityB, RealmID: realmID},
		},
		Spaces: []management.Space{
			{ID: spaceA, RealmID: realmID, IdentityID: identityA, Class: memory.SpaceClassPrivate},
			{ID: spaceB, RealmID: realmID, IdentityID: identityB, Class: memory.SpaceClassPrivate},
		},
		Views: []management.ViewDefinition{
			{ID: viewA, RealmID: realmID, ReadSpaceIDs: []memory.SpaceID{spaceA}, WriteSpaceID: spaceA, MaxDisclosureClass: memory.SpaceClassPrivate, Version: 1},
			{ID: viewB, RealmID: realmID, ReadSpaceIDs: []memory.SpaceID{spaceB}, WriteSpaceID: spaceB, MaxDisclosureClass: memory.SpaceClassPrivate, Version: 1},
		},
		Grants: []management.Grant{
			{ID: grantA, PrincipalRef: principalA, ActorRef: actorA, ViewRef: viewA, AllowedOperations: []memory.Operation{memory.OperationRemember, memory.OperationRecall}, AllowedAudiences: []memory.Audience{memory.AudiencePrivate}, ExpiresAt: time.Now().Add(time.Hour), Version: 1},
			{ID: grantB, PrincipalRef: principalB, ActorRef: actorB, ViewRef: viewB, AllowedOperations: []memory.Operation{memory.OperationRemember, memory.OperationRecall}, AllowedAudiences: []memory.Audience{memory.AudiencePrivate}, ExpiresAt: time.Now().Add(time.Hour), Version: 1},
		},
		IssuerPrincipals: []string{principalA, principalB},
	})
	if err != nil {
		t.Fatal(err)
	}

	labelsA := memory.LabelSet{memory.Label("workspace:m07-a")}
	labelsB := memory.LabelSet{memory.Label("workspace:m07-b")}
	labelsOther := memory.LabelSet{memory.Label("workspace:m07-other")}
	issue := func(principal string, grant string, actor string, labels memory.LabelSet, operations []memory.Operation) memory.CallAuthorization {
		issued, issueErr := runtime.IssueCapability(ctx, bootstrap.IssuerCredentials[principal], memory.CapabilityIssueRequest{
			PrincipalRef: principal,
			GrantRef: memory.GrantID(grant),
			ActorRef: actor,
			Audience: memory.AudiencePrivate,
			Operations: operations,
			Labels: labels,
			TTLSeconds: 3600,
		})
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		return memory.CallAuthorization{Capability: issued.Token, ActorRef: actor, Audience: memory.AudiencePrivate}
	}

	return &fixture{
		ctx: ctx, runtime: runtime, reader: runtime.Facts(), evidence: runtime.Evidence(), management: managementPlane,
		authA: issue(principalA, grantA, actorA, labelsA, []memory.Operation{memory.OperationRemember, memory.OperationRecall}),
		authB: issue(principalB, grantB, actorB, labelsB, []memory.Operation{memory.OperationRemember, memory.OperationRecall}),
		authOther: issue(principalA, grantA, actorA, labelsOther, []memory.Operation{memory.OperationRemember, memory.OperationRecall}),
		readOnly: issue(principalA, grantA, actorA, labelsA, []memory.Operation{memory.OperationRecall}),
		labelsA: labelsA, labelsB: labelsB, labelsOther: labelsOther,
	}
}

func TestFactsConsumerGate(t *testing.T) {
	f := newFixture(t)
	if f.reader == nil || f.evidence == nil {
		t.Fatal("Runtime.Facts() and Runtime.Evidence() must expose public planes")
	}

	if err := f.evidence.SetIngestionPolicy(f.ctx, facts.IngestionPolicy{
		Scope: facts.Scope{SpaceID: spaceA, Labels: f.labelsA}, Producer: producer, Subject: "subject:m07-denied", Deny: true, Reason: "consumer gate denial",
	}); err != nil {
		t.Fatalf("SetIngestionPolicy: %v", err)
	}

	_, err := f.runtime.DataPlane().Remember(f.ctx, f.readOnly, memory.RememberRequest{Text: "read-only must not write", IdempotencyKey: "m07-read-only-remember"})
	if !memory.IsCode(err, memory.ErrorCodeUnauthorized) {
		t.Fatalf("read-only Remember error = %v, want unauthorized", err)
	}
	_, err = f.evidence.SubmitEvidence(f.ctx, f.readOnly, establishedEvidence("subject:m07-read-only", "m07-read-only-evidence", "m07-read-only-effect", "read-only evidence"))
	if !memory.IsCode(err, memory.ErrorCodeUnauthorized) {
		t.Fatalf("read-only SubmitEvidence error = %v, want unauthorized", err)
	}

	denied := establishedEvidence("subject:m07-denied", "m07-denied", "m07-denied-effect", "denied source")
	deniedResponse, err := f.evidence.SubmitEvidence(f.ctx, f.authA, denied)
	if err != nil {
		t.Fatalf("denied SubmitEvidence: %v", err)
	}
	if deniedResponse.Accepted || deniedResponse.ReceiptID != "" || deniedResponse.RejectionReason == "" {
		t.Fatalf("denied SubmitEvidence = %+v, want explicit rejection", deniedResponse)
	}

	bResponse := submitFact(t, f, f.authB, subjectB, "m07-b", "B prefers tea", "m07-b-event", "m07-b-effect")
	if len(bResponse.Facts) != 1 || bResponse.Facts[0].SpaceID != spaceB {
		t.Fatalf("B evidence facts = %+v, want one fact in B scope", bResponse.Facts)
	}
	aResponse := submitFact(t, f, f.authA, subjectA, factKey, "A prefers coffee", "m07-a-event", "m07-a-effect")
	if len(aResponse.Facts) != 1 {
		t.Fatalf("A evidence facts = %+v, want one fact", aResponse.Facts)
	}
	assertEvidence(t, aResponse.Facts[0], aResponse.ReceiptID, subjectA, facts.AdoptionConfirmed)
	recordID := aResponse.Facts[0].RecordID
	if recordID == "" {
		t.Fatal("trusted evidence did not return a Record identity")
	}

	readA := readFacts(t, f, f.authA, facts.ReadRequest{Subject: subjectA, Key: factKey, Query: "coffee", Budget: budget()})
	if len(readA.Facts) != 1 || readA.Facts[0].Text != "A prefers coffee" {
		t.Fatalf("A ReadFacts = %+v, want A fact", readA.Facts)
	}
	if readA.Background == "" || !strings.Contains(readA.Background, "A prefers coffee") {
		t.Fatalf("A Background = %q, want bounded current fact", readA.Background)
	}
	if readA.Cursor.Generation == "" || readA.Cursor.Scope == "" || readA.Cursor.Sequence == 0 {
		t.Fatalf("A cursor = %+v, want generation/scope/sequence", readA.Cursor)
	}
	repeatA := readFacts(t, f, f.authA, facts.ReadRequest{Subject: subjectA, Key: factKey, Query: "coffee", Budget: budget()})
	if repeatA.Cursor != readA.Cursor {
		t.Fatalf("A cursor changed without a write: first=%+v repeat=%+v", readA.Cursor, repeatA.Cursor)
	}

	if got := readFacts(t, f, f.authB, facts.ReadRequest{Subject: subjectA, Key: factKey, Query: "coffee", Budget: budget()}); len(got.Facts) != 0 {
		t.Fatalf("B scope leaked A facts: %+v", got.Facts)
	}
	if got := readFacts(t, f, f.authOther, facts.ReadRequest{Subject: subjectA, Key: factKey, Query: "coffee", Budget: budget()}); len(got.Facts) != 0 {
		t.Fatalf("other label leaked A facts: %+v", got.Facts)
	}
	if got := readFacts(t, f, f.authA, facts.ReadRequest{Subject: subjectB, Key: "m07-b", Query: "tea", Budget: budget()}); len(got.Facts) != 0 {
		t.Fatalf("subject argument widened A scope: %+v", got.Facts)
	}
	mismatched, err := f.reader.Changes(f.ctx, f.authOther, facts.ChangesRequest{After: readA.Cursor, Limit: 32})
	if err != nil {
		t.Fatalf("label-mismatched Changes: %v", err)
	}
	if !mismatched.ResetRequired {
		t.Fatalf("label-mismatched Changes = %+v, want reset", mismatched)
	}

	trace, err := f.management.TraceRecord(f.ctx, management.TraceRecordRequest{RecordID: steward.RecordID(recordID)})
	if err != nil {
		t.Fatalf("TraceRecord before correction: %v", err)
	}
	if trace.State != management.RecordStateActive || len(trace.Revisions) != 1 {
		t.Fatalf("TraceRecord before correction = %+v, want active one revision", trace)
	}
	search, err := f.management.SearchReceipts(f.ctx, management.SearchReceiptsRequest{Query: "A prefers coffee", SpaceID: spaceA, Limit: 16, IncludeCorrected: true})
	if err != nil {
		t.Fatalf("SearchReceipts before correction: %v", err)
	}
	if !containsReceipt(search.Receipts, aResponse.ReceiptID) {
		t.Fatalf("SearchReceipts = %+v, want initial receipt", search.Receipts)
	}

	corrected := submitMutation(t, f, f.authA, facts.Source{Producer: producer, EventID: "m07-correct", Revision: "1", Fragment: "0", Subject: subjectA, Role: facts.RoleConfirmation}, "A prefers tea", "m07-correct-effect", facts.Mutation{Transition: facts.TransitionCorrect, TargetRecordID: recordID, ExpectedRevision: 1, Subject: subjectA, Key: factKey, Text: "A prefers tea"})
	if len(corrected.Facts) != 1 || corrected.Facts[0].Revision != 2 || corrected.Facts[0].Text != "A prefers tea" {
		t.Fatalf("correction response = %+v, want revision 2", corrected)
	}
	correctionChanges := changesAfter(t, f, f.authA, readA.Cursor)
	if !hasKind(correctionChanges.Changes, "fact_correct") {
		t.Fatalf("Changes after correction = %+v, want fact_correct", correctionChanges.Changes)
	}
	readAfterCorrection := readFacts(t, f, f.authA, facts.ReadRequest{Subject: subjectA, Key: factKey, Query: "tea", Budget: budget()})
	if len(readAfterCorrection.Facts) != 1 || readAfterCorrection.Facts[0].Revision != 2 || readAfterCorrection.Facts[0].Text != "A prefers tea" {
		t.Fatalf("ReadFacts after correction = %+v", readAfterCorrection.Facts)
	}
	history := readHistory(t, f, f.authA, facts.HistoryRequest{Subject: subjectA, RecordID: recordID, Budget: budget()})
	if len(history.Facts) < 2 || !hasHistoricalState(history.Facts, "corrected") {
		t.Fatalf("FactHistory after correction = %+v, want corrected prior revision", history.Facts)
	}
	trace, err = f.management.TraceRecord(f.ctx, management.TraceRecordRequest{RecordID: steward.RecordID(recordID)})
	if err != nil || trace.State != management.RecordStateActive || len(trace.Revisions) != 2 {
		t.Fatalf("TraceRecord after correction = %+v, %v", trace, err)
	}
	search, err = f.management.SearchReceipts(f.ctx, management.SearchReceiptsRequest{Query: "A prefers", SpaceID: spaceA, Limit: 16, IncludeCorrected: true})
	if err != nil || !containsReceipt(search.Receipts, aResponse.ReceiptID) || !containsReceipt(search.Receipts, corrected.ReceiptID) {
		t.Fatalf("SearchReceipts after correction = %+v, %v", search.Receipts, err)
	}

	deniedFact := submitMutation(t, f, f.authA, facts.Source{Producer: producer, EventID: "m07-deny", Revision: "1", Fragment: "0", Subject: subjectA, Role: facts.RoleConfirmation}, "A preference denied", "m07-deny-effect", facts.Mutation{Transition: facts.TransitionDeny, TargetRecordID: recordID, ExpectedRevision: 2, Subject: subjectA, Key: factKey, Text: "A preference denied"})
	if len(deniedFact.Facts) != 1 || deniedFact.Facts[0].Metadata.Adoption != facts.AdoptionDenied {
		t.Fatalf("deny response = %+v, want denied adoption", deniedFact)
	}
	denyChanges := changesAfter(t, f, f.authA, correctionChanges.Cursor)
	if !hasKind(denyChanges.Changes, "fact_deny") {
		t.Fatalf("Changes after deny = %+v, want fact_deny", denyChanges.Changes)
	}
	if got := readFacts(t, f, f.authA, facts.ReadRequest{Subject: subjectA, Key: factKey, Query: "tea", Budget: budget()}); len(got.Facts) != 0 || got.Background != "" {
		t.Fatalf("ReadFacts after deny = %+v, background=%q, want empty", got.Facts, got.Background)
	}
	trace, err = f.management.TraceRecord(f.ctx, management.TraceRecordRequest{RecordID: steward.RecordID(recordID)})
	if err != nil || trace.State != management.RecordStateInvalidated {
		t.Fatalf("TraceRecord after deny = %+v, %v, want invalidated", trace, err)
	}

	deleted, err := f.management.DeleteReceipt(f.ctx, management.DeleteReceiptRequest{ReceiptID: aResponse.ReceiptID, Reason: "M07 consumer gate", IdempotencyKey: "m07-delete-initial"})
	if err != nil || !deleted.Deleted || deleted.TombstoneID == "" {
		t.Fatalf("DeleteReceipt = %+v, %v", deleted, err)
	}
	cleanup := waitCleanup(t, f, aResponse.ReceiptID)
	if cleanup.State != management.CleanupStateCompleted {
		t.Fatalf("CleanupStatus = %+v, want completed", cleanup)
	}
	deleteChanges := changesAfter(t, f, f.authA, denyChanges.Cursor)
	if !hasKind(deleteChanges.Changes, "receipt_deleted") && !hasKind(deleteChanges.Changes, "record_forgotten") {
		t.Fatalf("Changes after delete = %+v, want deletion change", deleteChanges.Changes)
	}
	trace, err = f.management.TraceRecord(f.ctx, management.TraceRecordRequest{RecordID: steward.RecordID(recordID)})
	if err != nil || trace.State != management.RecordStateForgotten {
		t.Fatalf("TraceRecord after delete = %+v, %v, want forgotten", trace, err)
	}
	for _, revision := range trace.Revisions {
		if revision.Text != "" || len(revision.Evidence) == 0 {
			t.Fatalf("forgotten revision retained content: %+v", revision)
		}
	}
	search, err = f.management.SearchReceipts(f.ctx, management.SearchReceiptsRequest{Query: "A prefers coffee", SpaceID: spaceA, Limit: 16, IncludeCorrected: true})
	if err != nil {
		t.Fatalf("SearchReceipts after delete: %v", err)
	}
	if containsReceipt(search.Receipts, aResponse.ReceiptID) {
		t.Fatalf("SearchReceipts after delete returned forgotten receipt: %+v", search.Receipts)
	}
	if got := readFacts(t, f, f.authA, facts.ReadRequest{Subject: subjectA, Key: factKey, Query: "tea", Budget: budget()}); len(got.Facts) != 0 || got.Background != "" {
		t.Fatalf("ReadFacts after delete = %+v, background=%q, want empty", got.Facts, got.Background)
	}
	if got := readHistory(t, f, f.authA, facts.HistoryRequest{Subject: subjectA, RecordID: recordID, Budget: budget()}); len(got.Facts) != 0 {
		t.Fatalf("FactHistory after delete = %+v, want no resurrected facts", got.Facts)
	}
}

func establishedEvidence(subject, eventID, effect, text string) facts.SubmitEvidenceRequest {
	return facts.SubmitEvidenceRequest{
		Source: facts.Source{Producer: producer, EventID: eventID, Revision: "1", Fragment: "0", Subject: subject, Role: facts.RoleObservation},
		Text: text, IdempotencyKey: effect,
		Mutations: []facts.Mutation{{Transition: facts.TransitionEstablish, Subject: subject, Key: factKey, Text: text}},
	}
}

func submitFact(t *testing.T, f *fixture, auth memory.CallAuthorization, subject, key, text, eventID, effect string) facts.SubmitEvidenceResponse {
	t.Helper()
	request := establishedEvidence(subject, eventID, effect, text)
	request.Mutations[0].Key = key
	response, err := f.evidence.SubmitEvidence(f.ctx, auth, request)
	if err != nil || !response.Accepted || response.ReceiptID == "" {
		t.Fatalf("SubmitEvidence(%s) = %+v, %v", subject, response, err)
	}
	return response
}

func submitMutation(t *testing.T, f *fixture, auth memory.CallAuthorization, source facts.Source, text, effect string, mutation facts.Mutation) facts.SubmitEvidenceResponse {
	t.Helper()
	response, err := f.evidence.SubmitEvidence(f.ctx, auth, facts.SubmitEvidenceRequest{Source: source, Text: text, IdempotencyKey: effect, Mutations: []facts.Mutation{mutation}})
	if err != nil || !response.Accepted || response.ReceiptID == "" {
		t.Fatalf("SubmitEvidence mutation = %+v, %v", response, err)
	}
	return response
}

func readFacts(t *testing.T, f *fixture, auth memory.CallAuthorization, request facts.ReadRequest) facts.ReadResponse {
	t.Helper()
	response, err := f.reader.ReadFacts(f.ctx, auth, request)
	if err != nil {
		t.Fatalf("ReadFacts(%+v): %v", request, err)
	}
	return response
}

func readHistory(t *testing.T, f *fixture, auth memory.CallAuthorization, request facts.HistoryRequest) facts.ReadResponse {
	t.Helper()
	response, err := f.reader.FactHistory(f.ctx, auth, request)
	if err != nil {
		t.Fatalf("FactHistory(%+v): %v", request, err)
	}
	return response
}

func changesAfter(t *testing.T, f *fixture, auth memory.CallAuthorization, cursor facts.Cursor) facts.ChangesResponse {
	t.Helper()
	response, err := f.reader.Changes(f.ctx, auth, facts.ChangesRequest{After: cursor, Limit: 64})
	if err != nil {
		t.Fatalf("Changes(after=%+v): %v", cursor, err)
	}
	return response
}

func waitCleanup(t *testing.T, f *fixture, receipt memory.ReceiptID) management.CleanupStatusResponse {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		response, err := f.management.CleanupStatus(f.ctx, management.CleanupStatusRequest{ReceiptID: receipt})
		if err != nil {
			t.Fatalf("CleanupStatus: %v", err)
		}
		if response.State != management.CleanupStatePending {
			return response
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("CleanupStatus remained pending")
	return management.CleanupStatusResponse{}
}

func assertEvidence(t *testing.T, fact facts.Fact, receipt memory.ReceiptID, subject string, adoption facts.Adoption) {
	t.Helper()
	if fact.Metadata.Subject != subject || fact.Metadata.Adoption != adoption || len(fact.Evidence) != 1 || fact.Evidence[0].ReceiptID != receipt || fact.Evidence[0].Source == nil || fact.Evidence[0].Source.Subject != subject {
		t.Fatalf("trusted evidence attribution = %+v", fact)
	}
}

func budget() facts.Budget {
	return facts.Budget{MaxFacts: 8, MaxBytes: 64 << 10}
}

func hasKind(changes []facts.Change, wanted string) bool {
	for _, change := range changes {
		if strings.Contains(change.Kind, wanted) {
			return true
		}
	}
	return false
}

func hasHistoricalState(values []facts.Fact, wanted string) bool {
	for _, value := range values {
		if value.HistoricalState == wanted {
			return true
		}
	}
	return false
}

func containsReceipt(receipts []management.Receipt, wanted memory.ReceiptID) bool {
	for _, receipt := range receipts {
		if receipt.ReceiptID == wanted {
			return true
		}
	}
	return false
}
`
