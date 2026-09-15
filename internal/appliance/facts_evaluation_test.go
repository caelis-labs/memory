package appliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// The M06 facts longitudinal evaluation gate runs authored blocking
// trajectories against real on-disk SQLite and the owning facts/evidence APIs.
// It measures structural lifecycle, scope, privacy, restart and anti-
// resurrection invariants. It deliberately does not measure paraphrase,
// extraction, model or consumer-answer quality, and it never treats machine-
// expanded candidates as human-reviewed gold.
const (
	factsEvalFixtureDir = "testdata/facts_eval"
	factsEvalManifest   = "manifest.json"

	// Frozen before any measurement. The gate refuses a manifest that lowers
	// these, so a result can never come from relaxing the bar after tuning.
	factsEvalFrozenExplicitPreferenceMin = 0.95
	factsEvalFrozenRecallAt8AliasMin     = 0.90

	factsEvalCandidatesEnv = "MEMORY_FACTS_EVAL_CANDIDATES"
	factsEvalReportEnv     = "MEMORY_FACTS_EVAL_REPORT"

	factsEvalFixedClock = "2026-09-01T00:00:00Z"
)

type factsEvalManifestDoc struct {
	FormatVersion      int                         `json:"format_version"`
	Corpus             string                      `json:"corpus"`
	ReviewStatus       string                      `json:"review_status"`
	HumanReviewedCases int                         `json:"human_reviewed_cases"`
	ReleaseBlocker     string                      `json:"release_blocker"`
	Thresholds         map[string]float64          `json:"thresholds"`
	ControlledAliases  map[string][]string         `json:"controlled_aliases"`
	Fixtures           []factsEvalManifestFixture  `json:"fixtures"`
	CandidateExpansion factsEvalCandidateExpansion `json:"candidate_expansion"`
}

type factsEvalManifestFixture struct {
	File                 string `json:"file"`
	SHA256               string `json:"sha256"`
	ExpectedTrajectories int    `json:"expected_trajectories"`
}

type factsEvalCandidateExpansion struct {
	Generator          string   `json:"generator"`
	Subjects           int      `json:"subjects"`
	Templates          int      `json:"templates"`
	ExpectedCandidates int      `json:"expected_candidates"`
	ReviewStatus       string   `json:"review_status"`
	Tiers              []string `json:"tiers"`
}

type factsEvalTrajectoryDoc struct {
	FormatVersion int                   `json:"format_version"`
	Corpus        string                `json:"corpus"`
	ReviewStatus  string                `json:"review_status"`
	Trajectories  []factsEvalTrajectory `json:"trajectories"`
}

type factsEvalTrajectory struct {
	ID           string          `json:"id"`
	Tier         string          `json:"tier"`
	Language     string          `json:"language"`
	ReviewStatus string          `json:"review_status"`
	Summary      string          `json:"summary"`
	Scope        string          `json:"scope"`
	Steps        []factsEvalStep `json:"steps"`
}

type factsEvalStep struct {
	Op             string              `json:"op"`
	ID             string              `json:"id"`
	Scope          string              `json:"scope"`
	Producer       string              `json:"producer"`
	EventID        string              `json:"event_id"`
	Revision       string              `json:"revision"`
	Fragment       string              `json:"fragment"`
	Role           string              `json:"role"`
	Subject        string              `json:"subject"`
	FactKey        string              `json:"fact_key"`
	Text           string              `json:"text"`
	OccurredAt     string              `json:"occurred_at"`
	IdempotencyKey string              `json:"idempotency_key"`
	SourceRef      string              `json:"source_ref"`
	Mutations      []factsEvalMutation `json:"mutations"`
	Deny           *bool               `json:"deny"`
	Reason         string              `json:"reason"`
	ReceiptRef     string              `json:"receipt_ref"`
	Key            string              `json:"key"`
	Query          string              `json:"query"`
	AsOf           string              `json:"as_of"`
	Context        map[string]string   `json:"context"`
	Budget         *factsEvalBudget    `json:"budget"`
	FactRef        string              `json:"fact_ref"`
	AfterSequence  uint64              `json:"after_sequence"`
	Want           factsEvalWant       `json:"want"`
}

type factsEvalMutation struct {
	Transition       string               `json:"transition"`
	TargetRef        string               `json:"target_ref"`
	TargetRecordID   string               `json:"target_record_id"`
	ExpectedRevision *uint64              `json:"expected_revision"`
	Subject          string               `json:"subject"`
	Key              string               `json:"key"`
	Text             string               `json:"text"`
	ValidFrom        string               `json:"valid_from"`
	ValidUntil       string               `json:"valid_until"`
	Conditions       []factsEvalCondition `json:"conditions"`
}

type factsEvalCondition struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type factsEvalBudget struct {
	MaxFacts int `json:"max_facts"`
	MaxBytes int `json:"max_bytes"`
}

type factsEvalWant struct {
	ErrorCode                   string              `json:"error_code"`
	Accepted                    *bool               `json:"accepted"`
	Deduplicated                *bool               `json:"deduplicated"`
	RejectionReasonPrefix       string              `json:"rejection_reason_prefix"`
	Organization                string              `json:"organization"`
	Facts                       []factsEvalWantFact `json:"facts"`
	ReceiptRef                  string              `json:"receipt_ref"`
	RecordCount                 *int                `json:"record_count"`
	RecordCountMin              int                 `json:"record_count_min"`
	StatesInclude               []string            `json:"states_include"`
	State                       string              `json:"state"`
	Cleanup                     string              `json:"cleanup"`
	RevisionCount               *int                `json:"revision_count"`
	RevisionTextsEmpty          *bool               `json:"revision_texts_empty"`
	FactTexts                   []string            `json:"fact_texts"`
	FactTextsInclude            []string            `json:"fact_texts_include"`
	FactCount                   *int                `json:"fact_count"`
	FactCountMin                int                 `json:"fact_count_min"`
	FactCountMax                int                 `json:"fact_count_max"`
	BackgroundContains          []string            `json:"background_contains"`
	Truncated                   *bool               `json:"truncated"`
	BytesUsedMax                int                 `json:"bytes_used_max"`
	ResetRequired               *bool               `json:"reset_required"`
	KindsInclude                []string            `json:"kinds_include"`
	ContainsText                string              `json:"contains_text"`
	MaxRank                     int                 `json:"max_rank"`
	Deleted                     *bool               `json:"deleted"`
	InvalidationVersionPositive bool                `json:"invalidation_version_positive"`
}

