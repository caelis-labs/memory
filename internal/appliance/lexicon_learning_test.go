package appliance

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestSchemaFiveMigrationLearnsHistoricalPrivateLexicon(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "lexicon-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC()
	if err := migrateTo(t.Context(), database, now, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(),
		`INSERT INTO realms(id, created_at) VALUES ('realm-lexicon', ?)`, formatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(),
		`INSERT INTO spaces(id, realm_id, identity_id, class, created_at)
		 VALUES ('space-lexicon', 'realm-lexicon', NULL, 'shared', ?)`, formatTime(now)); err != nil {
		t.Fatal(err)
	}
	receiptTable := spaceIndexTable("space-lexicon")
	semanticTable := semanticSpaceIndexTable("space-lexicon")
	if _, err := database.ExecContext(t.Context(),
		`INSERT INTO space_indexes(space_id, table_name) VALUES ('space-lexicon', ?)`, receiptTable); err != nil {
		t.Fatal(err)
	}
	if err := createReceiptFTSTable(t.Context(), database, receiptTable); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(),
		`INSERT INTO semantic_space_indexes(space_id, table_name) VALUES ('space-lexicon', ?)`, semanticTable); err != nil {
		t.Fatal(err)
	}
	if err := createSemanticFTSTable(t.Context(), database, semanticTable); err != nil {
		t.Fatal(err)
	}
	for index, text := range []string{"采用云舟网络。", "升级云舟网络。", "云舟网络稳定。"} {
		receiptID := fmt.Sprintf("receipt-migration-%d", index)
		if _, err := database.ExecContext(t.Context(),
			`INSERT INTO receipts(
			 receipt_id, space_id, text, source_context, occurred_at, received_at,
			 idempotency_key, request_digest, consistency_token
			) VALUES (?, 'space-lexicon', ?, '{}', NULL, ?, ?, ?, ?)`,
			receiptID, text, formatTime(now), fmt.Sprintf("migration-%d", index),
			fmt.Sprintf("digest-%d", index), fmt.Sprintf("cursor-%d", index)); err != nil {
			t.Fatal(err)
		}
		if err := indexReceiptProjection(t.Context(), database, receiptTable, v1alpha1.ReceiptID(receiptID), text, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateTo(t.Context(), database, now, 6); err != nil {
		t.Fatal(err)
	}
	var status string
	var frequency int
	if err := database.QueryRowContext(t.Context(),
		`SELECT status, document_frequency FROM lexicon_terms
		 WHERE space_id = 'space-lexicon' AND term = '云舟网络'`).Scan(&status, &frequency); err != nil {
		t.Fatal(err)
	}
	if status != "active" || frequency != 3 {
		t.Fatalf("migrated lexicon = %s/%d, want active/3", status, frequency)
	}
	var generation, indexedGeneration int
	if err := database.QueryRowContext(t.Context(),
		`SELECT generation, indexed_generation FROM space_lexicons WHERE space_id = 'space-lexicon'`).Scan(
		&generation, &indexedGeneration); err != nil {
		t.Fatal(err)
	}
	if generation <= 1 || generation != indexedGeneration {
		t.Fatalf("migrated generations = %d/%d", generation, indexedGeneration)
	}
}

func TestPrivateLexiconPromotesAcrossIndependentReceiptsAndPersists(t *testing.T) {
	dataDir := t.TempDir()
	store, auth := newGoldenStore(t, dataDir, time.Now)
	texts := []string{
		"采用量子织网。",
		"升级量子织网。",
		"量子织网稳定。",
	}
	for index, text := range texts {
		if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: text, IdempotencyKey: "lexicon-promote-" + string(rune('a'+index)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertLexiconTerm(t, store, "space-bot-a", "量子织网", "active", 3)
	assertLexiconMissing(t, store, "space-bot-b", "量子织网")
	inspection, err := store.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Lexicon.ActiveTerms == 0 || inspection.Lexicon.PendingRebuilds != 0 ||
		inspection.Lexicon.GenerationSum <= inspection.Lexicon.Spaces {
		t.Fatalf("lexicon diagnostics = %+v", inspection.Lexicon)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), Options{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assertLexiconTerm(t, store, "space-bot-a", "量子织网", "active", 3)
	response, err := store.Recall(t.Context(), auth, testRecall("量子织网", ""))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, response, texts[0])
}

func TestLexiconRetiresWhenEvidenceIsCorrectedAndDeleted(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer store.Close()
	receipts := make([]v1alpha1.ReceiptID, 0, 3)
	for index, text := range []string{"采用星河编排。", "升级星河编排。", "星河编排稳定。"} {
		remembered, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: text, IdempotencyKey: "lexicon-retire-" + string(rune('a'+index)),
		})
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, remembered.ReceiptID)
	}
	assertLexiconTerm(t, store, "space-bot-a", "星河编排", "active", 3)
	if _, err := store.CorrectReceipt(t.Context(), managementv1alpha1.CorrectReceiptRequest{
		ReceiptID: receipts[0], ReplacementText: "改用普通调度。", Reason: "test correction",
		IdempotencyKey: "lexicon-correct",
	}); err != nil {
		t.Fatal(err)
	}
	assertLexiconTerm(t, store, "space-bot-a", "星河编排", "retired", 2)
	if _, err := store.DeleteReceipt(t.Context(), managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: receipts[1], Reason: "test deletion", IdempotencyKey: "lexicon-delete",
	}); err != nil {
		t.Fatal(err)
	}
	assertLexiconTerm(t, store, "space-bot-a", "星河编排", "retired", 1)
}

