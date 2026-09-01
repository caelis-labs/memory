package reference

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

type Options struct {
	Clock      func() time.Time
	Random     io.Reader
	RecallStep func()
}

type Server struct {
	mu sync.RWMutex

	now        func() time.Time
	random     io.Reader
	recallStep func()

	generation string
	nextID     uint64
	requestID  atomic.Uint64
	sequence   uint64

	realms            map[v1alpha1.RealmID]Realm
	identities        map[v1alpha1.IdentityID]Identity
	spaces            map[v1alpha1.SpaceID]Space
	views             map[v1alpha1.ViewID]ViewDefinition
	grants            map[v1alpha1.GrantID]Grant
	capabilities      map[v1alpha1.CapabilityToken]capabilityState
	issuerCredentials map[string]string

	receipts        map[v1alpha1.ReceiptID]receipt
	receiptsBySpace map[v1alpha1.SpaceID][]v1alpha1.ReceiptID
	idempotency     map[idempotencyIdentity]idempotencyEntry
	tokens          map[v1alpha1.ConsistencyToken]cursor
	candidateReads  map[v1alpha1.SpaceID]*atomic.Uint64
	available       bool
}

type capabilityState struct {
	grantID      v1alpha1.GrantID
	principalRef string
	viewVersion  uint64
	actorRef     string
	audience     v1alpha1.Audience
	operations   []v1alpha1.Operation
	expiresAt    time.Time
}

type receipt struct {
	id               v1alpha1.ReceiptID
	spaceID          v1alpha1.SpaceID
	text             string
	sourceContext    v1alpha1.SourceContext
	occurredAt       *time.Time
	receivedAt       time.Time
	idempotencyKey   string
	requestDigest    string
	commitSequence   uint64
	consistencyToken v1alpha1.ConsistencyToken
	processingState  v1alpha1.ProcessingState
}

type idempotencyEntry struct {
	requestDigest string
	receiptID     v1alpha1.ReceiptID
}

type idempotencyIdentity struct {
	spaceID v1alpha1.SpaceID
	key     string
}

type cursor struct {
	generation string
	spaceID    v1alpha1.SpaceID
	sequence   uint64
}

func New(options Options) (*Server, error) {
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	generationBytes := make([]byte, 16)
	if _, err := io.ReadFull(options.Random, generationBytes); err != nil {
		return nil, fmt.Errorf("create storage generation: %w", err)
	}
	return &Server{
		now:               options.Clock,
		random:            options.Random,
		recallStep:        options.RecallStep,
		generation:        hex.EncodeToString(generationBytes),
		realms:            make(map[v1alpha1.RealmID]Realm),
		identities:        make(map[v1alpha1.IdentityID]Identity),
		spaces:            make(map[v1alpha1.SpaceID]Space),
		views:             make(map[v1alpha1.ViewID]ViewDefinition),
		grants:            make(map[v1alpha1.GrantID]Grant),
		capabilities:      make(map[v1alpha1.CapabilityToken]capabilityState),
		issuerCredentials: make(map[string]string),
		receipts:          make(map[v1alpha1.ReceiptID]receipt),
		receiptsBySpace:   make(map[v1alpha1.SpaceID][]v1alpha1.ReceiptID),
		idempotency:       make(map[idempotencyIdentity]idempotencyEntry),
		tokens:            make(map[v1alpha1.ConsistencyToken]cursor),
		candidateReads:    make(map[v1alpha1.SpaceID]*atomic.Uint64),
		available:         true,
	}, nil
}