type factsEvalWantFact struct {
	Text          string   `json:"text"`
	Key           string   `json:"key"`
	Adoption      string   `json:"adoption"`
	Transition    string   `json:"transition"`
	EvidenceRefs  []string `json:"evidence_refs"`
	RelatedRef    string   `json:"related_ref"`
	HasConditions *bool    `json:"has_conditions"`
}

type factsEvalFactRef struct {
	RecordID        string
	Revision        uint64
	Text            string
	Key             string
	Subject         string
	SpaceID         v1alpha1.SpaceID
	Adoption        string
	RelatedRecordID string
}

type factsEvalScopeDef struct {
	Key        string
	Space      v1alpha1.SpaceID
	Labels     v1alpha1.LabelSet
	Actor      string
	Audience   v1alpha1.Audience
	Grant      v1alpha1.GrantID
	Operations []v1alpha1.Operation
}

var factsEvalDefaultOperations = []v1alpha1.Operation{
	v1alpha1.OperationRemember,
	v1alpha1.OperationRecall,
	v1alpha1.OperationReceiptStatus,
}

var factsEvalScopes = []factsEvalScopeDef{
	{Key: "a", Space: "space-bot-a", Labels: v1alpha1.LabelSet{"workspace:alpha"}, Actor: "actor-bot-a", Audience: v1alpha1.AudiencePrivate, Grant: "grant-bot-a", Operations: factsEvalDefaultOperations},
	{Key: "a_alt_label", Space: "space-bot-a", Labels: v1alpha1.LabelSet{"workspace:beta"}, Actor: "actor-bot-a", Audience: v1alpha1.AudiencePrivate, Grant: "grant-bot-a", Operations: factsEvalDefaultOperations},
	{Key: "b", Space: "space-bot-b", Labels: v1alpha1.LabelSet{"workspace:beta"}, Actor: "actor-bot-b", Audience: v1alpha1.AudiencePrivate, Grant: "grant-bot-b", Operations: factsEvalDefaultOperations},
	{Key: "shared", Space: "space-shared", Labels: v1alpha1.LabelSet{"workspace:alpha"}, Actor: "actor-shared-a", Audience: v1alpha1.AudienceShared, Grant: "grant-shared-a", Operations: factsEvalDefaultOperations},
	{Key: "readonly", Space: "space-bot-a", Labels: v1alpha1.LabelSet{"workspace:alpha"}, Actor: "actor-bot-a", Audience: v1alpha1.AudiencePrivate, Grant: "grant-recall-only", Operations: []v1alpha1.Operation{v1alpha1.OperationRecall}},
}

func factsEvalScopeIndex() map[string]factsEvalScopeDef {
	out := make(map[string]factsEvalScopeDef, len(factsEvalScopes))
	for _, scope := range factsEvalScopes {
		out[scope.Key] = scope
	}
	return out
}

type factsEvalHarness struct {
	t          *testing.T
	ctx        context.Context
	clock      func() time.Time
	dataDir    string
	store      *Store
	scopes     map[string]factsEvalScopeDef
	auths      map[string]v1alpha1.CallAuthorization
	requests   map[string]facts.SubmitEvidenceRequest
	facts      map[string]factsEvalFactRef
	receipts   map[string]v1alpha1.ReceiptID
	mismatches int
	recallRuns int
	recallHits int
}

func loadFactsEvalManifest(t *testing.T, dir string) factsEvalManifestDoc {
	t.Helper()
	path := filepath.Join(dir, factsEvalManifest)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read facts evaluation manifest: %v", err)
	}
	var manifest factsEvalManifestDoc
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse facts evaluation manifest: %v", err)
	}
	if manifest.FormatVersion != 1 {
		t.Fatalf("facts evaluation manifest format_version = %d", manifest.FormatVersion)
	}
	if manifest.ReviewStatus != "not_human_reviewed" || manifest.HumanReviewedCases != 0 {
		t.Fatalf("facts evaluation manifest claims human review (%q, %d cases) without evidence", manifest.ReviewStatus, manifest.HumanReviewedCases)
	}
	if strings.TrimSpace(manifest.ReleaseBlocker) == "" {
		t.Fatal("facts evaluation manifest must record the outstanding human-review release blocker")
	}
	for name, floor := range map[string]float64{
		"explicit_preference_min": factsEvalFrozenExplicitPreferenceMin,
		"recall_at8_alias_min":    factsEvalFrozenRecallAt8AliasMin,
	} {
		got, ok := manifest.Thresholds[name]
		if !ok {
			t.Fatalf("facts evaluation manifest is missing frozen threshold %q", name)
		}
		if got < floor {
			t.Fatalf("facts evaluation manifest threshold %s = %v is below the frozen floor %v; thresholds may not be loosened after tuning", name, got, floor)
		}
	}
	if len(manifest.ControlledAliases) == 0 {
		t.Fatal("facts evaluation manifest has no controlled alias dictionary")
	}
	for _, fixture := range manifest.Fixtures {
		raw, err := os.ReadFile(filepath.Join(dir, fixture.File))
		if err != nil {
			t.Fatalf("read facts evaluation fixture %s: %v", fixture.File, err)
		}
		digest := sha256.Sum256(raw)
		if got := hex.EncodeToString(digest[:]); got != fixture.SHA256 {
			t.Fatalf("facts evaluation fixture %s digest = %s, manifest %s; review the delta, then update the manifest", fixture.File, got, fixture.SHA256)
		}
	}
	return manifest
}

