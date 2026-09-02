package appliance

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const lexiconAlgorithmVersion = "han-boundary-v1"

// LexiconPolicy is experimental tuning input used only by corpus evaluation
// and focused tests. A nil policy keeps adaptive lexicon learning and use out
// of the production Remember, Recall, correction, deletion, and Steward paths.
type LexiconPolicy struct {
	MinTermRunes          int
	MaxTermRunes          int
	MaxCandidatesPerText  int
	MinDocumentFrequency  int
	MinBoundaryDiversity  int
	MaxAverageOccurrences float64
	MinActivationScore    float64
}

func normalizeLexiconPolicy(policy *LexiconPolicy) LexiconPolicy {
	result := LexiconPolicy{
		MinTermRunes:          3,
		MaxTermRunes:          8,
		MaxCandidatesPerText:  128,
		MinDocumentFrequency:  3,
		MinBoundaryDiversity:  2,
		MaxAverageOccurrences: 8,
		MinActivationScore:    6,
	}
	if policy == nil {
		return result
	}
	if policy.MinTermRunes > 0 {
		result.MinTermRunes = policy.MinTermRunes
	}
	if policy.MaxTermRunes >= result.MinTermRunes {
		result.MaxTermRunes = policy.MaxTermRunes
	}
	if policy.MaxCandidatesPerText > 0 {
		result.MaxCandidatesPerText = policy.MaxCandidatesPerText
	}
	if policy.MinDocumentFrequency > 0 {
		result.MinDocumentFrequency = policy.MinDocumentFrequency
	}
	if policy.MinBoundaryDiversity > 0 {
		result.MinBoundaryDiversity = policy.MinBoundaryDiversity
	}
	if policy.MaxAverageOccurrences > 0 {
		result.MaxAverageOccurrences = policy.MaxAverageOccurrences
	}
	if policy.MinActivationScore > 0 {
		result.MinActivationScore = policy.MinActivationScore
	}
	return result
}

type lexiconObservation struct {
	term          string
	occurrences   int
	leftContexts  map[string]struct{}
	rightContexts map[string]struct{}
}

type lexiconUpdate struct {
	activeTerms []string
	changed     bool
}

func discoverLexiconCandidates(text string, policy LexiconPolicy) ([]lexiconObservation, error) {
	segmenter, err := sharedBaseSegmenter()
	if err != nil {
		return nil, err
	}
	observations := make(map[string]*lexiconObservation)
	for _, run := range hanRuns(text) {
		runes := []rune(run)
		for size := policy.MinTermRunes; size <= policy.MaxTermRunes; size++ {
			for start := 0; start+size <= len(runes); start++ {
				term := string(runes[start : start+size])
				if baseDictionaryContains(segmenter, term) {
					continue
				}
				item := observations[term]
				if item == nil {
					item = &lexiconObservation{
						term: term, leftContexts: make(map[string]struct{}), rightContexts: make(map[string]struct{}),
					}
					observations[term] = item
				}
				item.occurrences++
				left := "^"
				if start > 0 {
					left = string(runes[start-1])
				}
				right := "$"
				if start+size < len(runes) {
					right = string(runes[start+size])
				}
				item.leftContexts[left] = struct{}{}
				item.rightContexts[right] = struct{}{}
			}
		}
	}
	result := make([]lexiconObservation, 0, len(observations))
	for _, item := range observations {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].occurrences != result[j].occurrences {
			return result[i].occurrences > result[j].occurrences
		}
		iRunes, jRunes := utf8.RuneCountInString(result[i].term), utf8.RuneCountInString(result[j].term)
		if iRunes != jRunes {
			return iRunes > jRunes
		}
		return result[i].term < result[j].term
	})
	if len(result) > policy.MaxCandidatesPerText {
		result = result[:policy.MaxCandidatesPerText]
	}
	return result, nil
}

func baseDictionaryContains(segmenter interface {
	Cut(string, ...bool) []string
}, term string) bool {
	pieces := segmenter.Cut(term, false)
	meaningful := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		piece = normalizeLexicalToken(piece)
		if piece != "" {
			meaningful = append(meaningful, piece)
		}
	}
	return len(meaningful) == 1 && meaningful[0] == term
}