func (s *Server) Remember(
	ctx context.Context,
	auth v1alpha1.CallAuthorization,
	request v1alpha1.RememberRequest,
) (v1alpha1.RememberResponse, error) {
	if err := contextError(ctx); err != nil {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", false)
	}
	if !s.isAvailable() {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeUnavailable, "reference service is unavailable", true)
	}
	if err := request.Validate(); err != nil {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	digest, err := requestDigest(request)
	if err != nil {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to normalize request", false)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	view, err := s.authorizeLocked(auth, v1alpha1.OperationRemember)
	if err != nil {
		return v1alpha1.RememberResponse{}, err
	}
	if view.WriteSpaceID == "" {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "View has no writable Space", false)
	}
	effectID := idempotencyIdentity{spaceID: view.WriteSpaceID, key: request.IdempotencyKey}
	if previous, ok := s.idempotency[effectID]; ok {
		if previous.requestDigest != digest {
			return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeConflict, "idempotency key conflicts with an existing effect", false)
		}
		stored := s.receipts[previous.receiptID]
		return rememberResponse(stored, true), nil
	}

	s.sequence++
	receiptID := v1alpha1.ReceiptID(s.nextIdentifierLocked("receipt"))
	token, err := s.randomConsistencyTokenLocked(view.WriteSpaceID, s.sequence)
	if err != nil {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to create consistency cursor", false)
	}
	now := s.now().UTC()
	stored := receipt{
		id:               receiptID,
		spaceID:          view.WriteSpaceID,
		text:             request.Text,
		sourceContext:    cloneSourceContext(request.SourceContext),
		occurredAt:       cloneTime(request.OccurredAt),
		receivedAt:       now,
		idempotencyKey:   request.IdempotencyKey,
		requestDigest:    digest,
		commitSequence:   s.sequence,
		consistencyToken: token,
		processingState:  v1alpha1.ProcessingStateAccepted,
	}
	s.receipts[receiptID] = stored
	s.receiptsBySpace[view.WriteSpaceID] = append(s.receiptsBySpace[view.WriteSpaceID], receiptID)
	s.idempotency[effectID] = idempotencyEntry{
		requestDigest: digest,
		receiptID:     receiptID,
	}
	return rememberResponse(stored, false), nil
}

func (s *Server) Recall(
	ctx context.Context,
	auth v1alpha1.CallAuthorization,
	request v1alpha1.RecallRequest,
) (v1alpha1.RecallResponse, error) {
	if err := contextError(ctx); err != nil {
		return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", false)
	}
	if !s.isAvailable() {
		return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeUnavailable, "reference service is unavailable", true)
	}
	if err := request.Validate(); err != nil {
		return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.Budget.DeadlineMS)*time.Millisecond)
	defer cancel()

	sources, err := s.recallSnapshot(ctx, auth, request.MinConsistencyToken)
	if err != nil {
		return v1alpha1.RecallResponse{}, err
	}

	terms := queryTerms(request.Query)
	candidates := make([]recallCandidate, 0)
	for _, source := range sources {
		if s.recallStep != nil {
			s.recallStep()
		}
		if err := contextError(ctx); err != nil {
			return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", true)
		}
		score := lexicalScore(source.receipt.text, terms)
		if score == 0 {
			continue
		}
		source.score = score
		candidates = append(candidates, source)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].receipt.commitSequence != candidates[j].receipt.commitSequence {
			return candidates[i].receipt.commitSequence > candidates[j].receipt.commitSequence
		}
		return candidates[i].receipt.id < candidates[j].receipt.id
	})
	if err := contextError(ctx); err != nil {
		return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", true)
	}

	response := v1alpha1.RecallResponse{
		Fragments:        make([]v1alpha1.RecallFragment, 0, min(len(candidates), request.Budget.MaxFragments)),
		ConsistencyToken: request.MinConsistencyToken,
	}
	for index, candidate := range candidates {
		if err := contextError(ctx); err != nil {
			return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", true)
		}
		if len(response.Fragments) == request.Budget.MaxFragments {
			response.Truncated = true
			break
		}
		text, truncated, fits := fitProjectedText(response.Fragments, candidate.receipt.text, request.Budget.MaxBytes)
		if !fits {
			response.Truncated = true
			break
		}
		response.Fragments = append(response.Fragments, v1alpha1.RecallFragment{
			FragmentID:   "fragment:" + string(candidate.receipt.id),
			Text:         text,
			EvidenceRefs: []v1alpha1.ReceiptID{candidate.receipt.id},
			SpaceClass:   candidate.class,
		})
		response.Truncated = response.Truncated || truncated || index < len(candidates)-1 && len(response.Fragments) == request.Budget.MaxFragments
		if truncated {
			break
		}
	}
	return response, nil
}