func loadFactsEvalTrajectories(t *testing.T, dir string) factsEvalTrajectoryDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "trajectories.json"))
	if err != nil {
		t.Fatalf("read facts evaluation trajectories: %v", err)
	}
	var doc factsEvalTrajectoryDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse facts evaluation trajectories: %v", err)
	}
	return doc
}

func newFactsEvalHarness(t *testing.T, dataDir string) *factsEvalHarness {
	t.Helper()
	clock := func() time.Time {
		parsed, err := time.Parse(time.RFC3339, factsEvalFixedClock)
		if err != nil {
			t.Fatalf("fixed clock: %v", err)
		}
		return parsed
	}
	store, credentials := bootstrapFixture(t, Options{DataDir: dataDir, Clock: clock})
	h := &factsEvalHarness{
		t:        t,
		ctx:      t.Context(),
		clock:    clock,
		dataDir:  dataDir,
		store:    store,
		scopes:   factsEvalScopeIndex(),
		auths:    map[string]v1alpha1.CallAuthorization{},
		requests: map[string]facts.SubmitEvidenceRequest{},
		facts:    map[string]factsEvalFactRef{},
		receipts: map[string]v1alpha1.ReceiptID{},
	}
	for _, scope := range factsEvalScopes {
		capability, err := store.IssueCapability(h.ctx, IssueCapabilityRequest{
			Authorization: IssuerAuthorization{PrincipalRef: "principal:" + scope.Actor, Credential: credentials["principal:"+scope.Actor]},
			GrantRef:      scope.Grant,
			ActorRef:      scope.Actor,
			Audience:      scope.Audience,
			Operations:    scope.Operations,
			Labels:        scope.Labels,
			TTL:           time.Hour,
		})
		if err != nil {
			t.Fatalf("issue facts evaluation capability for scope %q: %v", scope.Key, err)
		}
		h.auths[scope.Key] = v1alpha1.CallAuthorization{Capability: capability.Token, ActorRef: scope.Actor, Audience: scope.Audience}
	}
	return h
}

func (h *factsEvalHarness) scope(key string) factsEvalScopeDef {
	scope, ok := h.scopes[key]
	if !ok {
		h.t.Fatalf("facts evaluation scope %q is not defined", key)
	}
	return scope
}

func (h *factsEvalHarness) auth(key string) v1alpha1.CallAuthorization {
	auth, ok := h.auths[key]
	if !ok {
		h.t.Fatalf("facts evaluation scope %q has no capability", key)
	}
	return auth
}

func (h *factsEvalHarness) mismatchf(format string, args ...any) {
	h.mismatches++
	h.t.Errorf(format, args...)
}

func (h *factsEvalHarness) restart() {
	if err := h.store.Close(); err != nil {
		h.t.Fatalf("close facts evaluation store for restart: %v", err)
	}
	store, err := Open(h.ctx, Options{DataDir: h.dataDir, Clock: h.clock})
	if err != nil {
		h.t.Fatalf("reopen facts evaluation store: %v", err)
	}
	h.store = store
}

func (h *factsEvalHarness) runTrajectory(trajectory factsEvalTrajectory) int {
	before := h.mismatches
	for index := range trajectory.Steps {
		h.runStep(&trajectory, &trajectory.Steps[index])
	}
	return h.mismatches - before
}

func (h *factsEvalHarness) runStep(trajectory *factsEvalTrajectory, step *factsEvalStep) {
	scopeKey := step.Scope
	if scopeKey == "" {
		scopeKey = trajectory.Scope
	}
	switch step.Op {
	case "ingest":
		h.stepIngest(trajectory, step, scopeKey)
	case "policy":
		h.stepPolicy(step, scopeKey)
	case "restart":
		h.restart()
	case "forget":
		h.stepForget(step)
	case "rebuild_fts":
		if err := h.store.RebuildFTS(h.ctx); err != nil {
			h.mismatchf("trajectory %s: rebuild FTS: %v", trajectory.ID, err)
		}
	case "list_records":
		h.stepListRecords(trajectory, step, scopeKey)
	case "trace_record":
		h.stepTraceRecord(trajectory, step)
	case "fact_history":
		h.stepFactHistory(trajectory, step, scopeKey)
	case "read_facts":
		h.stepReadFacts(trajectory, step, scopeKey)
	case "changes":
		h.stepChanges(trajectory, step, scopeKey)
	case "recall":
		h.stepRecall(trajectory, step, scopeKey)
	default:
		h.t.Fatalf("trajectory %s: unsupported step op %q", trajectory.ID, step.Op)
	}
}

func (h *factsEvalHarness) stepIngest(trajectory *factsEvalTrajectory, step *factsEvalStep, scopeKey string) {
	request := facts.SubmitEvidenceRequest{
		Source: facts.Source{
			Producer: step.Producer, EventID: step.EventID, Revision: step.Revision,
			Fragment: step.Fragment, Subject: step.Subject, FactKey: step.FactKey, Role: facts.Role(step.Role),
		},
		Text:           step.Text,
		OccurredAt:     factsEvalParseOptionalTime(h.t, step.OccurredAt),
		IdempotencyKey: step.IdempotencyKey,
	}
	if step.SourceRef != "" {
		replayed, ok := h.requests[step.SourceRef]
		if !ok {
			h.t.Fatalf("trajectory %s step %s: source ref %q is unknown", trajectory.ID, step.ID, step.SourceRef)
		}
		request = replayed
	} else {
		for _, mutation := range step.Mutations {
			request.Mutations = append(request.Mutations, h.resolveMutation(trajectory, step, mutation))
		}
	}
	if step.ID != "" {
		h.requests[step.ID] = request
	}
	response, err := h.store.SubmitEvidence(h.ctx, h.auth(scopeKey), request)
	h.expectIngest(trajectory, step, scopeKey, response, err)
}

