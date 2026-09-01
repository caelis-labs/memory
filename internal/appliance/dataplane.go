package appliance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

type storedReceipt struct {
	id               v1alpha1.ReceiptID
	spaceID          v1alpha1.SpaceID
	text             string
	receivedAt       time.Time
	requestDigest    string
	commitSequence   int64
	consistencyToken v1alpha1.ConsistencyToken
	processingState  v1alpha1.ProcessingState
}

type recallCandidate struct {
	receipt storedReceipt
	class   v1alpha1.SpaceClass
	rank    float64
}

// Remember commits immutable evidence and its lexical projection before
// returning accepted=true.
func (s *Store) Remember(
	ctx context.Context,
	auth v1alpha1.CallAuthorization,
	request v1alpha1.RememberRequest,
) (v1alpha1.RememberResponse, error) {
	if err := contextError(ctx); err != nil {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", false)
	}
	if s.closing.Load() {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeUnavailable, "memoryd is shutting down", true)
	}
	if err := request.Validate(); err != nil {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	digest, err := rememberRequestDigest(request)
	if err != nil {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to normalize request", false)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v1alpha1.RememberResponse{}, s.databaseError("begin Remember", err)
	}
	view, err := s.authorize(ctx, tx, auth, v1alpha1.OperationRemember)
	if err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, err
	}
	if view.writeSpaceID == "" {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "View has no writable Space", false)
	}
	previous, found, err := lookupIdempotentReceipt(ctx, tx, view.writeSpaceID, request.IdempotencyKey)
	if err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.databaseError("read idempotency state", err)
	}
	if found {
		_ = tx.Rollback()
		return s.resolveIdempotent(previous, digest)
	}
	receiptSuffix, err := s.randomHex(16)
	if err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to create receipt identity", false)
	}
	consistencyToken, err := s.randomToken(24)
	if err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to create consistency cursor", false)
	}
	sourceContext, err := json.Marshal(request.SourceContext)
	if err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to normalize source context", false)
	}
	var occurredAt any
	if request.OccurredAt != nil {
		occurredAt = formatTime(*request.OccurredAt)
	}
	receivedAt := s.now().UTC()
	receiptID := v1alpha1.ReceiptID("receipt-" + receiptSuffix)
	result, err := tx.ExecContext(ctx,
		`INSERT INTO receipts(receipt_id, space_id, text, source_context, occurred_at, received_at,
		 idempotency_key, request_digest, consistency_token)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		receiptID, view.writeSpaceID, request.Text, string(sourceContext), occurredAt, formatTime(receivedAt),
		request.IdempotencyKey, digest, consistencyToken)
	if err != nil {
		_ = tx.Rollback()
		return s.resolveRememberInsertFailure(ctx, view.writeSpaceID, request.IdempotencyKey, digest, err)
	}
	commitSequence, err := result.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.databaseError("read Remember sequence", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO receipt_processing(receipt_id, state) VALUES (?, ?)`,
		receiptID, v1alpha1.ProcessingStateAccepted); err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.databaseError("store receipt processing state", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO consistency_cursors(token, generation, space_id, commit_sequence) VALUES (?, ?, ?, ?)`,
		consistencyToken, s.generation, view.writeSpaceID, commitSequence); err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.databaseError("store consistency cursor", err)
	}
	tableName, err := readSpaceIndex(ctx, tx, view.writeSpaceID)
	if err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.databaseError("resolve Space index", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO `+tableName+`(receipt_id, text) VALUES (?, ?)`, receiptID, request.Text); err != nil {
		_ = tx.Rollback()
		return v1alpha1.RememberResponse{}, s.databaseError("index accepted receipt", err)
	}
	if s.faults.BeforeRememberCommit != nil {
		if err := s.faults.BeforeRememberCommit(); err != nil {
			_ = tx.Rollback()
			return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeUnavailable, "Remember could not commit", true)
		}
	}
	if err := tx.Commit(); err != nil {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeUnknownOutcome, "Remember commit outcome is unknown; retry the same effect identity", true)
	}
	if s.faults.AfterRememberCommit != nil {
		if err := s.faults.AfterRememberCommit(); err != nil {
			return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeUnknownOutcome, "Remember committed but the response outcome is unknown; retry the same effect identity", true)
		}
	}
	return v1alpha1.RememberResponse{
		Accepted:         true,
		ReceiptID:        receiptID,
		ConsistencyToken: v1alpha1.ConsistencyToken(consistencyToken),
		ProcessingState:  v1alpha1.ProcessingStateAccepted,
	}, nil
}

func (s *Store) resolveRememberInsertFailure(
	ctx context.Context,
	spaceID v1alpha1.SpaceID,
	key string,
	digest string,
	insertErr error,
) (v1alpha1.RememberResponse, error) {
	previous, found, lookupErr := lookupIdempotentReceipt(ctx, s.db, spaceID, key)
	if lookupErr == nil && found {
		return s.resolveIdempotent(previous, digest)
	}
	return v1alpha1.RememberResponse{}, s.databaseError("store Remember receipt", insertErr)
}