func (s *Server) recallSnapshot(
	ctx context.Context,
	auth v1alpha1.CallAuthorization,
	minConsistencyToken v1alpha1.ConsistencyToken,
) ([]recallCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	view, err := s.authorizeLocked(auth, v1alpha1.OperationRecall)
	if err != nil {
		return nil, err
	}
	if minConsistencyToken != "" {
		cursorValue, ok := s.tokens[minConsistencyToken]
		if !ok || cursorValue.generation != s.generation {
			return nil, s.serviceError(v1alpha1.ErrorCodeStaleConsistencyToken, "consistency token is stale or unknown", false)
		}
		if !slices.Contains(view.ReadSpaceIDs, cursorValue.spaceID) {
			return nil, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "consistency token Space is not readable", false)
		}
	}
	sources := make([]recallCandidate, 0)
	for _, spaceID := range view.ReadSpaceIDs {
		if err := contextError(ctx); err != nil {
			return nil, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", true)
		}
		s.candidateReads[spaceID].Add(1)
		space := s.spaces[spaceID]
		for _, receiptID := range s.receiptsBySpace[spaceID] {
			if err := contextError(ctx); err != nil {
				return nil, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", true)
			}
			sources = append(sources, recallCandidate{
				receipt: s.receipts[receiptID],
				class:   space.Class,
			})
		}
	}
	return sources, nil
}

func (s *Server) GetReceiptStatus(
	ctx context.Context,
	auth v1alpha1.CallAuthorization,
	request v1alpha1.GetReceiptStatusRequest,
) (v1alpha1.ReceiptStatus, error) {
	if err := contextError(ctx); err != nil {
		return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", false)
	}
	if !s.isAvailable() {
		return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeUnavailable, "reference service is unavailable", true)
	}
	if request.ReceiptID == "" {
		return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, "receipt_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	view, err := s.authorizeLocked(auth, v1alpha1.OperationReceiptStatus)
	if err != nil {
		return v1alpha1.ReceiptStatus{}, err
	}
	stored, ok := s.receipts[request.ReceiptID]
	if !ok || !slices.Contains(view.ReadSpaceIDs, stored.spaceID) {
		return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "receipt not found", false)
	}
	return v1alpha1.ReceiptStatus{
		ReceiptID:  stored.id,
		State:      stored.processingState,
		AcceptedAt: stored.receivedAt,
	}, nil
}

// CandidateReadCount reports how often the reference Recall path entered a
// Space. It exists solely to prove pre-candidate authorization in conformance.
func (s *Server) CandidateReadCount(spaceID v1alpha1.SpaceID) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counter := s.candidateReads[spaceID]
	if counter == nil {
		return 0
	}
	return counter.Load()
}

// SetAvailable controls reference-service availability for conformance tests.
// It is not a production lifecycle API.
func (s *Server) SetAvailable(available bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.available = available
}

func (s *Server) isAvailable() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.available
}