func (h *factsEvalHarness) resolveMutation(trajectory *factsEvalTrajectory, step *factsEvalStep, mutation factsEvalMutation) facts.Mutation {
	subject := mutation.Subject
	if subject == "" {
		subject = step.Subject
	}
	out := facts.Mutation{
		Transition: facts.Transition(mutation.Transition),
		Subject:    subject,
		Key:        mutation.Key,
		Text:       mutation.Text,
		ValidFrom:  factsEvalParseOptionalTime(h.t, mutation.ValidFrom),
		ValidUntil: factsEvalParseOptionalTime(h.t, mutation.ValidUntil),
	}
	for _, condition := range mutation.Conditions {
		out.Conditions = append(out.Conditions, facts.Condition{Key: condition.Key, Value: condition.Value})
	}
	if mutation.TargetRef != "" {
		ref, ok := h.facts[mutation.TargetRef]
		if !ok {
			h.t.Fatalf("trajectory %s step %s: target ref %q is unknown", trajectory.ID, step.ID, mutation.TargetRef)
		}
		out.TargetRecordID = ref.RecordID
		out.ExpectedRevision = ref.Revision
		if out.Key == "" {
			out.Key = ref.Key
		}
	}
	if mutation.TargetRecordID != "" {
		out.TargetRecordID = mutation.TargetRecordID
	}
	if mutation.ExpectedRevision != nil {
		out.ExpectedRevision = *mutation.ExpectedRevision
	}
	return out
}

func (h *factsEvalHarness) expectIngest(trajectory *factsEvalTrajectory, step *factsEvalStep, scopeKey string, response facts.SubmitEvidenceResponse, err error) {
	label := fmt.Sprintf("trajectory %s step %s", trajectory.ID, step.ID)
	if step.Want.ErrorCode != "" {
		if err == nil {
			h.mismatchf("%s: expected %s error, got response %+v", label, step.Want.ErrorCode, response)
			return
		}
		if !v1alpha1.IsCode(err, v1alpha1.ErrorCode(step.Want.ErrorCode)) {
			h.mismatchf("%s: expected %s error, got %v", label, step.Want.ErrorCode, err)
		}
		return
	}
	if err != nil {
		h.mismatchf("%s: unexpected error %v", label, err)
		return
	}
	if step.Want.Accepted != nil && response.Accepted != *step.Want.Accepted {
		h.mismatchf("%s: accepted = %v, want %v (%s)", label, response.Accepted, *step.Want.Accepted, response.RejectionReason)
	}
	if step.Want.Deduplicated != nil && response.Deduplicated != *step.Want.Deduplicated {
		h.mismatchf("%s: deduplicated = %v, want %v", label, response.Deduplicated, *step.Want.Deduplicated)
	}
	if step.Want.RejectionReasonPrefix != "" && !strings.HasPrefix(response.RejectionReason, step.Want.RejectionReasonPrefix) {
		h.mismatchf("%s: rejection reason %q does not start with %q", label, response.RejectionReason, step.Want.RejectionReasonPrefix)
	}
	if step.Want.Organization != "" && string(response.Organization) != step.Want.Organization {
		h.mismatchf("%s: organization = %q, want %q", label, response.Organization, step.Want.Organization)
	}
	if step.Want.ReceiptRef != "" {
		want, ok := h.receipts[step.Want.ReceiptRef]
		if !ok {
			h.t.Fatalf("%s: receipt ref %q is unknown", label, step.Want.ReceiptRef)
		}
		if response.ReceiptID != want {
			h.mismatchf("%s: receipt = %q, want %q", label, response.ReceiptID, want)
		}
	}
	if !response.Accepted {
		return
	}
	if step.ID != "" {
		h.receipts[step.ID] = response.ReceiptID
		for index, fact := range response.Facts {
			h.facts[step.ID+".f"+strconv.Itoa(index)] = factsEvalFactRef{
				RecordID: fact.RecordID, Revision: fact.Revision, Text: fact.Text, Key: fact.Metadata.Key,
				Subject: fact.Metadata.Subject, SpaceID: fact.SpaceID, Adoption: string(fact.Metadata.Adoption),
				RelatedRecordID: fact.Metadata.RelatedRecordID,
			}
		}
	}
	h.checkWantFacts(label, step.ID, response.Facts, step.Want.Facts)
}