func TestLexiconRejectsSingleReceiptRepetition(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer store.Close()
	if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "幻海矩阵幻海矩阵幻海矩阵幻海矩阵。", IdempotencyKey: "lexicon-spam",
	}); err != nil {
		t.Fatal(err)
	}
	assertLexiconTerm(t, store, "space-bot-a", "幻海矩阵", "candidate", 1)
}

func TestStewardCanApproveOnlyEvidenceBackedNearThresholdTerm(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer store.Close()
	putAndBindSteward(t, store, 1)
	for index, text := range []string{"采用曜石编排。", "升级曜石编排。"} {
		if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: text, IdempotencyKey: "lexicon-steward-" + string(rune('a'+index)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found {
		t.Fatalf("ClaimStewardJob found=%v err=%v", found, err)
	}
	foundCandidate := false
	for _, candidate := range work.Request.LexiconCandidates {
		if candidate.Term == "曜石编排" && candidate.DocumentFrequency == 2 {
			foundCandidate = true
		}
	}
	if !foundCandidate {
		t.Fatalf("Steward candidates = %+v", work.Request.LexiconCandidates)
	}
	result, err := store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationIgnore, LexiconTerms: []string{"曜石编排"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.LexiconActivated != 1 {
		t.Fatalf("ApplyStewardProposal = %+v", result)
	}
	assertLexiconTerm(t, store, "space-bot-a", "曜石编排", "active", 2)
	var source string
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT source FROM lexicon_terms WHERE space_id = 'space-bot-a' AND term = '曜石编排'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "steward" {
		t.Fatalf("lexicon source = %q, want steward", source)
	}
}

func TestStewardCannotInventLexiconTerm(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer store.Close()
	putAndBindSteward(t, store, 1)
	receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "普通记忆内容。", IdempotencyKey: "lexicon-steward-invalid",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || work.Request.Receipt.ReceiptID != receipt.ReceiptID {
		t.Fatalf("ClaimStewardJob = %+v found=%v err=%v", work, found, err)
	}
	_, err = store.ApplyStewardProposal(t.Context(), work.Lease, stewardv1alpha1.Proposal{
		Operation: stewardv1alpha1.OperationIgnore, LexiconTerms: []string{"伪造领域词"},
	})
	if !errors.Is(err, ErrStewardProposalInvalid) {
		t.Fatalf("invented lexicon term error = %v", err)
	}
}

func assertLexiconTerm(t *testing.T, store *Store, spaceID, term, status string, documentFrequency int) {
	t.Helper()
	var gotStatus string
	var gotFrequency int
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT status, document_frequency FROM lexicon_terms WHERE space_id = ? AND term = ?`,
		spaceID, term).Scan(&gotStatus, &gotFrequency); err != nil {
		t.Fatalf("read lexicon term %q: %v", term, err)
	}
	if gotStatus != status || gotFrequency != documentFrequency {
		t.Fatalf("lexicon term %q = %s/%d, want %s/%d", term, gotStatus, gotFrequency, status, documentFrequency)
	}
}

func assertLexiconMissing(t *testing.T, store *Store, spaceID, term string) {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM lexicon_terms WHERE space_id = ? AND term = ?`, spaceID, term).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("lexicon term %q crossed into Space %q", term, spaceID)
	}
}
