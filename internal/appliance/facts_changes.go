package appliance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// factServiceError preserves versioned semantic errors and prevents SQL/parser
// details leaking through the public facts boundary. Callers can safely retry
// transient storage/deadline failures with the same source identity.
func (s *Store) factServiceError(err error) error {
	if err == nil {
		return nil
	}
	var service *memory.ServiceError
	if errors.As(err, &service) {
		return err
	}
	return s.databaseError("fact storage is unavailable", err)
}

func (s *Store) recordMemoryChange(ctx context.Context, tx *sql.Tx, spaceID memory.SpaceID, labelDigest, kind string, receiptID memory.ReceiptID, recordID string) (uint64, error) {
	r, err := tx.ExecContext(ctx, `INSERT INTO memory_changes(space_id,label_set_digest,kind,receipt_id,record_id,changed_at) VALUES(?,?,?,?,?,?)`, spaceID, labelDigest, kind, receiptID, recordID, formatTime(s.now().UTC()))
	if err != nil {
		return 0, err
	}
	n, err := r.LastInsertId()
	return uint64(n), err
}
func (s *Store) factCursor(ctx context.Context, tx *sql.Tx, view authorizedView) (facts.Cursor, error) {
	spaces := append([]memory.SpaceID(nil), view.readSpaceIDs...)
	sort.Slice(spaces, func(i, j int) bool { return spaces[i] < spaces[j] })
	encoded, _ := json.Marshal(struct {
		Spaces []memory.SpaceID
		Labels string
	}{spaces, view.labelSetDigest})
	cursor := facts.Cursor{Generation: s.generation, Scope: digestString(string(encoded))}
	for _, space := range spaces {
		var n uint64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM memory_changes WHERE space_id=? AND label_set_digest=?`, space, view.labelSetDigest).Scan(&n); err != nil {
			return cursor, err
		}
		if n > cursor.Sequence {
			cursor.Sequence = n
		}
	}
	return cursor, nil
}
func (s *Store) Changes(ctx context.Context, auth memory.CallAuthorization, request facts.ChangesRequest) (response facts.ChangesResponse, resultErr error) {
	defer func() { resultErr = s.factServiceError(resultErr) }()
	if err := s.requireMutableGeneration(); err != nil {
		return facts.ChangesResponse{}, err
	}
	if request.Limit < 1 || request.Limit > 256 {
		return facts.ChangesResponse{}, s.serviceError(memory.ErrorCodeInvalidArgument, "change limit must be 1..256", false)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return facts.ChangesResponse{}, err
	}
	defer tx.Rollback()
	view, err := s.authorize(ctx, tx, auth, memory.OperationRecall)
	if err != nil {
		return facts.ChangesResponse{}, err
	}
	cursor, err := s.factCursor(ctx, tx, view)
	if err != nil {
		return facts.ChangesResponse{}, err
	}
	out := facts.ChangesResponse{Cursor: cursor, Changes: []facts.Change{}}
	if request.After.Generation != cursor.Generation || request.After.Scope != cursor.Scope || request.After.Sequence > cursor.Sequence {
		out.ResetRequired = true
		return out, nil
	}
	for _, space := range view.readSpaceIDs {
		rows, e := tx.QueryContext(ctx, `SELECT sequence,kind,receipt_id,record_id,changed_at FROM memory_changes WHERE space_id=? AND label_set_digest=? AND sequence>? ORDER BY sequence LIMIT ?`, space, view.labelSetDigest, request.After.Sequence, request.Limit+1)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var c facts.Change
			var at string
			if e = rows.Scan(&c.Sequence, &c.Kind, &c.ReceiptID, &c.RecordID, &at); e != nil {
				rows.Close()
				return out, e
			}
			c.EffectiveAt, e = parseTime(at)
			if e != nil {
				rows.Close()
				return out, fmt.Errorf("change timestamp: %w", e)
			}
			out.Changes = append(out.Changes, c)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
	}
	sort.Slice(out.Changes, func(i, j int) bool { return out.Changes[i].Sequence < out.Changes[j].Sequence })
	if len(out.Changes) > request.Limit {
		out.HasMore = true
		out.Changes = out.Changes[:request.Limit]
		out.Cursor.Sequence = out.Changes[len(out.Changes)-1].Sequence
	}
	return out, tx.Commit()
}