func (h *factsEvalHarness) checkWantFacts(label, stepID string, got []facts.Fact, want []factsEvalWantFact) {
	for index, expected := range want {
		if index >= len(got) {
			h.mismatchf("%s: fact %d missing, only %d returned", label, index, len(got))
			continue
		}
		fact := got[index]
		if fact.Text != expected.Text {
			h.mismatchf("%s: fact %d text = %q, want %q", label, index, fact.Text, expected.Text)
		}
		if expected.Key != "" && fact.Metadata.Key != expected.Key {
			h.mismatchf("%s: fact %d key = %q, want %q", label, index, fact.Metadata.Key, expected.Key)
		}
		if expected.Adoption != "" && string(fact.Metadata.Adoption) != expected.Adoption {
			h.mismatchf("%s: fact %d adoption = %q, want %q", label, index, fact.Metadata.Adoption, expected.Adoption)
		}
		if expected.Transition != "" && string(fact.Metadata.Transition) != expected.Transition {
			h.mismatchf("%s: fact %d transition = %q, want %q", label, index, fact.Metadata.Transition, expected.Transition)
		}
		if expected.HasConditions != nil && (len(fact.Metadata.Conditions) > 0) != *expected.HasConditions {
			h.mismatchf("%s: fact %d conditions = %+v, want has_conditions %v", label, index, fact.Metadata.Conditions, *expected.HasConditions)
		}
		for _, evidenceRef := range expected.EvidenceRefs {
			receipt, ok := h.receipts[evidenceRef]
			if !ok {
				h.t.Fatalf("%s: evidence ref %q is unknown", label, evidenceRef)
			}
			found := false
			for _, evidence := range fact.Evidence {
				if evidence.ReceiptID == receipt {
					found = true
				}
			}
			if !found {
				h.mismatchf("%s: fact %d evidence %+v does not include %q", label, index, fact.Evidence, receipt)
			}
		}
		if expected.RelatedRef != "" {
			related, ok := h.facts[expected.RelatedRef+".f0"]
			if !ok {
				// The related reference may be expressed as a step id whose first
				// fact is the target.
				related, ok = h.facts[expected.RelatedRef]
			}
			if !ok {
				h.t.Fatalf("%s: related ref %q is unknown", label, expected.RelatedRef)
			}
			if fact.Metadata.RelatedRecordID != related.RecordID {
				h.mismatchf("%s: fact %d related record = %q, want %q", label, index, fact.Metadata.RelatedRecordID, related.RecordID)
			}
		}
	}
}

func (h *factsEvalHarness) stepPolicy(step *factsEvalStep, scopeKey string) {
	scope := h.scope(scopeKey)
	if step.Deny == nil {
		h.t.Fatalf("policy step requires deny")
	}
	err := h.store.SetIngestionPolicy(h.ctx, facts.IngestionPolicy{
		Scope:    facts.Scope{SpaceID: scope.Space, Labels: scope.Labels},
		Producer: step.Producer, Subject: step.Subject, Deny: *step.Deny, Reason: step.Reason,
	})
	if err != nil {
		h.mismatchf("policy %s/%s: %v", step.Producer, step.Subject, err)
	}
}

func (h *factsEvalHarness) stepForget(step *factsEvalStep) {
	receipt, ok := h.receipts[step.ReceiptRef]
	if !ok {
		h.t.Fatalf("forget step receipt ref %q is unknown", step.ReceiptRef)
	}
	response, err := h.store.DeleteReceipt(h.ctx, managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: receipt, Reason: step.Reason, IdempotencyKey: step.IdempotencyKey,
	})
	if err != nil {
		h.mismatchf("forget %s: %v", receipt, err)
		return
	}
	if step.Want.Deleted != nil && response.Deleted != *step.Want.Deleted {
		h.mismatchf("forget %s: deleted = %v, want %v", receipt, response.Deleted, *step.Want.Deleted)
	}
	if step.Want.Cleanup != "" && string(response.Cleanup) != step.Want.Cleanup {
		h.mismatchf("forget %s: cleanup = %q, want %q", receipt, response.Cleanup, step.Want.Cleanup)
	}
	if step.Want.InvalidationVersionPositive && response.InvalidationVersion == 0 {
		h.mismatchf("forget %s: invalidation_version is zero", receipt)
	}
}

func (h *factsEvalHarness) stepListRecords(trajectory *factsEvalTrajectory, step *factsEvalStep, scopeKey string) {
	scope := h.scope(scopeKey)
	response, err := h.store.ListRecords(h.ctx, managementv1alpha1.ListRecordsRequest{SpaceID: scope.Space, Limit: 100})
	if err != nil {
		h.mismatchf("trajectory %s: list records in %s: %v", trajectory.ID, scope.Space, err)
		return
	}
	label := fmt.Sprintf("trajectory %s list_records %s", trajectory.ID, scope.Space)
	// ListRecords is Space-scoped; the fixture subject is attribution, not a
	// listing selector, so the whole Space page is checked.
	if step.Want.RecordCount != nil && len(response.Records) != *step.Want.RecordCount {
		h.mismatchf("%s: record count = %d, want %d", label, len(response.Records), *step.Want.RecordCount)
	}
	if step.Want.RecordCountMin != 0 && len(response.Records) < step.Want.RecordCountMin {
		h.mismatchf("%s: record count = %d, want at least %d", label, len(response.Records), step.Want.RecordCountMin)
	}
	if len(step.Want.StatesInclude) != 0 {
		states := map[string]bool{}
		for _, record := range response.Records {
			states[string(record.State)] = true
		}
		for _, want := range step.Want.StatesInclude {
			if !states[want] {
				h.mismatchf("%s: states %v do not include %q", label, states, want)
			}
		}
	}
}

func (h *factsEvalHarness) factRef(step *factsEvalStep) factsEvalFactRef {
	ref, ok := h.facts[step.FactRef]
	if !ok {
		h.t.Fatalf("fact ref %q is unknown", step.FactRef)
	}
	return ref
}

func (h *factsEvalHarness) stepTraceRecord(trajectory *factsEvalTrajectory, step *factsEvalStep) {
	ref := h.factRef(step)
	response, err := h.store.TraceRecord(h.ctx, managementv1alpha1.TraceRecordRequest{RecordID: stewardv1alpha1.RecordID(ref.RecordID)})
	if err != nil {
		h.mismatchf("trajectory %s: trace record %s: %v", trajectory.ID, ref.RecordID, err)
		return
	}
	label := fmt.Sprintf("trajectory %s trace_record %s", trajectory.ID, ref.RecordID)
	if step.Want.State != "" && string(response.State) != step.Want.State {
		h.mismatchf("%s: state = %q, want %q", label, response.State, step.Want.State)
	}
	if step.Want.Cleanup != "" && string(response.Cleanup) != step.Want.Cleanup {
		h.mismatchf("%s: cleanup = %q, want %q", label, response.Cleanup, step.Want.Cleanup)
	}
	if step.Want.RevisionCount != nil && len(response.Revisions) != *step.Want.RevisionCount {
		h.mismatchf("%s: revision count = %d, want %d", label, len(response.Revisions), *step.Want.RevisionCount)
	}
	if step.Want.RevisionTextsEmpty != nil {
		empty := true
		for _, revision := range response.Revisions {
			if revision.Text != "" {
				empty = false
			}
		}
		if empty != *step.Want.RevisionTextsEmpty {
			h.mismatchf("%s: revision texts empty = %v, want %v", label, empty, *step.Want.RevisionTextsEmpty)
		}
	}
}

