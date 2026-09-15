package appliance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	steward "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func (s *Store) applyFactMutation(ctx context.Context, tx *sql.Tx, view authorizedView, source facts.Source, receipt memory.ReceiptID, m facts.Mutation, now time.Time) (facts.Fact, error) {
	fail := func(message string) (facts.Fact, error) {
		return facts.Fact{}, s.serviceError(memory.ErrorCodeConflict, message, false)
	}
	meta := facts.Metadata{Subject: m.Subject, Key: m.Key, Adoption: facts.AdoptionPending, ValidFrom: m.ValidFrom, ValidUntil: m.ValidUntil, Conditions: m.Conditions, Transition: m.Transition}
	if source.Role == facts.RoleConfirmation || source.Role == facts.RoleObservation {
		meta.Adoption = facts.AdoptionConfirmed
	}
	if m.Transition == facts.TransitionDeny {
		meta.Adoption = facts.AdoptionDenied
	}
	id := m.TargetRecordID
	revision := m.ExpectedRevision + 1
	var prior facts.Fact
	if id != "" {
		var space memory.SpaceID
		var label, status string
		var current uint64
		err := tx.QueryRowContext(ctx, `SELECT space_id,label_set_digest,status,current_revision FROM semantic_records WHERE record_id=?`, id).Scan(&space, &label, &status, &current)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("fact target is absent or stale")
		}
		if err != nil {
			return facts.Fact{}, err
		}
		if space != view.writeSpaceID || label != view.labelSetDigest || status != "active" || current != m.ExpectedRevision {
			return fail("fact target is absent, unauthorized or stale")
		}
		prior, err = readFactRevision(ctx, tx, id, current)
		if err != nil {
			return facts.Fact{}, err
		}
		if prior.Metadata.Subject != m.Subject || prior.Metadata.Key != m.Key {
			return fail("fact target subject or key differs")
		}
		if !slices.Equal(prior.Metadata.Conditions, m.Conditions) {
			return fail("lifecycle edit must retain exact conditions")
		}
		if m.Transition == facts.TransitionConfirm {
			if prior.Metadata.Adoption != facts.AdoptionPending || prior.Text != m.Text {
				return fail("confirmation must confirm exact pending content")
			}
			meta.ValidFrom = prior.Metadata.ValidFrom
			meta.ValidUntil = prior.Metadata.ValidUntil
		}
		if m.Transition == facts.TransitionChange || m.Transition == facts.TransitionException {
			if prior.Metadata.Adoption != facts.AdoptionConfirmed {
				return fail("change or exception requires confirmed fact")
			}
			if prior.Metadata.ValidFrom != nil && (m.ValidFrom.Before(*prior.Metadata.ValidFrom) || m.Transition == facts.TransitionChange && m.ValidFrom.Equal(*prior.Metadata.ValidFrom)) {
				return fail("change must follow target onset; use correction to edit the same interval")
			}
		}
		if m.Transition == facts.TransitionCorrect && prior.Metadata.RelatedRecordID == "" {
			history, err := s.factTimeline(ctx, tx, id)
			if err != nil {
				return facts.Fact{}, err
			}
			for i := len(history) - 2; i >= 0; i-- {
				previous := history[i]
				if previous.HistoricalState != "changed" {
					continue
				}
				if m.ValidFrom == nil || previous.Metadata.ValidFrom != nil && !m.ValidFrom.After(*previous.Metadata.ValidFrom) {
					return fail("corrected change requires an onset after the preceding interval onset")
				}
				break
			}
		}
		// An exception remains linked after any number of corrections. The
		// latest transition describes the edit, not the Record's lasting role.
		if prior.Metadata.RelatedRecordID != "" {
			if m.Transition != facts.TransitionDeny && m.Transition != facts.TransitionCorrect {
				return fail("exception can only be corrected or denied")
			}
			meta.RelatedRecordID = prior.Metadata.RelatedRecordID
			if meta.ValidFrom == nil || meta.ValidUntil == nil {
				return fail("corrected exception retains finite interval")
			}
		}
	}
	if m.Transition == facts.TransitionEstablish && m.Key != "" {
		// Same-key assertions cannot silently fork a current truth. Subject/condition
		// matching is exact; event records may omit the key and remain independent.
		rows, err := tx.QueryContext(ctx, `SELECT v.fact_json FROM semantic_records r JOIN semantic_revisions v ON v.record_id=r.record_id AND v.revision=r.current_revision WHERE r.space_id=? AND r.label_set_digest=? AND r.subject=? AND r.fact_key=? AND r.status='active'`, view.writeSpaceID, view.labelSetDigest, m.Subject, m.Key)
		if err != nil {
			return facts.Fact{}, err
		}
		conflict := false
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return facts.Fact{}, err
			}
			var other facts.Metadata
			if err = json.Unmarshal([]byte(raw), &other); err != nil {
				rows.Close()
				return facts.Fact{}, err
			}
			if other.RelatedRecordID == "" && slices.Equal(other.Conditions, m.Conditions) {
				conflict = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return facts.Fact{}, err
		}
		if conflict {
			return fail("fact key already exists; select explicit lifecycle target")
		}
	}
	if m.Transition == facts.TransitionException {
		meta.RelatedRecordID = id
	}
	// Establish already rejects every same-condition head (including pending).
	// All other edits that produce a confirmed base fact share the same guard;
	// in particular, correcting a pending Steward proposal cannot bypass confirm.
	if meta.Adoption == facts.AdoptionConfirmed && meta.RelatedRecordID == "" && m.Key != "" && m.Transition != facts.TransitionEstablish {
		conflict, err := confirmedFactConflict(ctx, tx, view, meta, id)
		if err != nil {
			return facts.Fact{}, err
		}
		if conflict {
			return fail("edit would fork a confirmed fact; explicitly change or correct its head")
		}
	}
	if meta.RelatedRecordID != "" && meta.Adoption == facts.AdoptionConfirmed {
		// Both a new exception and a corrected interval must remain unambiguous.
		rows, err := tx.QueryContext(ctx, `SELECT v.fact_json FROM semantic_records r JOIN semantic_revisions v ON v.record_id=r.record_id AND v.revision=r.current_revision WHERE r.space_id=? AND r.label_set_digest=? AND r.subject=? AND r.fact_key=? AND r.status='active' AND r.record_id<>?`, view.writeSpaceID, view.labelSetDigest, m.Subject, m.Key, id)
		if err != nil {
			return facts.Fact{}, err
		}
		conflict := false
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return facts.Fact{}, err
			}
			var other facts.Metadata
			if err = json.Unmarshal([]byte(raw), &other); err != nil {
				rows.Close()
				return facts.Fact{}, err
			}
			if other.RelatedRecordID == meta.RelatedRecordID && other.Adoption == facts.AdoptionConfirmed && other.ValidFrom != nil && other.ValidUntil != nil && meta.ValidFrom.Before(*other.ValidUntil) && other.ValidFrom.Before(*meta.ValidUntil) {
				conflict = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return facts.Fact{}, err
		}
		if conflict {
			return fail("overlapping exceptions are ambiguous")
		}
	}
	newRecord := m.Transition == facts.TransitionEstablish || m.Transition == facts.TransitionException
	if newRecord {
		suffix, err := s.randomHex(16)
		if err != nil {
			return facts.Fact{}, err
		}
		id = "record-" + suffix
		revision = 1
		_, err = tx.ExecContext(ctx, `INSERT INTO semantic_records(record_id,space_id,kind,status,current_revision,created_at,updated_at,label_set,label_set_digest,subject,fact_key) VALUES(?,?,'fact','active',1,?,?,?,?,?,?)`, id, view.writeSpaceID, formatTime(now), formatTime(now), view.labelSetEncoded, view.labelSetDigest, m.Subject, m.Key)
		if err != nil {
			return facts.Fact{}, err
		}
	} else {
		status := "active"
		if m.Transition == facts.TransitionDeny {
			status = "invalidated"
		}
		_, err := tx.ExecContext(ctx, `UPDATE semantic_records SET current_revision=?,status=?,invalidated_reason=?,updated_at=? WHERE record_id=?`, revision, status, string(m.Transition), formatTime(now), id)
		if err != nil {
			return facts.Fact{}, err
		}
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return facts.Fact{}, err
	}
	op := "SUPERSEDE"
	if newRecord {
		op = "ADD"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO semantic_revisions(record_id,revision,space_id,kind,text,operation,job_id,created_at,fact_json) VALUES(?,?,?,'fact',?,?,NULL,?,?)`, id, revision, view.writeSpaceID, m.Text, op, formatTime(now), string(encoded)); err != nil {
		return facts.Fact{}, err
	}
	evidence := []memory.ReceiptID{receipt}
	if m.Transition == facts.TransitionConfirm {
		for _, e := range prior.Evidence {
			if !slices.Contains(evidence, e.ReceiptID) {
				evidence = append(evidence, e.ReceiptID)
			}
		}
	}
	for i, e := range evidence {
		if _, err = tx.ExecContext(ctx, `INSERT INTO semantic_evidence(record_id,revision,ordinal,receipt_id,space_id) VALUES(?,?,?,?,?)`, id, revision, i, e, view.writeSpaceID); err != nil {
			return facts.Fact{}, err
		}
	}
	table, err := readSemanticSpaceIndex(ctx, tx, view.writeSpaceID)
	if err != nil {
		return facts.Fact{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE record_id=?`, id); err != nil {
		return facts.Fact{}, err
	}
	if meta.Adoption != facts.AdoptionDenied {
		if err = indexSemanticProjection(ctx, tx, table, steward.RecordID(id), revision, m.Text, nil); err != nil {
			return facts.Fact{}, err
		}
	}
	if _, err = s.recordMemoryChange(ctx, tx, view.writeSpaceID, view.labelSetDigest, "fact_"+string(m.Transition), receipt, id); err != nil {
		return facts.Fact{}, err
	}
	return readFactRevision(ctx, tx, id, revision)
}