func (s *Store) resolveIdempotent(previous storedReceipt, digest string) (v1alpha1.RememberResponse, error) {
	if previous.requestDigest != digest {
		return v1alpha1.RememberResponse{}, s.serviceError(v1alpha1.ErrorCodeConflict, "idempotency key conflicts with an existing effect", false)
	}
	return v1alpha1.RememberResponse{
		Accepted:          true,
		ReceiptID:         previous.id,
		ConsistencyToken:  previous.consistencyToken,
		DeduplicatedRetry: true,
		ProcessingState:   previous.processingState,
	}, nil
}

func lookupIdempotentReceipt(ctx context.Context, db databaseExecutor, spaceID v1alpha1.SpaceID, key string) (storedReceipt, bool, error) {
	var receipt storedReceipt
	var receivedAt string
	err := db.QueryRowContext(ctx,
		`SELECT r.receipt_id, r.space_id, r.text, r.received_at, r.request_digest,
		 r.commit_sequence, r.consistency_token, p.state
		 FROM receipts r JOIN receipt_processing p ON p.receipt_id = r.receipt_id
		 WHERE r.space_id = ? AND r.idempotency_key = ?`, spaceID, key).Scan(
		&receipt.id, &receipt.spaceID, &receipt.text, &receivedAt, &receipt.requestDigest,
		&receipt.commitSequence, &receipt.consistencyToken, &receipt.processingState)
	if errors.Is(err, sql.ErrNoRows) {
		return storedReceipt{}, false, nil
	}
	if err != nil {
		return storedReceipt{}, false, err
	}
	receipt.receivedAt, err = parseTime(receivedAt)
	return receipt, true, err
}