func (s *Store) learnReceiptLexicon(
	ctx context.Context,
	tx *sql.Tx,
	spaceID v1alpha1.SpaceID,
	receiptID v1alpha1.ReceiptID,
	commitSequence int64,
	text string,
) (lexiconUpdate, error) {
	if !s.experimentalLexicon {
		return lexiconUpdate{}, nil
	}
	observations, err := discoverLexiconCandidates(text, s.lexiconPolicy)
	if err != nil {
		return lexiconUpdate{}, err
	}
	if len(observations) == 0 {
		terms, err := readActiveLexiconTerms(ctx, tx, spaceID)
		return lexiconUpdate{activeTerms: terms}, err
	}
	now := formatTime(s.now().UTC())
	affected := make([]string, 0, len(observations))
	for _, item := range observations {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO lexicon_terms(
			 space_id, term, status, source, first_seen_sequence, last_seen_sequence, updated_at
			) VALUES (?, ?, 'candidate', 'learner', ?, ?, ?)
			 ON CONFLICT(space_id, term) DO NOTHING`,
			spaceID, item.term, commitSequence, commitSequence, now); err != nil {
			return lexiconUpdate{}, fmt.Errorf("record lexicon candidate: %w", err)
		}
		result, err := tx.ExecContext(ctx,
			`INSERT INTO lexicon_term_evidence(
			 space_id, term, receipt_id, occurrences, left_contexts, right_contexts, commit_sequence
			) VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(space_id, term, receipt_id) DO NOTHING`,
			spaceID, item.term, receiptID, item.occurrences,
			joinContexts(item.leftContexts), joinContexts(item.rightContexts), commitSequence)
		if err != nil {
			return lexiconUpdate{}, fmt.Errorf("record lexicon evidence: %w", err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return lexiconUpdate{}, fmt.Errorf("read lexicon evidence effect: %w", err)
		}
		if inserted == 1 {
			affected = append(affected, item.term)
		}
	}
	changed, err := s.refreshLexiconTerms(ctx, tx, spaceID, affected, now)
	if err != nil {
		return lexiconUpdate{}, err
	}
	terms, err := readActiveLexiconTerms(ctx, tx, spaceID)
	return lexiconUpdate{activeTerms: terms, changed: changed}, err
}

func (s *Store) removeReceiptLexiconEvidence(
	ctx context.Context,
	tx *sql.Tx,
	spaceID v1alpha1.SpaceID,
	receiptID v1alpha1.ReceiptID,
) (lexiconUpdate, error) {
	if !s.experimentalLexicon {
		return lexiconUpdate{}, nil
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT term FROM lexicon_term_evidence WHERE space_id = ? AND receipt_id = ? ORDER BY term`,
		spaceID, receiptID)
	if err != nil {
		return lexiconUpdate{}, fmt.Errorf("read removed lexicon evidence: %w", err)
	}
	var affected []string
	for rows.Next() {
		var term string
		if err := rows.Scan(&term); err != nil {
			_ = rows.Close()
			return lexiconUpdate{}, fmt.Errorf("read removed lexicon evidence: %w", err)
		}
		affected = append(affected, term)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return lexiconUpdate{}, fmt.Errorf("read removed lexicon evidence: %w", err)
	}
	if err := rows.Close(); err != nil {
		return lexiconUpdate{}, fmt.Errorf("close removed lexicon evidence: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM lexicon_term_evidence WHERE space_id = ? AND receipt_id = ?`, spaceID, receiptID); err != nil {
		return lexiconUpdate{}, fmt.Errorf("remove lexicon evidence: %w", err)
	}
	changed, err := s.refreshLexiconTerms(ctx, tx, spaceID, affected, formatTime(s.now().UTC()))
	if err != nil {
		return lexiconUpdate{}, err
	}
	terms, err := readActiveLexiconTerms(ctx, tx, spaceID)
	return lexiconUpdate{activeTerms: terms, changed: changed}, err
}

type lexiconAggregate struct {
	documentFrequency int
	occurrenceCount   int
	left              map[string]struct{}
	right             map[string]struct{}
	firstSequence     int64
	lastSequence      int64
}

func (s *Store) refreshLexiconTerms(
	ctx context.Context,
	tx *sql.Tx,
	spaceID v1alpha1.SpaceID,
	terms []string,
	updatedAt string,
) (bool, error) {
	terms = uniqueSortedStrings(terms)
	if len(terms) == 0 {
		return false, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(terms)), ",")
	args := make([]any, 0, len(terms)+1)
	args = append(args, spaceID)
	for _, term := range terms {
		args = append(args, term)
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT term, occurrences, left_contexts, right_contexts, commit_sequence
		 FROM lexicon_term_evidence
		 WHERE space_id = ? AND term IN (`+placeholders+`)
		 ORDER BY term, commit_sequence`, args...)
	if err != nil {
		return false, fmt.Errorf("aggregate lexicon evidence: %w", err)
	}
	aggregates := make(map[string]*lexiconAggregate, len(terms))
	for rows.Next() {
		var term, left, right string
		var occurrences int
		var sequence int64
		if err := rows.Scan(&term, &occurrences, &left, &right, &sequence); err != nil {
			_ = rows.Close()
			return false, fmt.Errorf("aggregate lexicon evidence: %w", err)
		}
		item := aggregates[term]
		if item == nil {
			item = &lexiconAggregate{left: make(map[string]struct{}), right: make(map[string]struct{}), firstSequence: sequence}
			aggregates[term] = item
		}
		item.documentFrequency++
		item.occurrenceCount += occurrences
		item.lastSequence = sequence
		for _, context := range strings.Fields(left) {
			item.left[context] = struct{}{}
		}
		for _, context := range strings.Fields(right) {
			item.right[context] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, fmt.Errorf("aggregate lexicon evidence: %w", err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("close lexicon evidence: %w", err)
	}
	changed := false
	for _, term := range terms {
		item := aggregates[term]
		if item == nil {
			var prior string
			if err := tx.QueryRowContext(ctx,
				`SELECT status FROM lexicon_terms WHERE space_id = ? AND term = ?`, spaceID, term).Scan(&prior); err != nil {
				return false, fmt.Errorf("read empty lexicon term: %w", err)
			}
			changed = changed || prior == "active"
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM lexicon_terms WHERE space_id = ? AND term = ?`, spaceID, term); err != nil {
				return false, fmt.Errorf("remove empty lexicon term: %w", err)
			}
			continue
		}
		score := lexiconScore(item)
		var prior string
		if err := tx.QueryRowContext(ctx,
			`SELECT status FROM lexicon_terms WHERE space_id = ? AND term = ?`, spaceID, term).Scan(&prior); err != nil {
			return false, fmt.Errorf("read lexicon term state: %w", err)
		}
		status := "candidate"
		if lexiconTermActive(item, score, s.lexiconPolicy) {
			status = "active"
		} else if prior == "active" || prior == "retired" {
			status = "retired"
		}
		changed = changed || prior != status && (prior == "active" || status == "active")
		if _, err := tx.ExecContext(ctx,
			`UPDATE lexicon_terms
			 SET status = ?, document_frequency = ?, occurrence_count = ?,
			 left_diversity = ?, right_diversity = ?, score = ?,
			 first_seen_sequence = ?, last_seen_sequence = ?, updated_at = ?
			 WHERE space_id = ? AND term = ?`,
			status, item.documentFrequency, item.occurrenceCount, len(item.left), len(item.right), score,
			item.firstSequence, item.lastSequence, updatedAt, spaceID, term); err != nil {
			return false, fmt.Errorf("refresh lexicon term: %w", err)
		}
	}
	if changed {
		if _, err := tx.ExecContext(ctx,
			`UPDATE space_lexicons SET generation = generation + 1, updated_at = ? WHERE space_id = ?`,
			updatedAt, spaceID); err != nil {
			return false, fmt.Errorf("advance lexicon generation: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE lexicon_terms
			 SET activated_generation = (SELECT generation FROM space_lexicons WHERE space_id = ?)
			 WHERE space_id = ? AND status = 'active' AND activated_generation = 0`, spaceID, spaceID); err != nil {
			return false, fmt.Errorf("record lexicon activation generation: %w", err)
		}
	}
	return changed, nil
}

func lexiconScore(item *lexiconAggregate) float64 {
	return 2*math.Log1p(float64(item.documentFrequency)) +
		math.Log1p(float64(item.occurrenceCount)) +
		0.5*float64(min(len(item.left), 4)+min(len(item.right), 4))
}

func lexiconTermActive(item *lexiconAggregate, score float64, policy LexiconPolicy) bool {
	if item.documentFrequency < policy.MinDocumentFrequency ||
		len(item.left) < policy.MinBoundaryDiversity ||
		len(item.right) < policy.MinBoundaryDiversity ||
		score < policy.MinActivationScore {
		return false
	}
	return float64(item.occurrenceCount)/float64(item.documentFrequency) <= policy.MaxAverageOccurrences
}

func readActiveLexiconTerms(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT term FROM lexicon_terms WHERE space_id = ? AND status = 'active'
		 ORDER BY length(term) DESC, score DESC, term`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var terms []string
	for rows.Next() {
		var term string
		if err := rows.Scan(&term); err != nil {
			return nil, err
		}
		terms = append(terms, term)
	}
	return terms, rows.Err()
}

func (s *Store) activeLexiconTerms(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
) ([]string, error) {
	if !s.experimentalLexicon {
		return nil, nil
	}
	return readActiveLexiconTerms(ctx, db, spaceID)
}

func markLexiconIndexed(ctx context.Context, db databaseExecutor, spaceID v1alpha1.SpaceID, updatedAt string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE space_lexicons SET indexed_generation = generation, updated_at = ? WHERE space_id = ?`,
		updatedAt, spaceID)
	return err
}

func rebuildSpaceLexicalProjection(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	activeTerms []string,
	updatedAt string,
) error {
	receiptTable, err := readSpaceIndex(ctx, db, spaceID)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM `+receiptTable); err != nil {
		return err
	}
	if err := rebuildReceiptProjection(ctx, db, spaceID, receiptTable, activeTerms); err != nil {
		return err
	}
	semanticTable, err := readSemanticSpaceIndex(ctx, db, spaceID)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM `+semanticTable); err != nil {
		return err
	}
	if err := rebuildSemanticProjection(ctx, db, spaceID, semanticTable, activeTerms); err != nil {
		return err
	}
	return markLexiconIndexed(ctx, db, spaceID, updatedAt)
}

func joinContexts(contexts map[string]struct{}) string {
	items := make([]string, 0, len(contexts))
	for item := range contexts {
		items = append(items, item)
	}
	sort.Strings(items)
	return strings.Join(items, " ")
}

func uniqueSortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validStewardLexiconTerm(value string, policy LexiconPolicy) bool {
	count := utf8.RuneCountInString(value)
	if count < policy.MinTermRunes || count > policy.MaxTermRunes {
		return false
	}
	for _, r := range value {
		if !unicode.Is(unicode.Han, r) && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

func readStewardLexiconCandidates(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	receiptID v1alpha1.ReceiptID,
) ([]stewardv1alpha1.LexiconCandidate, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.term, t.document_frequency, t.occurrence_count,
		 t.left_diversity, t.right_diversity, t.score
		 FROM lexicon_terms t
		 JOIN lexicon_term_evidence e ON e.space_id = t.space_id AND e.term = t.term
		 WHERE t.space_id = ? AND e.receipt_id = ? AND t.status IN ('candidate', 'retired')
		 AND t.document_frequency >= 2
		 ORDER BY t.score DESC, length(t.term) DESC, t.term
		 LIMIT ?`, spaceID, receiptID, stewardv1alpha1.MaxLexiconTerms)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []stewardv1alpha1.LexiconCandidate
	for rows.Next() {
		var item stewardv1alpha1.LexiconCandidate
		if err := rows.Scan(
			&item.Term, &item.DocumentFrequency, &item.OccurrenceCount,
			&item.LeftDiversity, &item.RightDiversity, &item.Score,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) applyStewardLexiconTerms(
	ctx context.Context,
	tx *sql.Tx,
	job storedStewardJob,
	terms []string,
	updatedAt string,
) (int, error) {
	if len(terms) == 0 {
		return 0, nil
	}
	if job.labelSetDigest != emptyLabelSetDigest {
		return 0, fmt.Errorf("%w: adaptive lexicon is not LabelSet-scoped", ErrStewardProposalInvalid)
	}
	if !s.experimentalLexicon {
		return 0, fmt.Errorf("%w: adaptive lexicon is experimental and disabled", ErrStewardProposalInvalid)
	}
	activated := 0
	for _, term := range terms {
		term = normalizeLexicalToken(term)
		if !validStewardLexiconTerm(term, s.lexiconPolicy) {
			return 0, fmt.Errorf("%w: Steward lexicon term is invalid", ErrStewardProposalInvalid)
		}
		var status string
		var documentFrequency, occurrenceCount, leftDiversity, rightDiversity int
		var score float64
		if err := tx.QueryRowContext(ctx,
			`SELECT status, document_frequency, occurrence_count, left_diversity, right_diversity, score
			 FROM lexicon_terms WHERE space_id = ? AND term = ?`, job.spaceID, term).Scan(
			&status, &documentFrequency, &occurrenceCount, &leftDiversity, &rightDiversity, &score,
		); err != nil {
			if err == sql.ErrNoRows {
				return 0, fmt.Errorf("%w: Steward lexicon term is not a candidate", ErrStewardProposalInvalid)
			}
			return 0, fmt.Errorf("read Steward lexicon term: %w", err)
		}
		var cited bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(
			 SELECT 1 FROM lexicon_term_evidence
			 WHERE space_id = ? AND term = ? AND receipt_id = ?
			)`, job.spaceID, term, job.receiptID).Scan(&cited); err != nil {
			return 0, fmt.Errorf("read Steward lexicon evidence: %w", err)
		}
		minimumScore := math.Max(0, s.lexiconPolicy.MinActivationScore-1.5)
		if !cited || documentFrequency < 2 || leftDiversity < 1 || rightDiversity < 1 ||
			score < minimumScore ||
			float64(occurrenceCount)/float64(documentFrequency) > s.lexiconPolicy.MaxAverageOccurrences {
			return 0, fmt.Errorf("%w: Steward lexicon term lacks bounded evidence", ErrStewardProposalInvalid)
		}
		if status == "active" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE lexicon_terms SET status = 'active', source = 'steward', updated_at = ?
			 WHERE space_id = ? AND term = ? AND status IN ('candidate', 'retired')`,
			updatedAt, job.spaceID, term); err != nil {
			return 0, fmt.Errorf("activate Steward lexicon term: %w", err)
		}
		activated++
	}
	if activated == 0 {
		return 0, nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE space_lexicons SET generation = generation + 1, updated_at = ? WHERE space_id = ?`,
		updatedAt, job.spaceID); err != nil {
		return 0, fmt.Errorf("advance Steward lexicon generation: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE lexicon_terms
		 SET activated_generation = (SELECT generation FROM space_lexicons WHERE space_id = ?)
		 WHERE space_id = ? AND source = 'steward' AND status = 'active' AND activated_generation = 0`,
		job.spaceID, job.spaceID); err != nil {
		return 0, fmt.Errorf("record Steward lexicon generation: %w", err)
	}
	activeTerms, err := readActiveLexiconTerms(ctx, tx, job.spaceID)
	if err != nil {
		return 0, fmt.Errorf("read Steward lexicon generation: %w", err)
	}
	if err := rebuildSpaceLexicalProjection(ctx, tx, job.spaceID, activeTerms, updatedAt); err != nil {
		return 0, fmt.Errorf("publish Steward lexicon generation: %w", err)
	}
	return activated, nil
}
