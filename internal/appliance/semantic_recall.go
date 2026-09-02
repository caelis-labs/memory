package appliance

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strconv"
	"strings"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// semanticRecallCandidates is deliberately best-effort after authorization.
// Any semantic-only fault discards that Space's derived candidates and marks
// the successful baseline response degraded. Context expiry remains an error.
func (s *Store) semanticRecallCandidates(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	class v1alpha1.SpaceClass,
	query string,
	ftsQuery string,
	privateTerms []string,
) ([]recallCandidate, bool, error) {
	if err := contextError(ctx); err != nil {
		return nil, false, err
	}
	degraded := false
	var outstanding bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS(
		 SELECT 1 FROM steward_jobs
		 WHERE space_id = ? AND (
		  state IN ('pending', 'leased') OR
		  (state = 'failed' AND terminal_error_code NOT IN ('steward_disabled', 'receipt_corrected', 'receipt_deleted'))
		 )
		)`, spaceID).Scan(&outstanding); err != nil {
		if contextError(ctx) != nil {
			return nil, false, ctx.Err()
		}
		degraded = true
	} else {
		degraded = outstanding
	}
	tableName, err := readSemanticSpaceIndex(ctx, db, spaceID)
	if err != nil {
		if contextError(ctx) != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, nil
	}
	var expected, projected, usable int64
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_records WHERE space_id = ? AND status = 'active'`, spaceID).Scan(&expected); err != nil {
		degraded = true
	} else if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableName).Scan(&projected); err != nil {
		degraded = true
	} else if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+tableName+` f
		 JOIN semantic_records r ON r.record_id = f.record_id AND r.space_id = ?
		 WHERE r.status = 'active' AND r.current_revision = f.revision`, spaceID).Scan(&usable); err != nil {
		degraded = true
	} else if expected != projected || expected != usable {
		degraded = true
	}
	if contextError(ctx) != nil {
		return nil, false, ctx.Err()
	}
	rows, err := db.QueryContext(ctx,
		`SELECT r.record_id, r.current_revision, v.text, r.updated_at,
		 bm25(`+tableName+`, 0.0, 0.0, 4.0, 2.0, 0.25)
		 FROM `+tableName+` f
		 JOIN semantic_records r ON r.record_id = f.record_id AND r.space_id = ?
		 JOIN semantic_revisions v ON v.record_id = r.record_id AND v.revision = r.current_revision
		 WHERE `+tableName+` MATCH ? AND r.status = 'active' AND r.current_revision = f.revision
		 ORDER BY bm25(`+tableName+`, 0.0, 0.0, 4.0, 2.0, 0.25), r.updated_at DESC, r.record_id
		 LIMIT 512`, spaceID, ftsQuery)
	if err != nil {
		if contextError(ctx) != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, nil
	}
	var candidates []recallCandidate
	for rows.Next() {
		var candidate recallCandidate
		var recordID stewardv1alpha1.RecordID
		var revision uint64
		var updatedAt string
		var bm25Rank float64
		if err := rows.Scan(&recordID, &revision, &candidate.text, &updatedAt, &bm25Rank); err != nil {
			_ = rows.Close()
			return nil, true, nil
		}
		candidate.rank, err = lexicalRank(query, candidate.text, privateTerms, bm25Rank)
		if err != nil {
			_ = rows.Close()
			return nil, true, nil
		}
		candidate.observedAt, err = parseTime(updatedAt)
		if err != nil {
			_ = rows.Close()
			return nil, true, nil
		}
		candidate.fragmentID = "fragment:record:" + string(recordID) + ":" + strconv.FormatUint(revision, 10)
		candidate.recordRefs = []string{string(recordID)}
		candidate.recordRevision = revision
		candidate.class = class
		candidate.semantic = true
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		if contextError(ctx) != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, nil
	}
	if err := rows.Close(); err != nil {
		return nil, true, nil
	}
	validCandidates := candidates[:0]
	for _, candidate := range candidates {
		recordID := stewardv1alpha1.RecordID(candidate.recordRefs[0])
		evidence, valid, err := readRecallSemanticEvidence(ctx, db, recordID, candidate.recordRevision, spaceID)
		if err != nil {
			if contextError(ctx) != nil {
				return nil, false, ctx.Err()
			}
			degraded = true
			continue
		}
		if !valid {
			degraded = true
			continue
		}
		candidate.evidenceRefs = evidence
		validCandidates = append(validCandidates, candidate)
	}
	return validCandidates, degraded, nil
}