func (h *factsEvalHarness) stepFactHistory(trajectory *factsEvalTrajectory, step *factsEvalStep, scopeKey string) {
	ref := h.factRef(step)
	response, err := h.store.FactHistory(h.ctx, h.auth(scopeKey), facts.HistoryRequest{
		Subject: ref.Subject, RecordID: ref.RecordID, Budget: h.budget(step),
	})
	if err != nil {
		h.mismatchf("trajectory %s: fact history %s: %v", trajectory.ID, ref.RecordID, err)
		return
	}
	label := fmt.Sprintf("trajectory %s fact_history %s", trajectory.ID, ref.RecordID)
	if step.Want.RevisionCount != nil && len(response.Facts) != *step.Want.RevisionCount {
		h.mismatchf("%s: revision count = %d, want %d", label, len(response.Facts), *step.Want.RevisionCount)
	}
	if step.Want.FactCount != nil && len(response.Facts) != *step.Want.FactCount {
		h.mismatchf("%s: fact count = %d, want %d", label, len(response.Facts), *step.Want.FactCount)
	}
	h.checkTexts(label, response, step.Want)
}

func (h *factsEvalHarness) stepReadFacts(trajectory *factsEvalTrajectory, step *factsEvalStep, scopeKey string) {
	response, err := h.store.ReadFacts(h.ctx, h.auth(scopeKey), facts.ReadRequest{
		Subject: step.Subject, Key: step.Key, Query: step.Query,
		AsOf:    factsEvalParseOptionalTime(h.t, step.AsOf),
		Context: step.Context, Budget: h.budget(step),
	})
	if err != nil {
		h.mismatchf("trajectory %s: read facts: %v", trajectory.ID, err)
		return
	}
	label := fmt.Sprintf("trajectory %s read_facts", trajectory.ID)
	if step.Want.FactCount != nil && len(response.Facts) != *step.Want.FactCount {
		h.mismatchf("%s: fact count = %d, want %d (texts %v)", label, len(response.Facts), *step.Want.FactCount, factsEvalFactTexts(response.Facts))
	}
	if step.Want.FactCountMin != 0 && len(response.Facts) < step.Want.FactCountMin {
		h.mismatchf("%s: fact count = %d, want at least %d", label, len(response.Facts), step.Want.FactCountMin)
	}
	if step.Want.FactCountMax != 0 && len(response.Facts) > step.Want.FactCountMax {
		h.mismatchf("%s: fact count = %d, want at most %d", label, len(response.Facts), step.Want.FactCountMax)
	}
	if step.Want.Truncated != nil && response.Truncated != *step.Want.Truncated {
		h.mismatchf("%s: truncated = %v, want %v", label, response.Truncated, *step.Want.Truncated)
	}
	if step.Want.BytesUsedMax != 0 && response.BytesUsed > step.Want.BytesUsedMax {
		h.mismatchf("%s: bytes used = %d, want at most %d", label, response.BytesUsed, step.Want.BytesUsedMax)
	}
	for _, needle := range step.Want.BackgroundContains {
		if !strings.Contains(response.Background, needle) {
			h.mismatchf("%s: background does not contain %q", label, needle)
		}
	}
	h.checkTexts(label, response, step.Want)
}

func (h *factsEvalHarness) checkTexts(label string, response facts.ReadResponse, want factsEvalWant) {
	if len(want.FactTexts) != 0 {
		if len(response.Facts) != len(want.FactTexts) {
			h.mismatchf("%s: facts = %v, want %v", label, factsEvalFactTexts(response.Facts), want.FactTexts)
		} else {
			for index, text := range want.FactTexts {
				if response.Facts[index].Text != text {
					h.mismatchf("%s: fact %d text = %q, want %q", label, index, response.Facts[index].Text, text)
				}
			}
		}
	}
	for _, text := range want.FactTextsInclude {
		found := false
		for _, fact := range response.Facts {
			if fact.Text == text {
				found = true
			}
		}
		if !found {
			h.mismatchf("%s: facts %v do not include %q", label, factsEvalFactTexts(response.Facts), text)
		}
	}
}

func (h *factsEvalHarness) stepChanges(trajectory *factsEvalTrajectory, step *factsEvalStep, scopeKey string) {
	auth := h.auth(scopeKey)
	first, err := h.store.Changes(h.ctx, auth, facts.ChangesRequest{Limit: 256})
	if err != nil {
		h.mismatchf("trajectory %s: changes cursor: %v", trajectory.ID, err)
		return
	}
	after := first.Cursor
	after.Sequence = step.AfterSequence
	response, err := h.store.Changes(h.ctx, auth, facts.ChangesRequest{After: after, Limit: 256})
	if err != nil {
		h.mismatchf("trajectory %s: changes: %v", trajectory.ID, err)
		return
	}
	label := fmt.Sprintf("trajectory %s changes", trajectory.ID)
	if step.Want.ResetRequired != nil && response.ResetRequired != *step.Want.ResetRequired {
		h.mismatchf("%s: reset_required = %v, want %v", label, response.ResetRequired, *step.Want.ResetRequired)
	}
	if len(step.Want.KindsInclude) != 0 {
		kinds := map[string]bool{}
		for _, change := range response.Changes {
			kinds[change.Kind] = true
		}
		for _, want := range step.Want.KindsInclude {
			if !kinds[want] {
				h.mismatchf("%s: kinds %v do not include %q", label, kinds, want)
			}
		}
	}
}

