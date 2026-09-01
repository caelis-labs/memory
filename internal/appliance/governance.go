package appliance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const sessionCopyBoundary = "Caelis Session history must be deleted or redacted separately"

type receiptSearchCandidate struct {
	receipt managementv1alpha1.Receipt
	rank    float64
}

// SearchReceipts performs owner-authorized lexical search across one or all
// Spaces. Corrected originals are excluded unless explicitly requested.
func (s *Store) SearchReceipts(
	ctx context.Context,
	request managementv1alpha1.SearchReceiptsRequest,
) (managementv1alpha1.SearchReceiptsResponse, error) {
	if err := request.Validate(); err != nil {
		return managementv1alpha1.SearchReceiptsResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	ftsQuery := lexicalFTSQuery(request.Query)
	if ftsQuery == "" {
		return managementv1alpha1.SearchReceiptsResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, "query has no searchable terms", false)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return managementv1alpha1.SearchReceiptsResponse{}, s.databaseError("begin management receipt search", err)
	}
	defer tx.Rollback()
	indexes, err := managementSearchIndexes(ctx, tx, request.SpaceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return managementv1alpha1.SearchReceiptsResponse{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "Space not found", false)
		}
		return managementv1alpha1.SearchReceiptsResponse{}, s.databaseError("resolve management search Spaces", err)
	}
	candidates := make([]receiptSearchCandidate, 0)
	for _, tableName := range indexes {
		activePredicate := ""
		if !request.IncludeCorrected {
			activePredicate = ` AND NOT EXISTS (
				SELECT 1 FROM receipt_corrections c WHERE c.original_receipt_id = r.receipt_id
			)`
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT r.receipt_id, r.space_id, r.text, r.source_context, r.occurred_at,
			 r.received_at, r.commit_sequence, p.state,
			 COALESCE((SELECT replacement_receipt_id FROM receipt_corrections WHERE original_receipt_id = r.receipt_id), ''),
			 COALESCE((SELECT original_receipt_id FROM receipt_corrections WHERE replacement_receipt_id = r.receipt_id), ''),
			 bm25(`+tableName+`)
			 FROM `+tableName+` f
			 JOIN receipts r ON r.receipt_id = f.receipt_id
			 JOIN receipt_processing p ON p.receipt_id = r.receipt_id
			 WHERE `+tableName+` MATCH ?`+activePredicate+`
			 ORDER BY bm25(`+tableName+`), r.commit_sequence DESC, r.receipt_id
			 LIMIT ?`, ftsQuery, request.Limit+1)
		if err != nil {
			return managementv1alpha1.SearchReceiptsResponse{}, s.databaseError("query management receipt search", err)
		}
		for rows.Next() {
			var candidate receiptSearchCandidate
			if err := scanManagementReceipt(rows, &candidate.receipt, &candidate.rank); err != nil {
				_ = rows.Close()
				return managementv1alpha1.SearchReceiptsResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored receipt metadata is invalid", false)
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Close(); err != nil {
			return managementv1alpha1.SearchReceiptsResponse{}, s.databaseError("close management receipt search", err)
		}
		if err := rows.Err(); err != nil {
			return managementv1alpha1.SearchReceiptsResponse{}, s.databaseError("query management receipt search", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return managementv1alpha1.SearchReceiptsResponse{}, s.databaseError("complete management receipt search", err)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		if candidates[i].receipt.CommitSequence != candidates[j].receipt.CommitSequence {
			return candidates[i].receipt.CommitSequence > candidates[j].receipt.CommitSequence
		}
		return candidates[i].receipt.ReceiptID < candidates[j].receipt.ReceiptID
	})
	response := managementv1alpha1.SearchReceiptsResponse{Receipts: make([]managementv1alpha1.Receipt, 0)}
	if len(candidates) > request.Limit {
		response.Truncated = true
		candidates = candidates[:request.Limit]
	}
	for _, candidate := range candidates {
		response.Receipts = append(response.Receipts, candidate.receipt)
	}
	return response, nil
}

// TraceReceipt returns active evidence, its correction links, or a content-free
// deletion tombstone.
func (s *Store) TraceReceipt(
	ctx context.Context,
	request managementv1alpha1.TraceReceiptRequest,
) (managementv1alpha1.TraceReceiptResponse, error) {
	if err := request.Validate(); err != nil {
		return managementv1alpha1.TraceReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	receipt, err := s.readManagementReceipt(ctx, request.ReceiptID)
	if err == nil {
		state := managementv1alpha1.ReceiptStateActive
		if receipt.CorrectedBy != "" {
			state = managementv1alpha1.ReceiptStateCorrected
		}
		return managementv1alpha1.TraceReceiptResponse{State: state, Receipt: &receipt}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return managementv1alpha1.TraceReceiptResponse{}, s.databaseError("trace receipt", err)
	}
	tombstone, err := s.readTombstone(ctx, request.ReceiptID)
	if errors.Is(err, sql.ErrNoRows) {
		return managementv1alpha1.TraceReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "receipt not found", false)
	}
	if err != nil {
		return managementv1alpha1.TraceReceiptResponse{}, s.databaseError("trace receipt tombstone", err)
	}
	return managementv1alpha1.TraceReceiptResponse{State: managementv1alpha1.ReceiptStateDeleted, Tombstone: &tombstone}, nil
}

// CorrectReceipt appends same-Space replacement evidence and removes the
// original only from the disposable Recall projection.
func (s *Store) CorrectReceipt(
	ctx context.Context,
	request managementv1alpha1.CorrectReceiptRequest,
) (managementv1alpha1.CorrectReceiptResponse, error) {
	if err := s.requireMutableGeneration(); err != nil {
		return managementv1alpha1.CorrectReceiptResponse{}, err
	}
	if err := request.Validate(); err != nil {
		return managementv1alpha1.CorrectReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	digest, err := managementRequestDigest(request)
	if err != nil {
		return managementv1alpha1.CorrectReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to normalize correction", false)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return managementv1alpha1.CorrectReceiptResponse{}, s.databaseError("begin receipt correction", err)
	}
	rollback := func(err error) (managementv1alpha1.CorrectReceiptResponse, error) {
		_ = tx.Rollback()
		return managementv1alpha1.CorrectReceiptResponse{}, err
	}
	var replay managementv1alpha1.CorrectReceiptResponse
	found, err := s.lookupManagementEffect(ctx, tx, "correct_receipt", request.IdempotencyKey, digest, &replay)
	if err != nil {
		return rollback(err)
	}
	if found {
		_ = tx.Rollback()
		replay.DeduplicatedRetry = true
		return replay, nil
	}
	var spaceID v1alpha1.SpaceID
	if err := tx.QueryRowContext(ctx, `SELECT space_id FROM receipts WHERE receipt_id = ?`, request.ReceiptID).Scan(&spaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rollback(s.serviceError(v1alpha1.ErrorCodeNotFound, "receipt not found", false))
		}
		return rollback(s.databaseError("read receipt for correction", err))
	}
	var alreadyCorrected bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM receipt_corrections WHERE original_receipt_id = ?)`, request.ReceiptID).Scan(&alreadyCorrected); err != nil {
		return rollback(s.databaseError("read existing correction", err))
	}
	if alreadyCorrected {
		return rollback(s.serviceError(v1alpha1.ErrorCodeConflict, "receipt already has a replacement", false))
	}
	if err := s.invalidateSemanticRecordsForReceipt(ctx, tx, request.ReceiptID, "receipt_corrected"); err != nil {
		return rollback(s.databaseError("invalidate corrected semantic Records", err))
	}
	receiptSuffix, err := s.randomHex(16)
	if err != nil {
		return rollback(s.serviceError(v1alpha1.ErrorCodeInternal, "failed to create replacement receipt identity", false))
	}
	consistencyToken, err := s.randomToken(24)
	if err != nil {
		return rollback(s.serviceError(v1alpha1.ErrorCodeInternal, "failed to create correction cursor", false))
	}
	replacementID := v1alpha1.ReceiptID("receipt-" + receiptSuffix)
	sourceContext := v1alpha1.SourceContext{
		SourceType:      "management_correction",
		ExtensionLabels: map[string]string{"corrects_receipt": string(request.ReceiptID)},
	}
	sourceJSON, err := json.Marshal(sourceContext)
	if err != nil {
		return rollback(s.serviceError(v1alpha1.ErrorCodeInternal, "failed to normalize correction source", false))
	}
	receiptRequest := v1alpha1.RememberRequest{
		Text: request.ReplacementText, SourceContext: sourceContext,
		IdempotencyKey: "management:correction:" + digestString(request.IdempotencyKey)[:32],
	}
	receiptDigest, err := rememberRequestDigest(receiptRequest)
	if err != nil {
		return rollback(s.serviceError(v1alpha1.ErrorCodeInternal, "failed to normalize replacement receipt", false))
	}
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx,
		`INSERT INTO receipts(receipt_id, space_id, text, source_context, occurred_at, received_at,
		 idempotency_key, request_digest, consistency_token)
		 VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?)`,
		replacementID, spaceID, request.ReplacementText, string(sourceJSON), formatTime(now),
		receiptRequest.IdempotencyKey, receiptDigest, consistencyToken)
	if err != nil {
		return rollback(s.databaseError("store replacement receipt", err))
	}
	commitSequence, err := result.LastInsertId()
	if err != nil {
		return rollback(s.databaseError("read replacement receipt sequence", err))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO receipt_processing(receipt_id, state) VALUES (?, ?)`, replacementID, v1alpha1.ProcessingStateAccepted); err != nil {
		return rollback(s.databaseError("store replacement processing state", err))
	}
	if err := s.enqueueStewardJob(ctx, tx, replacementID, spaceID, now); err != nil {
		return rollback(s.databaseError("enqueue correction Steward job", err))
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO consistency_cursors(token, generation, space_id, commit_sequence) VALUES (?, ?, ?, ?)`,
		consistencyToken, s.generation, spaceID, commitSequence); err != nil {
		return rollback(s.databaseError("store correction cursor", err))
	}
	tableName, err := readSpaceIndex(ctx, tx, spaceID)
	if err != nil {
		return rollback(s.databaseError("resolve correction Space index", err))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+tableName+`(receipt_id, text) VALUES (?, ?)`, replacementID, request.ReplacementText); err != nil {
		return rollback(s.databaseError("index replacement receipt", err))
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO receipt_corrections(original_receipt_id, replacement_receipt_id, space_id, created_at, reason)
		 VALUES (?, ?, ?, ?, ?)`, request.ReceiptID, replacementID, spaceID, formatTime(now), request.Reason); err != nil {
		return rollback(s.databaseError("record receipt correction", err))
	}
	response := managementv1alpha1.CorrectReceiptResponse{
		OriginalReceiptID: request.ReceiptID, ReplacementReceiptID: replacementID,
		ConsistencyToken: v1alpha1.ConsistencyToken(consistencyToken),
	}
	if err := s.storeManagementEffect(ctx, tx, "correct_receipt", request.IdempotencyKey, digest, response, now); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return managementv1alpha1.CorrectReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeUnknownOutcome, "correction commit outcome is unknown; retry the same effect identity", true)
	}
	return response, nil
}

// DeleteReceipt removes receipt content and its processing/projection state in
// one transaction after first recording a content-free tombstone.
func (s *Store) DeleteReceipt(
	ctx context.Context,
	request managementv1alpha1.DeleteReceiptRequest,
) (managementv1alpha1.DeleteReceiptResponse, error) {
	if err := s.requireMutableGeneration(); err != nil {
		return managementv1alpha1.DeleteReceiptResponse{}, err
	}
	if err := request.Validate(); err != nil {
		return managementv1alpha1.DeleteReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	digest, err := managementRequestDigest(request)
	if err != nil {
		return managementv1alpha1.DeleteReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeInternal, "failed to normalize deletion", false)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return managementv1alpha1.DeleteReceiptResponse{}, s.databaseError("begin receipt deletion", err)
	}
	rollback := func(err error) (managementv1alpha1.DeleteReceiptResponse, error) {
		_ = tx.Rollback()
		return managementv1alpha1.DeleteReceiptResponse{}, err
	}
	var replay managementv1alpha1.DeleteReceiptResponse
	found, err := s.lookupManagementEffect(ctx, tx, "delete_receipt", request.IdempotencyKey, digest, &replay)
	if err != nil {
		return rollback(err)
	}
	if found {
		_ = tx.Rollback()
		replay.DeduplicatedRetry = true
		return replay, nil
	}
	var spaceID v1alpha1.SpaceID
	var receiptKey, receiptDigest string
	err = tx.QueryRowContext(ctx,
		`SELECT space_id, idempotency_key, request_digest FROM receipts WHERE receipt_id = ?`, request.ReceiptID).Scan(
		&spaceID, &receiptKey, &receiptDigest)
	if errors.Is(err, sql.ErrNoRows) {
		tombstone, tombstoneErr := readTombstoneWith(ctx, tx, request.ReceiptID)
		if errors.Is(tombstoneErr, sql.ErrNoRows) {
			return rollback(s.serviceError(v1alpha1.ErrorCodeNotFound, "receipt not found", false))
		}
		if tombstoneErr != nil {
			return rollback(s.databaseError("read prior receipt deletion", tombstoneErr))
		}
		response := managementv1alpha1.DeleteReceiptResponse{
			Deleted: true, ReceiptID: request.ReceiptID, TombstoneID: tombstone.TombstoneID,
			DeduplicatedRetry: true, SessionCopyBoundary: sessionCopyBoundary,
		}
		if err := s.storeManagementEffect(ctx, tx, "delete_receipt", request.IdempotencyKey, digest, response, s.now().UTC()); err != nil {
			return rollback(err)
		}
		if err := tx.Commit(); err != nil {
			return managementv1alpha1.DeleteReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeUnknownOutcome, "deletion commit outcome is unknown; retry the same effect identity", true)
		}
		return response, nil
	}
	if err != nil {
		return rollback(s.databaseError("read receipt for deletion", err))
	}
	tombstoneSuffix, err := s.randomHex(16)
	if err != nil {
		return rollback(s.serviceError(v1alpha1.ErrorCodeInternal, "failed to create tombstone identity", false))
	}
	tombstoneID := "tombstone-" + tombstoneSuffix
	now := s.now().UTC()
	if err := s.invalidateSemanticRecordsForReceipt(ctx, tx, request.ReceiptID, "receipt_deleted"); err != nil {
		return rollback(s.databaseError("invalidate deleted semantic Records", err))
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO receipt_tombstones(tombstone_id, receipt_id, space_id, idempotency_key, request_digest, deleted_at, reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tombstoneID, request.ReceiptID, spaceID, receiptKey, receiptDigest, formatTime(now), request.Reason); err != nil {
		return rollback(s.databaseError("store receipt tombstone", err))
	}
	tableName, err := readSpaceIndex(ctx, tx, spaceID)
	if err != nil {
		return rollback(s.databaseError("resolve deletion Space index", err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableName+` WHERE receipt_id = ?`, request.ReceiptID); err != nil {
		return rollback(s.databaseError("remove receipt projection", err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM receipt_processing WHERE receipt_id = ?`, request.ReceiptID); err != nil {
		return rollback(s.databaseError("remove receipt processing state", err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM receipts WHERE receipt_id = ?`, request.ReceiptID); err != nil {
		return rollback(s.databaseError("remove receipt content", err))
	}
	response := managementv1alpha1.DeleteReceiptResponse{
		Deleted: true, ReceiptID: request.ReceiptID, TombstoneID: tombstoneID,
		SessionCopyBoundary: sessionCopyBoundary,
	}
	if err := s.storeManagementEffect(ctx, tx, "delete_receipt", request.IdempotencyKey, digest, response, now); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return managementv1alpha1.DeleteReceiptResponse{}, s.serviceError(v1alpha1.ErrorCodeUnknownOutcome, "deletion commit outcome is unknown; retry the same effect identity", true)
	}
	return response, nil
}

func managementSearchIndexes(ctx context.Context, tx *sql.Tx, spaceID v1alpha1.SpaceID) ([]string, error) {
	if spaceID != "" {
		name, err := readSpaceIndex(ctx, tx, spaceID)
		if err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT table_name FROM space_indexes ORDER BY space_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		indexes = append(indexes, name)
	}
	return indexes, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanManagementReceipt(scanner rowScanner, receipt *managementv1alpha1.Receipt, trailing ...any) error {
	var sourceJSON, receivedAt string
	var occurred sql.NullString
	values := []any{
		&receipt.ReceiptID, &receipt.SpaceID, &receipt.Text, &sourceJSON, &occurred,
		&receivedAt, &receipt.CommitSequence, &receipt.ProcessingState,
		&receipt.CorrectedBy, &receipt.CorrectionOf,
	}
	values = append(values, trailing...)
	if err := scanner.Scan(values...); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(sourceJSON), &receipt.SourceContext); err != nil {
		return err
	}
	parsed, err := parseTime(receivedAt)
	if err != nil {
		return err
	}
	receipt.ReceivedAt = parsed
	if occurred.Valid {
		parsed, err := parseTime(occurred.String)
		if err != nil {
			return err
		}
		receipt.OccurredAt = &parsed
	}
	return nil
}

func (s *Store) readManagementReceipt(ctx context.Context, receiptID v1alpha1.ReceiptID) (managementv1alpha1.Receipt, error) {
	var receipt managementv1alpha1.Receipt
	err := scanManagementReceipt(s.db.QueryRowContext(ctx,
		`SELECT r.receipt_id, r.space_id, r.text, r.source_context, r.occurred_at,
		 r.received_at, r.commit_sequence, p.state,
		 COALESCE((SELECT replacement_receipt_id FROM receipt_corrections WHERE original_receipt_id = r.receipt_id), ''),
		 COALESCE((SELECT original_receipt_id FROM receipt_corrections WHERE replacement_receipt_id = r.receipt_id), '')
		 FROM receipts r JOIN receipt_processing p ON p.receipt_id = r.receipt_id
		 WHERE r.receipt_id = ?`, receiptID), &receipt)
	return receipt, err
}

func (s *Store) readTombstone(ctx context.Context, receiptID v1alpha1.ReceiptID) (managementv1alpha1.Tombstone, error) {
	return readTombstoneWith(ctx, s.db, receiptID)
}

func readTombstoneWith(ctx context.Context, db databaseExecutor, receiptID v1alpha1.ReceiptID) (managementv1alpha1.Tombstone, error) {
	var tombstone managementv1alpha1.Tombstone
	var deletedAt string
	err := db.QueryRowContext(ctx,
		`SELECT tombstone_id, receipt_id, space_id, deleted_at, reason
		 FROM receipt_tombstones WHERE receipt_id = ?`, receiptID).Scan(
		&tombstone.TombstoneID, &tombstone.ReceiptID, &tombstone.SpaceID, &deletedAt, &tombstone.Reason)
	if err != nil {
		return managementv1alpha1.Tombstone{}, err
	}
	tombstone.DeletedAt, err = parseTime(deletedAt)
	return tombstone, err
}

func managementRequestDigest(request any) (string, error) {
	normalized, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(normalized)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Store) lookupManagementEffect(
	ctx context.Context,
	db databaseExecutor,
	operation, key, digest string,
	output any,
) (bool, error) {
	var storedDigest, resultJSON string
	err := db.QueryRowContext(ctx,
		`SELECT request_digest, result_json FROM management_effects WHERE operation = ? AND idempotency_key = ?`,
		operation, key).Scan(&storedDigest, &resultJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, s.databaseError("read management idempotency state", err)
	}
	if storedDigest != digest {
		return false, s.serviceError(v1alpha1.ErrorCodeConflict, "management idempotency key conflicts with an existing effect", false)
	}
	if err := json.Unmarshal([]byte(resultJSON), output); err != nil {
		return false, s.serviceError(v1alpha1.ErrorCodeInternal, "stored management result is invalid", false)
	}
	return true, nil
}

func (s *Store) storeManagementEffect(
	ctx context.Context,
	db databaseExecutor,
	operation, key, digest string,
	result any,
	createdAt time.Time,
) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return s.serviceError(v1alpha1.ErrorCodeInternal, "failed to encode management result", false)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO management_effects(operation, idempotency_key, request_digest, result_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`, operation, key, digest, string(encoded), formatTime(createdAt)); err != nil {
		return s.databaseError("store management idempotency state", err)
	}
	return nil
}