func (s *Server) authorizeLocked(
	auth v1alpha1.CallAuthorization,
	operation v1alpha1.Operation,
) (ViewDefinition, error) {
	state, ok := s.capabilities[auth.Capability]
	if !ok || auth.Capability == "" {
		return ViewDefinition{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability is missing or unknown", false)
	}
	grant, ok := s.grants[state.grantID]
	if !ok || grant.Revoked || grant.PrincipalRef != state.principalRef || !grant.ExpiresAt.After(s.now()) || !state.expiresAt.After(s.now()) {
		return ViewDefinition{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability is expired or revoked", false)
	}
	if auth.ActorRef != state.actorRef || auth.ActorRef != grant.ActorRef {
		return ViewDefinition{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability actor mismatch", false)
	}
	if auth.Audience != state.audience || !containsAudience(grant.AllowedAudiences, auth.Audience) {
		return ViewDefinition{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability audience mismatch", false)
	}
	if !containsOperation(state.operations, operation) || !containsOperation(grant.AllowedOperations, operation) {
		return ViewDefinition{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability operation mismatch", false)
	}
	view, ok := s.views[grant.ViewRef]
	if !ok || view.Version != state.viewVersion {
		return ViewDefinition{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "capability View is stale", false)
	}
	if auth.Audience == v1alpha1.AudienceShared && view.MaxDisclosureClass == v1alpha1.SpaceClassPrivate {
		return ViewDefinition{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "private View is incompatible with shared audience", false)
	}
	if operation == v1alpha1.OperationRemember && auth.Audience == v1alpha1.AudiencePrivate {
		writeSpace, ok := s.spaces[view.WriteSpaceID]
		if !ok || writeSpace.Class != v1alpha1.SpaceClassPrivate {
			return ViewDefinition{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "private Runtime cannot write shared memory", false)
		}
	}
	return cloneView(view), nil
}

type recallCandidate struct {
	receipt receipt
	class   v1alpha1.SpaceClass
	score   int
}

func rememberResponse(stored receipt, deduplicated bool) v1alpha1.RememberResponse {
	return v1alpha1.RememberResponse{
		Accepted:          true,
		ReceiptID:         stored.id,
		ConsistencyToken:  stored.consistencyToken,
		DeduplicatedRetry: deduplicated,
		ProcessingState:   stored.processingState,
	}
}

func requestDigest(request v1alpha1.RememberRequest) (string, error) {
	value := struct {
		Text          string                 `json:"text"`
		SourceContext v1alpha1.SourceContext `json:"source_context"`
		OccurredAt    string                 `json:"occurred_at,omitempty"`
	}{
		Text:          request.Text,
		SourceContext: request.SourceContext,
	}
	if request.OccurredAt != nil {
		value.OccurredAt = request.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(normalized)
	return hex.EncodeToString(digest[:]), nil
}

func queryTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	if len(fields) == 0 {
		return []string{strings.ToLower(query)}
	}
	return fields
}

func lexicalScore(text string, terms []string) int {
	lower := strings.ToLower(text)
	score := 0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score++
		}
	}
	return score
}

func fitProjectedText(
	existing []v1alpha1.RecallFragment,
	value string,
	maxBytes int,
) (string, bool, bool) {
	if projectedRecallSize(existing, value) <= maxBytes {
		return value, false, true
	}
	runes := []rune(value)
	low, high := 0, len(runes)
	best := ""
	for low <= high {
		middle := low + (high-low)/2
		candidate := string(runes[:middle])
		if projectedRecallSize(existing, candidate) <= maxBytes {
			best = candidate
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	if best == "" {
		return "", true, false
	}
	return best, true, true
}

func projectedRecallSize(existing []v1alpha1.RecallFragment, next string) int {
	fragments := make([]string, 0, len(existing)+1)
	for _, fragment := range existing {
		fragments = append(fragments, fragment.Text)
	}
	if next != "" {
		fragments = append(fragments, next)
	}
	encoded, err := json.Marshal(struct {
		Fragments []string `json:"fragments"`
	}{Fragments: fragments})
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return len(encoded)
}

func (s *Server) nextIdentifierLocked(prefix string) string {
	s.nextID++
	return fmt.Sprintf("%s-%08d", prefix, s.nextID)
}

func (s *Server) randomToken() (v1alpha1.CapabilityToken, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", fmt.Errorf("create capability: %w", err)
	}
	return v1alpha1.CapabilityToken(base64.RawURLEncoding.EncodeToString(raw)), nil
}

func (s *Server) randomConsistencyTokenLocked(spaceID v1alpha1.SpaceID, sequence uint64) (v1alpha1.ConsistencyToken, error) {
	raw := make([]byte, 24)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", err
	}
	token := v1alpha1.ConsistencyToken(base64.RawURLEncoding.EncodeToString(raw))
	s.tokens[token] = cursor{generation: s.generation, spaceID: spaceID, sequence: sequence}
	return token, nil
}

func (s *Server) serviceError(code v1alpha1.ErrorCode, message string, retryable bool) error {
	requestID := s.requestID.Add(1)
	return &v1alpha1.ServiceError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		RequestID: fmt.Sprintf("request-%08d", requestID),
	}
}

func cloneSourceContext(source v1alpha1.SourceContext) v1alpha1.SourceContext {
	if source.ExtensionLabels != nil {
		source.ExtensionLabels = make(map[string]string, len(source.ExtensionLabels))
		for key, value := range source.ExtensionLabels {
			source.ExtensionLabels[key] = value
		}
	}
	return source
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