func (h *factsEvalHarness) stepRecall(trajectory *factsEvalTrajectory, step *factsEvalStep, scopeKey string) {
	response, err := h.store.Recall(h.ctx, h.auth(scopeKey), v1alpha1.RecallRequest{
		Query:  step.Query,
		Budget: v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 2000},
	})
	if err != nil {
		h.mismatchf("trajectory %s: baseline recall: %v", trajectory.ID, err)
		return
	}
	h.recallRuns++
	maxRank := step.Want.MaxRank
	if maxRank == 0 {
		maxRank = 8
	}
	for index, fragment := range response.Fragments {
		if fragment.Text == step.Want.ContainsText {
			if index+1 <= maxRank {
				h.recallHits++
			} else {
				h.mismatchf("trajectory %s: baseline recall rank %d exceeds %d", trajectory.ID, index+1, maxRank)
			}
			return
		}
	}
	h.mismatchf("trajectory %s: baseline recall fragments %v do not contain %q", trajectory.ID, response.Fragments, step.Want.ContainsText)
}

func (h *factsEvalHarness) budget(step *factsEvalStep) facts.Budget {
	if step.Budget == nil {
		return facts.Budget{MaxFacts: 8, MaxBytes: 8192}
	}
	return facts.Budget{MaxFacts: step.Budget.MaxFacts, MaxBytes: step.Budget.MaxBytes}
}

func factsEvalFactTexts(list []facts.Fact) []string {
	out := make([]string, 0, len(list))
	for _, fact := range list {
		out = append(out, fact.Text)
	}
	return out
}

func factsEvalParseOptionalTime(t *testing.T, value string) *time.Time {
	t.Helper()
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse fixture time %q: %v", value, err)
	}
	return &parsed
}