// Recall queries each authorized Space's independent FTS projection and merges
// only those already-authorized candidate streams.
func (s *Store) Recall(
	ctx context.Context,
	auth v1alpha1.CallAuthorization,
	request v1alpha1.RecallRequest,
) (v1alpha1.RecallResponse, error) {
	if err := contextError(ctx); err != nil {
		return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", false)
	}
	if s.closing.Load() {
		return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeUnavailable, "memoryd is shutting down", true)
	}
	if err := request.Validate(); err != nil {
		return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.Budget.DeadlineMS)*time.Millisecond)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return v1alpha1.RecallResponse{}, s.databaseError("begin Recall", err)
	}
	defer tx.Rollback()
	view, err := s.authorize(ctx, tx, auth, v1alpha1.OperationRecall)
	if err != nil {
		return v1alpha1.RecallResponse{}, err
	}
	if request.MinConsistencyToken != "" {
		var generation string
		var spaceID v1alpha1.SpaceID
		err := tx.QueryRowContext(ctx,
			`SELECT generation, space_id FROM consistency_cursors WHERE token = ?`, request.MinConsistencyToken).Scan(&generation, &spaceID)
		if errors.Is(err, sql.ErrNoRows) || err == nil && generation != s.generation {
			return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeStaleConsistencyToken, "consistency token is stale or unknown", false)
		}
		if err != nil {
			return v1alpha1.RecallResponse{}, s.databaseError("read consistency token", err)
		}
		if !slices.Contains(view.readSpaceIDs, spaceID) {
			return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeUnauthorized, "consistency token Space is not readable", false)
		}
	}
	ftsQuery := lexicalFTSQuery(request.Query)
	candidates := make([]recallCandidate, 0)
	if ftsQuery == "" {
		if err := tx.Commit(); err != nil {
			return v1alpha1.RecallResponse{}, s.databaseError("complete Recall snapshot", err)
		}
		return v1alpha1.RecallResponse{
			Fragments:        []v1alpha1.RecallFragment{},
			ConsistencyToken: request.MinConsistencyToken,
		}, nil
	}
	for _, spaceID := range view.readSpaceIDs {
		if err := contextError(ctx); err != nil {
			return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeDeadline, "request deadline exceeded", true)
		}
		if s.candidateRead != nil {
			s.candidateRead(spaceID)
		}
		tableName, err := readSpaceIndex(ctx, tx, spaceID)
		if err != nil {
			return v1alpha1.RecallResponse{}, s.databaseError("resolve Space index", err)
		}
		class, _, err := readSpaceScope(ctx, tx, spaceID)
		if err != nil {
			return v1alpha1.RecallResponse{}, s.databaseError("read Space class", err)
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT r.receipt_id, r.text, r.received_at, r.request_digest, r.commit_sequence,
			 r.consistency_token, p.state, bm25(`+tableName+`)
			 FROM `+tableName+` f
			 JOIN receipts r ON r.receipt_id = f.receipt_id AND r.space_id = ?
			 JOIN receipt_processing p ON p.receipt_id = r.receipt_id
			 WHERE `+tableName+` MATCH ?
			 ORDER BY bm25(`+tableName+`), r.commit_sequence DESC, r.receipt_id
			 LIMIT 512`, spaceID, ftsQuery)
		if err != nil {
			return v1alpha1.RecallResponse{}, s.databaseError("query Space index", err)
		}
		for rows.Next() {
			var candidate recallCandidate
			var receivedAt string
			candidate.receipt.spaceID = spaceID
			candidate.class = class
			if err := rows.Scan(
				&candidate.receipt.id, &candidate.receipt.text, &receivedAt, &candidate.receipt.requestDigest,
				&candidate.receipt.commitSequence, &candidate.receipt.consistencyToken,
				&candidate.receipt.processingState, &candidate.rank); err != nil {
				_ = rows.Close()
				return v1alpha1.RecallResponse{}, s.databaseError("read Space candidate", err)
			}
			candidate.receipt.receivedAt, err = parseTime(receivedAt)
			if err != nil {
				_ = rows.Close()
				return v1alpha1.RecallResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored receipt time is invalid", false)
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Close(); err != nil {
			return v1alpha1.RecallResponse{}, s.databaseError("close Space candidates", err)
		}
		if err := rows.Err(); err != nil {
			return v1alpha1.RecallResponse{}, s.databaseError("query Space candidates", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return v1alpha1.RecallResponse{}, s.databaseError("complete Recall snapshot", err)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		if candidates[i].receipt.commitSequence != candidates[j].receipt.commitSequence {
			return candidates[i].receipt.commitSequence > candidates[j].receipt.commitSequence
		}
		return candidates[i].receipt.id < candidates[j].receipt.id
	})
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

// GetReceiptStatus reads mutable processing state without exposing receipts in
// Spaces absent from the authorized View.
func (s *Store) GetReceiptStatus(
	ctx context.Context,
	auth v1alpha1.CallAuthorization,
	request v1alpha1.GetReceiptStatusRequest,
) (v1alpha1.ReceiptStatus, error) {
	if request.ReceiptID == "" {
		return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, "receipt_id is required", false)
	}
	if s.closing.Load() {
		return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeUnavailable, "memoryd is shutting down", true)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return v1alpha1.ReceiptStatus{}, s.databaseError("begin receipt status", err)
	}
	defer tx.Rollback()
	view, err := s.authorize(ctx, tx, auth, v1alpha1.OperationReceiptStatus)
	if err != nil {
		return v1alpha1.ReceiptStatus{}, err
	}
	var status v1alpha1.ReceiptStatus
	var spaceID v1alpha1.SpaceID
	var acceptedAt string
	var lastAttempt sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT r.receipt_id, r.space_id, r.received_at, p.state, p.last_attempt_at,
		 p.terminal_error_code, p.semantic_generation
		 FROM receipts r JOIN receipt_processing p ON p.receipt_id = r.receipt_id
		 WHERE r.receipt_id = ?`, request.ReceiptID).Scan(
		&status.ReceiptID, &spaceID, &acceptedAt, &status.State, &lastAttempt,
		&status.TerminalErrorCode, &status.SemanticGeneration); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "receipt not found", false)
		}
		return v1alpha1.ReceiptStatus{}, s.databaseError("read receipt status", err)
	}
	if !slices.Contains(view.readSpaceIDs, spaceID) {
		return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "receipt not found", false)
	}
	status.AcceptedAt, err = parseTime(acceptedAt)
	if err != nil {
		return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored receipt time is invalid", false)
	}
	if lastAttempt.Valid {
		value, err := parseTime(lastAttempt.String)
		if err != nil {
			return v1alpha1.ReceiptStatus{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored processing time is invalid", false)
		}
		status.LastAttemptAt = &value
	}
	if err := tx.Commit(); err != nil {
		return v1alpha1.ReceiptStatus{}, s.databaseError("complete receipt status", err)
	}
	return status, nil
}

func readSpaceIndex(ctx context.Context, db databaseExecutor, spaceID v1alpha1.SpaceID) (string, error) {
	var tableName string
	if err := db.QueryRowContext(ctx,
		`SELECT table_name FROM space_indexes WHERE space_id = ?`, spaceID).Scan(&tableName); err != nil {
		return "", err
	}
	if tableName != spaceIndexTable(spaceID) {
		return "", fmt.Errorf("Space index identity mismatch")
	}
	return tableName, nil
}

func rememberRequestDigest(request v1alpha1.RememberRequest) (string, error) {
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

func lexicalFTSQuery(query string) string {
	terms := strings.Fields(query)
	quoted := make([]string, 0, min(len(terms), 64))
	for _, term := range terms {
		if len(quoted) == 64 {
			break
		}
		if !strings.ContainsFunc(term, func(value rune) bool {
			return unicode.IsLetter(value) || unicode.IsNumber(value)
		}) {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}

func fitProjectedText(existing []v1alpha1.RecallFragment, value string, maxBytes int) (string, bool, bool) {
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

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
