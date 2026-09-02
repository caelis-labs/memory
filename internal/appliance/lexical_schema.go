package appliance

import (
	"context"
	"database/sql"
	"fmt"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func migrateLexicalProjection(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT s.id, i.table_name, si.table_name
		 FROM spaces s
		 JOIN space_indexes i ON i.space_id = s.id
		 JOIN semantic_space_indexes si ON si.space_id = s.id
		 ORDER BY s.id`)
	if err != nil {
		return fmt.Errorf("list lexical indexes: %w", err)
	}
	type spaceIndexes struct {
		spaceID       v1alpha1.SpaceID
		receiptTable  string
		semanticTable string
	}
	var indexes []spaceIndexes
	for rows.Next() {
		var item spaceIndexes
		if err := rows.Scan(&item.spaceID, &item.receiptTable, &item.semanticTable); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read lexical index: %w", err)
		}
		if item.receiptTable != spaceIndexTable(item.spaceID) || item.semanticTable != semanticSpaceIndexTable(item.spaceID) {
			_ = rows.Close()
			return fmt.Errorf("lexical Space index identity mismatch")
		}
		indexes = append(indexes, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("list lexical indexes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close lexical indexes: %w", err)
	}
	for _, item := range indexes {
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+item.receiptTable); err != nil {
			return fmt.Errorf("replace receipt lexical index: %w", err)
		}
		if err := createReceiptFTSTable(ctx, tx, item.receiptTable); err != nil {
			return err
		}
		if err := rebuildReceiptProjection(ctx, tx, item.spaceID, item.receiptTable, nil); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+item.semanticTable); err != nil {
			return fmt.Errorf("replace semantic lexical index: %w", err)
		}
		if err := createSemanticFTSTable(ctx, tx, item.semanticTable); err != nil {
			return err
		}
		if err := rebuildSemanticProjection(ctx, tx, item.spaceID, item.semanticTable, nil); err != nil {
			return err
		}
	}
	return nil
}

func createReceiptFTSTable(ctx context.Context, db databaseExecutor, tableName string) error {
	if _, err := db.ExecContext(ctx,
		`CREATE VIRTUAL TABLE `+tableName+` USING fts5(
			receipt_id UNINDEXED,
			text,
			terms,
			ngrams,
			tokenize = 'unicode61'
		)`); err != nil {
		return fmt.Errorf("create receipt lexical index: %w", err)
	}
	return nil
}

func createSemanticFTSTable(ctx context.Context, db databaseExecutor, tableName string) error {
	if _, err := db.ExecContext(ctx,
		`CREATE VIRTUAL TABLE `+tableName+` USING fts5(
			record_id UNINDEXED,
			revision UNINDEXED,
			text,
			terms,
			ngrams,
			tokenize = 'unicode61'
		)`); err != nil {
		return fmt.Errorf("create semantic lexical index: %w", err)
	}
	return nil
}

func indexReceiptProjection(
	ctx context.Context,
	db databaseExecutor,
	tableName string,
	receiptID v1alpha1.ReceiptID,
	text string,
	privateTerms []string,
) error {
	projection, err := projectLexical(text, privateTerms)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO `+tableName+`(receipt_id, text, terms, ngrams) VALUES (?, ?, ?, ?)`,
		receiptID, text, projection.terms, projection.ngrams)
	return err
}

func indexSemanticProjection(
	ctx context.Context,
	db databaseExecutor,
	tableName string,
	recordID stewardv1alpha1.RecordID,
	revision uint64,
	text string,
	privateTerms []string,
) error {
	projection, err := projectLexical(text, privateTerms)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO `+tableName+`(record_id, revision, text, terms, ngrams) VALUES (?, ?, ?, ?, ?)`,
		recordID, revision, text, projection.terms, projection.ngrams)
	return err
}

func rebuildReceiptProjection(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	tableName string,
	privateTerms []string,
) error {
	rows, err := db.QueryContext(ctx,
		`SELECT receipt_id, text FROM receipts WHERE space_id = ? ORDER BY commit_sequence`, spaceID)
	if err != nil {
		return fmt.Errorf("read receipt projection source: %w", err)
	}
	type receipt struct {
		id   v1alpha1.ReceiptID
		text string
	}
	var receipts []receipt
	for rows.Next() {
		var item receipt
		if err := rows.Scan(&item.id, &item.text); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read receipt projection source: %w", err)
		}
		receipts = append(receipts, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read receipt projection source: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close receipt projection source: %w", err)
	}
	for _, item := range receipts {
		if err := indexReceiptProjection(ctx, db, tableName, item.id, item.text, privateTerms); err != nil {
			return fmt.Errorf("rebuild receipt lexical projection: %w", err)
		}
	}
	return nil
}

func rebuildSemanticProjection(
	ctx context.Context,
	db databaseExecutor,
	spaceID v1alpha1.SpaceID,
	tableName string,
	privateTerms []string,
) error {
	rows, err := db.QueryContext(ctx,
		`SELECT r.record_id, r.current_revision, v.text
		 FROM semantic_records r
		 JOIN semantic_revisions v ON v.record_id = r.record_id AND v.revision = r.current_revision
		 WHERE r.space_id = ? AND r.status = 'active'
		 ORDER BY r.record_id`, spaceID)
	if err != nil {
		return fmt.Errorf("read semantic projection source: %w", err)
	}
	type record struct {
		id       stewardv1alpha1.RecordID
		revision uint64
		text     string
	}
	var records []record
	for rows.Next() {
		var item record
		if err := rows.Scan(&item.id, &item.revision, &item.text); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read semantic projection source: %w", err)
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read semantic projection source: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close semantic projection source: %w", err)
	}
	for _, item := range records {
		if err := indexSemanticProjection(ctx, db, tableName, item.id, item.revision, item.text, privateTerms); err != nil {
			return fmt.Errorf("rebuild semantic lexical projection: %w", err)
		}
	}
	return nil
}