// TestFactsLongitudinalEvaluationGate is the deterministic M06 facts gate. It
// always runs the authored blocking trajectories; the machine-expanded
// candidate tiers run when MEMORY_FACTS_EVAL_CANDIDATES=1 and are reported only
// as candidate-tier evidence.
func TestFactsLongitudinalEvaluationGate(t *testing.T) {
	dir := factsEvalFixtureDir
	manifest := loadFactsEvalManifest(t, dir)
	trajectories := loadFactsEvalTrajectories(t, dir)
	expectedTrajectories := 0
	for _, fixture := range manifest.Fixtures {
		if fixture.File == "trajectories.json" {
			expectedTrajectories = fixture.ExpectedTrajectories
		}
	}
	if got := len(trajectories.Trajectories); expectedTrajectories != 0 && got != expectedTrajectories {
		t.Fatalf("blocking trajectory count = %d, manifest expects %d", got, expectedTrajectories)
	}
	if trajectories.ReviewStatus != "authored_not_human_reviewed" {
		t.Fatalf("blocking trajectory review status = %q", trajectories.ReviewStatus)
	}

	started := time.Now().UTC()
	report := newFactsEvalReport(manifest, started)
	report.ManifestHashes = factsEvalManifestHashes(t, dir, manifest)

	c1Pass := 0
	for _, trajectory := range trajectories.Trajectories {
		if trajectory.ReviewStatus != "authored_not_human_reviewed" {
			t.Fatalf("trajectory %s review status = %q", trajectory.ID, trajectory.ReviewStatus)
		}
		harness := newFactsEvalHarness(t, t.TempDir())
		mismatches := harness.runTrajectory(trajectory)
		if err := harness.store.Close(); err != nil {
			t.Fatalf("close trajectory %s store: %v", trajectory.ID, err)
		}
		if mismatches == 0 {
			c1Pass++
		} else {
			t.Errorf("trajectory %s recorded %d facts evaluation mismatches", trajectory.ID, mismatches)
		}
		report.RecallRuns += harness.recallRuns
		report.RecallHits += harness.recallHits
	}

	report.setArm(factsEvalArm{
		ID: "C1-authored", Name: "structured_lifecycle_conformance", Status: "measured",
		Sample: len(trajectories.Trajectories), Passed: c1Pass, Rate: factsEvalRate(c1Pass, len(trajectories.Trajectories)),
		Detail: "authored blocking trajectories on real SQLite: scope/privacy, restart, establish/change/exception/correct/confirm/deny/forget, wrong subject, missing/stale target, untrusted role, batch atomicity, policy deny, anti-resurrection",
	})
	if c1Pass != len(trajectories.Trajectories) {
		t.Fatalf("facts lifecycle conformance failed for %d/%d blocking trajectories", len(trajectories.Trajectories)-c1Pass, len(trajectories.Trajectories))
	}
	if report.RecallRuns != 0 {
		controlRate := float64(report.RecallHits) / float64(report.RecallRuns)
		report.setArm(factsEvalArm{
			ID: "L0-legacy", Name: "in_tree_model_free_recall_control", Status: "measured",
			Sample: report.RecallRuns, Passed: report.RecallHits, Rate: &controlRate,
			Detail: "CONTROL ONLY, NOT exact v0.5.2: the current in-tree Store model-free receipt Recall over the blocking corpus evidence. It isolates whether the legacy receipt path still retrieves the same evidence; it is not a v0.5.2 result.",
		})
		if report.RecallHits != report.RecallRuns {
			t.Fatalf("legacy receipt Recall control hit %d/%d blocking recall steps", report.RecallHits, report.RecallRuns)
		}
	}
	report.setArm(factsEvalArm{
		ID: "B0-v0.5.2", Name: "exact_v0_5_2_checkout_recall", Status: "unrun",
		Reason: "requires an exact v0.5.2 checkout, which this workspace does not exercise; v0.5.2 has no facts/evidence API",
		Detail: "Reproducible method: git archive the v0.5.2 revision into a temporary tree, build its own tests, seed the same authored receipt texts through its Remember path with the same label partition, and run its Recall gate. The in-tree L0-legacy control above is not a substitute for this arm.",
	})
	report.setArm(factsEvalArm{
		ID: "B1-steward", Name: "prior_steward_baseline", Status: "unrun",
		Reason: "requires the prior Steward revision plus a fixed generator/profile/dataset binding; not runnable from the M06 facts harness alone",
		Detail: "Reproducible method: run the same scripted deterministic generator, profile and dataset against the prior Steward revision and the current revision, then compare relevant-context selection.",
	})
	report.setArm(factsEvalArm{
		ID: "C2-steward", Name: "relevant_steward_context", Status: "unrun",
		Reason: "requires a fixed scripted deterministic generator plus the B1 prior-Steward comparison; the facts harness calls no model",
		Detail: "A deterministic scripted generator is acceptable and needs no model tokens, but the result measures structural context selection, not model extraction or paraphrase quality. It must run on the same profile, data and generator as B1-steward.",
	})
	report.setArm(factsEvalArm{
		ID: "HUMAN-GOLD", Name: "human_reviewed_quality", Status: "blocked",
		Reason: manifest.ReleaseBlocker,
	})

	candidates := loadFactsEvalCandidates(t, dir)
	report.Candidates = len(candidates)
	if len(candidates) < 200 {
		t.Fatalf("candidate trajectory expansion = %d, want at least 200", len(candidates))
	}
	if expansion := manifest.CandidateExpansion; expansion.ExpectedCandidates != len(candidates) {
		t.Fatalf("candidate expansion = %d, manifest expects %d", len(candidates), expansion.ExpectedCandidates)
	}
	for _, candidate := range candidates {
		if candidate.ReviewStatus != "not_human_reviewed" {
			t.Fatalf("candidate %s review status = %q", candidate.ID, candidate.ReviewStatus)
		}
	}
	t.Logf("facts evaluation: authored C1=%d/%d clean, candidates=%d (%s), in-tree legacy receipt Recall control=%d/%d",
		c1Pass, len(trajectories.Trajectories), len(candidates), manifest.CandidateExpansion.ReviewStatus, report.RecallHits, report.RecallRuns)

	if os.Getenv(factsEvalCandidatesEnv) == "1" {
		adoption, aliases := runFactsEvalCandidates(t, candidates)
		report.setArm(factsEvalArm{
			ID: "C1-candidate", Name: "structured_adoption_candidates", Status: "measured",
			Sample: adoption.Total, Passed: adoption.Passed, Rate: factsEvalRate(adoption.Passed, adoption.Total),
			Threshold: factsEvalThreshold(manifest, "explicit_preference_min"),
			Detail:    "CANDIDATE TIER ONLY, not human-reviewed: machine-expanded establish -> confirm [-> change] trajectories reach one confirmed current fact whose text equals the latest explicit statement. This is structured adoption/lifecycle conformance, not model extraction or relevance quality.",
		})
		report.setArm(factsEvalArm{
			ID: "C3-candidate", Name: "controlled_alias_recall_at8", Status: "measured",
			Sample: aliases.Total, Passed: aliases.Passed, Rate: factsEvalRate(aliases.Passed, aliases.Total),
			Threshold: factsEvalThreshold(manifest, "recall_at8_alias_min"),
			Detail:    "CANDIDATE TIER ONLY, not human-reviewed: the current fact is returned within the first 8 facts for a controlled alias query from the frozen dictionary. It is not a paraphrase-quality claim and does not exercise arbitrary free-text paraphrase.",
		})
		if adoption.Total != 0 {
			if rate := float64(adoption.Passed) / float64(adoption.Total); rate < factsEvalFrozenExplicitPreferenceMin {
				t.Fatalf("candidate structured adoption rate %.4f is below the frozen %.2f floor", rate, factsEvalFrozenExplicitPreferenceMin)
			}
		}
		if aliases.Total != 0 {
			if rate := float64(aliases.Passed) / float64(aliases.Total); rate < factsEvalFrozenRecallAt8AliasMin {
				t.Fatalf("candidate controlled alias Recall@8 %.4f is below the frozen %.2f floor", rate, factsEvalFrozenRecallAt8AliasMin)
			}
		}
	} else {
		report.setArm(factsEvalArm{
			ID: "C1-candidate", Name: "structured_adoption_candidates", Status: "unrun",
			Threshold: factsEvalThreshold(manifest, "explicit_preference_min"),
			Reason:    fmt.Sprintf("set %s=1 to execute the %d machine-expanded candidate trajectories", factsEvalCandidatesEnv, len(candidates)),
		})
		report.setArm(factsEvalArm{
			ID: "C3-candidate", Name: "controlled_alias_recall_at8", Status: "unrun",
			Threshold: factsEvalThreshold(manifest, "recall_at8_alias_min"),
			Reason:    fmt.Sprintf("set %s=1 to execute the %d machine-expanded candidate trajectories", factsEvalCandidatesEnv, len(candidates)),
		})
	}

	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if path := os.Getenv(factsEvalReportEnv); path != "" {
		if err := writeFactsEvalReport(path, report); err != nil {
			t.Fatalf("write facts evaluation report: %v", err)
		}
		t.Logf("facts evaluation report written to %s", path)
	}
}

func factsEvalManifestHashes(t *testing.T, dir string, manifest factsEvalManifestDoc) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, fixture := range manifest.Fixtures {
		out[fixture.File] = fixture.SHA256
	}
	return out
}

func factsEvalThreshold(manifest factsEvalManifestDoc, name string) *float64 {
	if value, ok := manifest.Thresholds[name]; ok {
		return &value
	}
	return nil
}

func factsEvalRate(passed, total int) *float64 {
	if total == 0 {
		return nil
	}
	rate := float64(passed) / float64(total)
	return &rate
}
