package appliance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	steward "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// controlledFactAliases is versioned policy, not a learned profile or a query
// rewrite model. Original lexical candidates are retained alongside key hits.
var controlledFactAliases = map[string][]string{
	"preference.drink": {"coffee", "tea", "咖啡", "茶", "饮品", "饮料", "beverage", "drink"},
	"diet":             {"diet", "vegetarian", "素食", "饮食"},
	"language":         {"language", "语言", "中文", "english"},
	"timezone":         {"timezone", "time zone", "时区"},
}

func matchesFactAlias(query, alias string) bool {
	// ASCII aliases match words/phrases, not substrings ("team" is not "tea").
	ascii := true
	for _, r := range alias {
		if r > 127 {
			ascii = false
			break
		}
	}
	if !ascii {
		return strings.Contains(query, alias)
	}
	word := func(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' }
	for offset := 0; offset < len(query); {
		i := strings.Index(query[offset:], alias)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(alias)
		if (i == 0 || !word(query[i-1])) && (end == len(query) || !word(query[end])) {
			return true
		}
		offset = i + 1
	}
	return false
}

func factQueryRank(query string, f facts.Fact) (float64, error) {
	if strings.TrimSpace(query) == "" {
		return 0, nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	key := strings.ToLower(f.Metadata.Key)
	if q == key && key != "" {
		return -100, nil
	}
	for k, aliases := range controlledFactAliases {
		if key == k {
			for _, alias := range aliases {
				if matchesFactAlias(q, alias) {
					return -50, nil
				}
			}
		}
	}
	return lexicalRank(query, f.Text+" "+f.Metadata.Key, nil, 0)
}

func (s *Store) ReadFacts(ctx context.Context, auth memory.CallAuthorization, r facts.ReadRequest) (response facts.ReadResponse, resultErr error) {
	defer func() { resultErr = s.factServiceError(resultErr) }()
	if err := s.requireMutableGeneration(); err != nil {
		return facts.ReadResponse{}, err
	}
	if err := r.Validate(); err != nil {
		return facts.ReadResponse{}, s.serviceError(memory.ErrorCodeInvalidArgument, err.Error(), false)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return facts.ReadResponse{}, err
	}
	defer tx.Rollback()
	view, err := s.authorize(ctx, tx, auth, memory.OperationRecall)
	if err != nil {
		return facts.ReadResponse{}, err
	}
	out := facts.ReadResponse{Facts: []facts.Fact{}, BytesUsed: 2}
	out.Cursor, err = s.factCursor(ctx, tx, view)
	if err != nil {
		return out, err
	}
	at := s.now().UTC()
	if r.AsOf != nil {
		at = r.AsOf.UTC()
	}
	var candidates []facts.Fact
	for _, space := range view.readSpaceIDs {
		if s.candidateRead != nil {
			s.candidateRead(space)
		}
		ids, err := factRecordIDs(ctx, tx, space, view.labelSetDigest, r.Subject, r.Key, "", false)
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			history, err := s.factTimeline(ctx, tx, id)
			if err != nil {
				return out, err
			}
			var selected *facts.Fact
			for i := range history {
				f := history[i]
				m := f.Metadata
				if f.HistoricalState == "corrected" || m.Adoption != facts.AdoptionConfirmed {
					continue
				}
				matches := true
				for _, c := range m.Conditions {
					if r.Context[c.Key] != c.Value {
						matches = false
						break
					}
				}
				if !matches {
					continue
				}
				if r.AsOf == nil {
					for _, t := range []*time.Time{m.ValidFrom, m.ValidUntil} {
						if t != nil && t.After(at) && (out.RefreshAt == nil || t.Before(*out.RefreshAt)) {
							value := *t
							out.RefreshAt = &value
						}
					}
				}
				if r.AsOf != nil && m.ValidFrom == nil {
					continue
				}
				if m.ValidFrom != nil && m.ValidFrom.After(at) || m.ValidUntil != nil && !m.ValidUntil.After(at) {
					continue
				}
				value := f
				selected = &value
			}
			if selected != nil {
				candidates = append(candidates, *selected)
			}
		}
	}
	// An applicable finite exception suppresses only its exact same-partition
	// base. No global BM25 score comparison or cross-Space truth merge occurs.
	overridden := map[string]bool{}
	for _, f := range candidates {
		if f.Metadata.RelatedRecordID != "" {
			overridden[string(f.SpaceID)+"\x00"+f.Metadata.RelatedRecordID] = true
		}
	}
	// Determine adoption before query matching or budget trimming. Otherwise a
	// query naming the default can hide its applicable conditional replacement
	// (or one side of an ambiguity) and incorrectly revive that default.
	groups := map[string][]facts.Fact{}
	for _, f := range candidates {
		if overridden[string(f.SpaceID)+"\x00"+f.RecordID] {
			continue
		}
		key := string(f.SpaceID) + "\x00" + f.Metadata.Key
		if f.Metadata.Key == "" {
			key += "\x00" + f.RecordID
		}
		groups[key] = append(groups[key], f)
	}
	// Conditions refine unconditional facts. Equally-specific conflicting matches
	// abstain instead of arbitrarily asserting one user's fact.
	var ranked []rankedFact
	for _, group := range groups {
		max := -1
		for _, f := range group {
			if len(f.Metadata.Conditions) > max {
				max = len(f.Metadata.Conditions)
			}
		}
		var best []facts.Fact
		for _, f := range group {
			if len(f.Metadata.Conditions) == max {
				best = append(best, f)
			}
		}
		if len(best) != 1 {
			continue
		}
		f := best[0]
		rank, err := factQueryRank(r.Query, f)
		if err != nil {
			return out, err
		}
		if strings.TrimSpace(r.Query) != "" && rank >= 0 {
			continue
		}
		ranked = append(ranked, rankedFact{f, rank})
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.fact.SpaceID != b.fact.SpaceID {
			return a.fact.SpaceID < b.fact.SpaceID
		}
		if a.fact.Metadata.Key != b.fact.Metadata.Key {
			return a.fact.Metadata.Key < b.fact.Metadata.Key
		}
		return a.fact.RecordID < b.fact.RecordID
	})
	for _, c := range ranked {
		f, err := readFactRevision(ctx, tx, c.fact.RecordID, c.fact.Revision)
		if err != nil {
			return out, err
		}
		f.Metadata = c.fact.Metadata
		f.HistoricalState = c.fact.HistoricalState
		valid, err := factEvidenceUsable(ctx, tx, f, view.labelSetDigest)
		if err != nil {
			return out, err
		}
		if !valid {
			continue
		}
		if !appendBudgetFact(&out, f, r.Budget, true) {
			out.Truncated = true
		}
	}
	return out, tx.Commit()
}