func readFactRevision(ctx context.Context, db databaseExecutor, id string, revision uint64) (facts.Fact, error) {
	f := facts.Fact{RecordID: id, Revision: revision}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT space_id,text,fact_json FROM semantic_revisions WHERE record_id=? AND revision=?`, id, revision).Scan(&f.SpaceID, &f.Text, &raw)
	if err != nil {
		return f, err
	}
	if raw == "" {
		return f, fmt.Errorf("record has no attributable fact metadata")
	}
	if err = json.Unmarshal([]byte(raw), &f.Metadata); err != nil {
		return f, err
	}
	rows, err := db.QueryContext(ctx, `SELECT e.receipt_id,s.source_json FROM semantic_evidence e LEFT JOIN evidence_sources s ON s.receipt_id=e.receipt_id WHERE e.record_id=? AND e.revision=? ORDER BY e.ordinal`, id, revision)
	if err != nil {
		return f, err
	}
	defer rows.Close()
	for rows.Next() {
		var e facts.Evidence
		var source sql.NullString
		if err = rows.Scan(&e.ReceiptID, &source); err != nil {
			return f, err
		}
		if source.Valid && source.String != "" {
			e.Source = &facts.Source{}
			if err = json.Unmarshal([]byte(source.String), e.Source); err != nil {
				return f, err
			}
		}
		f.Evidence = append(f.Evidence, e)
	}
	return f, rows.Err()
}

// confirmedFactConflict is independent of the proposal's text and transition:
// identical values in distinct authoritative heads still create ambiguity.
func confirmedFactConflict(ctx context.Context, tx *sql.Tx, view authorizedView, meta facts.Metadata, id string) (bool, error) {
	var conflict bool
	conditions, err := json.Marshal(meta.Conditions)
	if err != nil {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM semantic_records r JOIN semantic_revisions v ON v.record_id=r.record_id AND v.revision=r.current_revision WHERE r.space_id=? AND r.label_set_digest=? AND r.subject=? AND r.fact_key=? AND r.record_id<>? AND r.status='active' AND v.fact_json!='' AND json_extract(v.fact_json,'$.adoption')='confirmed' AND COALESCE(json_extract(v.fact_json,'$.related_record_id'),'')='' AND COALESCE(json_extract(v.fact_json,'$.conditions'),'null')=?)`, view.writeSpaceID, view.labelSetDigest, meta.Subject, meta.Key, id, string(conditions)).Scan(&conflict)
	return conflict, err
}