func readRecallSemanticEvidence(
	ctx context.Context,
	db databaseExecutor,
	recordID stewardv1alpha1.RecordID,
	revision uint64,
	spaceID v1alpha1.SpaceID,
) ([]v1alpha1.ReceiptID, bool, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT e.receipt_id, r.space_id, c.original_receipt_id
		 FROM semantic_evidence e
		 LEFT JOIN receipts r ON r.receipt_id = e.receipt_id
		 LEFT JOIN receipt_corrections c ON c.original_receipt_id = e.receipt_id
		 WHERE e.record_id = ? AND e.revision = ?
		 ORDER BY e.ordinal`, recordID, revision)
	if err != nil {
		return nil, false, err
	}
	var evidence []v1alpha1.ReceiptID
	valid := true
	for rows.Next() {
		var receiptID v1alpha1.ReceiptID
		var receiptSpace, corrected sql.NullString
		if err := rows.Scan(&receiptID, &receiptSpace, &corrected); err != nil {
			_ = rows.Close()
			return nil, false, err
		}
		if !receiptSpace.Valid || v1alpha1.SpaceID(receiptSpace.String) != spaceID || corrected.Valid {
			valid = false
		}
		evidence = append(evidence, receiptID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, false, err
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}
	return evidence, valid && len(evidence) > 0, nil
}

func mergeRecallCandidates(candidates []recallCandidate) []recallCandidate {
	merged := make([]recallCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := normalizedRecallText(candidate.text)
		target := -1
		for index := range merged {
			if normalizedRecallText(merged[index].text) != key {
				continue
			}
			if candidate.semantic && merged[index].semantic ||
				candidate.semantic != merged[index].semantic && recallCandidatesShareEvidence(candidate, merged[index]) {
				target = index
				break
			}
		}
		if target >= 0 {
			index := target
			current := &merged[index]
			current.evidenceRefs = appendUniqueReceipts(current.evidenceRefs, candidate.evidenceRefs...)
			current.recordRefs = appendUniqueStrings(current.recordRefs, candidate.recordRefs...)
			if current.class == v1alpha1.SpaceClassShared && candidate.class == v1alpha1.SpaceClassPrivate {
				current.class = v1alpha1.SpaceClassPrivate
			}
			if current.semantic && !candidate.semantic {
				current.fragmentID = candidate.fragmentID
				current.text = candidate.text
				current.semantic = false
			}
			continue
		}
		candidate.evidenceRefs = appendUniqueReceipts(nil, candidate.evidenceRefs...)
		candidate.recordRefs = appendUniqueStrings(nil, candidate.recordRefs...)
		merged = append(merged, candidate)
	}
	for index := range merged {
		sort.Slice(merged[index].evidenceRefs, func(i, j int) bool {
			return merged[index].evidenceRefs[i] < merged[index].evidenceRefs[j]
		})
		sort.Strings(merged[index].recordRefs)
	}
	return merged
}

func recallCandidatesShareEvidence(first, second recallCandidate) bool {
	for _, receiptID := range first.evidenceRefs {
		if slices.Contains(second.evidenceRefs, receiptID) {
			return true
		}
	}
	return false
}

func normalizedRecallText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func appendUniqueReceipts(existing []v1alpha1.ReceiptID, values ...v1alpha1.ReceiptID) []v1alpha1.ReceiptID {
	for _, value := range values {
		if value != "" && !slices.Contains(existing, value) {
			existing = append(existing, value)
		}
	}
	return existing
}

func appendUniqueStrings(existing []string, values ...string) []string {
	for _, value := range values {
		if value != "" && !slices.Contains(existing, value) {
			existing = append(existing, value)
		}
	}
	return existing
}