type rankedFact struct {
	fact facts.Fact
	rank float64
}

func factRecordIDs(ctx context.Context, db databaseExecutor, space memory.SpaceID, labels, subject, key, id string, history bool) ([]string, error) {
	q := `SELECT record_id FROM semantic_records WHERE space_id=? AND label_set_digest=? AND subject=?`
	args := []any{space, labels, subject}
	if !history {
		q += ` AND status='active'`
	}
	if key != "" {
		q += ` AND fact_key=?`
		args = append(args, key)
	}
	if id != "" {
		q += ` AND record_id=?`
		args = append(args, id)
	}
	q += ` ORDER BY fact_key,record_id`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// factTimeline derives past intervals from explicit transitions, never from
// updated_at. Corrections mark erroneous revisions rather than inventing a past
// user preference; denied/forgotten records cannot be historical adoption.
func (s *Store) factTimeline(ctx context.Context, db databaseExecutor, id string) ([]facts.Fact, error) {
	forgotten, err := s.forgottenRecord(ctx, db, steward.RecordID(id))
	if err != nil {
		return nil, err
	}
	if forgotten {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT revision,space_id,text,fact_json FROM semantic_revisions WHERE record_id=? AND fact_json!='' ORDER BY revision`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []facts.Fact
	for rows.Next() {
		f := facts.Fact{RecordID: id}
		var raw string
		if err = rows.Scan(&f.Revision, &f.SpaceID, &f.Text, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &f.Metadata); err != nil {
			return nil, err
		}
		history = append(history, f)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Resolve each correction chain before deriving adjacent effective intervals.
	// A corrected change retains its place in the lifecycle, but only its final
	// revision may determine when the preceding fact stops applying.
	var effective []int
	for i := range history {
		switch history[i].Metadata.Transition {
		case facts.TransitionCorrect:
			if i > 0 {
				history[i-1].HistoricalState = "corrected"
			}
			if len(effective) > 0 {
				effective[len(effective)-1] = i
				continue
			}
		case facts.TransitionConfirm:
			// Confirmation adopts the same assertion, not a new preference.
			if len(effective) > 0 {
				effective[len(effective)-1] = i
				continue
			}
		case facts.TransitionDeny:
			for j := 0; j <= i; j++ {
				history[j].Metadata.Adoption = facts.AdoptionDenied
				history[j].HistoricalState = "denied"
			}
			return history, nil
		}
		effective = append(effective, i)
	}
	for i := 1; i < len(effective); i++ {
		prev := &history[effective[i-1]]
		next := history[effective[i]].Metadata
		if next.ValidFrom != nil && (prev.Metadata.ValidUntil == nil || next.ValidFrom.Before(*prev.Metadata.ValidUntil)) {
			v := *next.ValidFrom
			prev.Metadata.ValidUntil = &v
		}
		prev.HistoricalState = "changed"
	}
	return history, nil
}
func factEvidenceUsable(ctx context.Context, db databaseExecutor, f facts.Fact, labels string) (bool, error) {
	if len(f.Evidence) == 0 {
		return false, nil
	}
	for _, e := range f.Evidence {
		var valid bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM receipts r WHERE r.receipt_id=? AND r.space_id=? AND r.label_set_digest=? AND NOT EXISTS(SELECT 1 FROM receipt_tombstones t WHERE t.receipt_id=r.receipt_id) AND NOT EXISTS(SELECT 1 FROM receipt_corrections c WHERE c.original_receipt_id=r.receipt_id))`, e.ReceiptID, f.SpaceID, labels).Scan(&valid)
		if err != nil || !valid {
			return false, err
		}
	}
	return true, nil
}
func (s *Store) FactHistory(ctx context.Context, auth memory.CallAuthorization, r facts.HistoryRequest) (response facts.ReadResponse, resultErr error) {
	defer func() { resultErr = s.factServiceError(resultErr) }()
	if err := s.requireMutableGeneration(); err != nil {
		return facts.ReadResponse{}, err
	}
	if err := r.Validate(); err != nil {
		return facts.ReadResponse{}, s.serviceError(memory.ErrorCodeInvalidArgument, err.Error(), false)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return facts.ReadResponse{}, err
	}
	defer tx.Rollback()
	view, err := s.authorize(ctx, tx, auth, memory.OperationRecall)
	if err != nil {
		return facts.ReadResponse{}, err
	}
	out := facts.ReadResponse{Facts: []facts.Fact{}, BytesUsed: 2}
	out.Cursor, err = s.factCursor(ctx, tx, view)
	if err != nil {
		return out, err
	}
	for _, space := range view.readSpaceIDs {
		ids, err := factRecordIDs(ctx, tx, space, view.labelSetDigest, r.Subject, "", r.RecordID, true)
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			history, err := s.factTimeline(ctx, tx, id)
			if err != nil {
				return out, err
			}
			for _, h := range history {
				if h.Revision <= r.AfterRevision {
					continue
				}
				f, err := readFactRevision(ctx, tx, id, h.Revision)
				if err != nil {
					return out, err
				}
				f.Metadata = h.Metadata
				f.HistoricalState = h.HistoricalState
				if !appendBudgetFact(&out, f, r.Budget, false) {
					out.Truncated = true
					break
				}
				out.NextRevision = h.Revision
			}
		}
	}
	// History is audit data, never a personalization background.
	out.Background = ""
	encoded, _ := json.Marshal(out.Facts)
	out.BytesUsed = len(encoded)
	return out, tx.Commit()
}
func appendBudgetFact(out *facts.ReadResponse, f facts.Fact, b facts.Budget, background bool) bool {
	if len(out.Facts) >= b.MaxFacts {
		return false
	}
	encodedText, _ := json.Marshal(f.Text)
	line := ""
	if background {
		line = fmt.Sprintf("[%s@%d %s/%s] %s\n", f.RecordID, f.Revision, f.Metadata.Subject, f.Metadata.Key, encodedText)
	}
	next := append(append([]facts.Fact(nil), out.Facts...), f)
	encoded, _ := json.Marshal(next)
	used := len(encoded) + len(out.Background) + len(line)
	if used > b.MaxBytes {
		return false
	}
	out.Facts = next
	out.Background += line
	out.BytesUsed = used
	return true
}
