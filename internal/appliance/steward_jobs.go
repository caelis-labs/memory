package appliance

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const (
	minStewardLease    = time.Second
	maxStewardLease    = 10 * time.Minute
	maxStewardAttempts = 5
	stewardRetryBase   = time.Second
)

// StewardWork is one appliance-owned lease beside the model-visible request.
type StewardWork struct {
	Lease   StewardLease
	Attempt int
	Request stewardv1alpha1.WorkRequest
}

// StewardFailure transitions a leased Job either to a bounded retry or to a
// terminal failure. Code must be a non-sensitive stable classifier.
type StewardFailure struct {
	Code       string
	RetryAfter time.Duration
	Terminal   bool
}

// ClaimStewardJob reclaims expired leases and atomically leases the oldest
// available Job. Space and lease identity never enter the returned WorkRequest.
func (s *Store) ClaimStewardJob(ctx context.Context, leaseDuration time.Duration) (StewardWork, bool, error) {
	s.stewardJobMu.Lock()
	defer s.stewardJobMu.Unlock()
	if err := s.requireMutableGeneration(); err != nil {
		return StewardWork{}, false, err
	}
	if leaseDuration < minStewardLease || leaseDuration > maxStewardLease {
		return StewardWork{}, false, fmt.Errorf("Steward lease duration must be within %s..%s", minStewardLease, maxStewardLease)
	}
	formattedNow := formatTime(s.now().UTC())
	var available bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(
		 SELECT 1 FROM steward_jobs
		 WHERE (state = 'pending' AND available_at <= ?)
		 OR (state = 'leased' AND lease_expires_at <= ?)
		)`, formattedNow, formattedNow).Scan(&available); err != nil {
		return StewardWork{}, false, fmt.Errorf("inspect available Steward jobs: %w", err)
	}
	if !available {
		return StewardWork{}, false, nil
	}
	leaseToken, err := s.randomToken(32)
	if err != nil {
		return StewardWork{}, false, fmt.Errorf("create Steward lease: %w", err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		work, found, retry, err := s.claimStewardJob(ctx, leaseDuration, leaseToken)
		if err != nil || !retry {
			return work, found, err
		}
	}
	return StewardWork{}, false, nil
}

func (s *Store) claimStewardJob(
	ctx context.Context,
	leaseDuration time.Duration,
	leaseToken string,
) (StewardWork, bool, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StewardWork{}, false, false, fmt.Errorf("begin Steward claim: %w", err)
	}
	rollback := func(err error) (StewardWork, bool, bool, error) {
		_ = tx.Rollback()
		return StewardWork{}, false, false, err
	}
	now := s.now().UTC()
	formattedNow := formatTime(now)
	if _, err := tx.ExecContext(ctx,
		`UPDATE steward_jobs
		 SET state = 'failed', lease_expires_at = NULL, lease_token_digest = '',
		 terminal_error_code = 'attempts_exhausted', updated_at = ?
		 WHERE attempts >= ? AND (
		  (state = 'leased' AND lease_expires_at <= ?)
		  OR (state = 'pending' AND available_at <= ?)
		 )`, formattedNow, maxStewardAttempts, formattedNow, formattedNow); err != nil {
		return rollback(fmt.Errorf("fail exhausted Steward jobs: %w", err))
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE receipt_processing
		 SET state = 'failed', last_attempt_at = ?, terminal_error_code = 'attempts_exhausted'
		 WHERE receipt_id IN (
		  SELECT receipt_id FROM steward_jobs
		  WHERE state = 'failed' AND terminal_error_code = 'attempts_exhausted' AND updated_at = ?
		 )`, formattedNow, formattedNow); err != nil {
		return rollback(fmt.Errorf("fail exhausted receipt processing: %w", err))
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE steward_jobs
		 SET state = 'pending', lease_expires_at = NULL, lease_token_digest = '',
		 terminal_error_code = '', available_at = ?, updated_at = ?
		 WHERE state = 'leased' AND lease_expires_at <= ?`, formattedNow, formattedNow, formattedNow); err != nil {
		return rollback(fmt.Errorf("reclaim expired Steward jobs: %w", err))
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE receipt_processing
		 SET state = 'accepted', terminal_error_code = ''
		 WHERE receipt_id IN (SELECT receipt_id FROM steward_jobs WHERE state = 'pending')`); err != nil {
		return rollback(fmt.Errorf("requeue expired receipt processing: %w", err))
	}
	var jobID stewardv1alpha1.JobID
	var receiptID v1alpha1.ReceiptID
	var spaceID v1alpha1.SpaceID
	var profileID stewardv1alpha1.ProfileID
	var profileVersion uint64
	var attempts int
	err = tx.QueryRowContext(ctx,
		`SELECT job_id, receipt_id, space_id, profile_id, profile_version, attempts
		 FROM steward_jobs
		 WHERE state = 'pending' AND available_at <= ? AND attempts < ?
		 ORDER BY created_at, job_id LIMIT 1`, formattedNow, maxStewardAttempts).Scan(
		&jobID, &receiptID, &spaceID, &profileID, &profileVersion, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return StewardWork{}, false, false, fmt.Errorf("commit Steward reclaim: %w", err)
		}
		return StewardWork{}, false, false, nil
	}
	if err != nil {
		return rollback(fmt.Errorf("select Steward job: %w", err))
	}
	leaseExpiry := formatTime(now.Add(leaseDuration))
	leaseDigest := digestString(leaseToken)
	result, err := tx.ExecContext(ctx,
		`UPDATE steward_jobs
		 SET state = 'leased', attempts = attempts + 1, lease_expires_at = ?,
		 lease_token_digest = ?, updated_at = ?
		 WHERE job_id = ? AND state = 'pending' AND available_at <= ?`,
		leaseExpiry, leaseDigest, formattedNow, jobID, formattedNow)
	if err != nil {
		return rollback(fmt.Errorf("lease Steward job: %w", err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return rollback(fmt.Errorf("inspect Steward lease: %w", err))
	}
	if affected != 1 {
		_ = tx.Rollback()
		return StewardWork{}, false, true, nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE receipt_processing
		 SET state = 'processing', attempts = attempts + 1, last_attempt_at = ?, terminal_error_code = ''
		 WHERE receipt_id = ?`, formattedNow, receiptID); err != nil {
		return rollback(fmt.Errorf("start receipt semantic processing: %w", err))
	}
	profile, err := readStewardProfile(ctx, tx, profileID, profileVersion)
	if err != nil {
		return rollback(fmt.Errorf("read claimed Steward profile: %w", err))
	}
	request, err := readStewardWorkRequest(ctx, tx, profile.ProfileSpec, receiptID, spaceID)
	if err != nil {
		return rollback(err)
	}
	for {
		encodedSize, err := request.EncodedSize()
		if err != nil {
			return rollback(fmt.Errorf("encode Steward work request: %w", err))
		}
		if encodedSize <= profile.MaxInputBytes {
			break
		}
		if len(request.Records) == 0 {
			if err := failStewardJobTx(ctx, tx, jobID, receiptID, formattedNow, "input_too_large"); err != nil {
				return rollback(err)
			}
			if err := tx.Commit(); err != nil {
				return StewardWork{}, false, false, fmt.Errorf("commit oversized Steward job: %w", err)
			}
			return StewardWork{}, false, false, nil
		}
		request.Records = request.Records[:len(request.Records)-1]
	}
	if err := tx.Commit(); err != nil {
		return StewardWork{}, false, false, fmt.Errorf("commit Steward lease: %w", err)
	}
	return StewardWork{
		Lease: StewardLease{JobID: jobID, Token: leaseToken}, Attempt: attempts + 1, Request: request,
	}, true, false, nil
}

func readStewardWorkRequest(
	ctx context.Context,
	db databaseExecutor,
	profile stewardv1alpha1.ProfileSpec,
	receiptID v1alpha1.ReceiptID,
	spaceID v1alpha1.SpaceID,
) (stewardv1alpha1.WorkRequest, error) {
	request := stewardv1alpha1.WorkRequest{
		Protocol: stewardv1alpha1.ProtocolVersion, Profile: profile,
		Records: make([]stewardv1alpha1.RecordContext, 0, profile.MaxContextRecords),
	}
	var occurredAt sql.NullString
	var receivedAt string
	if err := db.QueryRowContext(ctx,
		`SELECT receipt_id, text, occurred_at, received_at FROM receipts WHERE receipt_id = ? AND space_id = ?`,
		receiptID, spaceID).Scan(&request.Receipt.ReceiptID, &request.Receipt.Text, &occurredAt, &receivedAt); err != nil {
		return stewardv1alpha1.WorkRequest{}, fmt.Errorf("read Steward receipt: %w", err)
	}
	var err error
	request.Receipt.ReceivedAt, err = parseTime(receivedAt)
	if err != nil {
		return stewardv1alpha1.WorkRequest{}, fmt.Errorf("parse Steward receipt time: %w", err)
	}
	if occurredAt.Valid {
		value, err := parseTime(occurredAt.String)
		if err != nil {
			return stewardv1alpha1.WorkRequest{}, fmt.Errorf("parse Steward occurrence time: %w", err)
		}
		request.Receipt.OccurredAt = &value
	}
	if profile.MaxContextRecords == 0 {
		return request, nil
	}
	rows, err := db.QueryContext(ctx,
		`SELECT r.record_id, r.current_revision, v.kind, v.text
		 FROM semantic_records r
		 JOIN semantic_revisions v ON v.record_id = r.record_id AND v.revision = r.current_revision
		 WHERE r.space_id = ? AND r.status = 'active'
		 ORDER BY r.updated_at DESC, r.record_id
		 LIMIT ?`, spaceID, profile.MaxContextRecords)
	if err != nil {
		return stewardv1alpha1.WorkRequest{}, fmt.Errorf("read Steward Record context: %w", err)
	}
	for rows.Next() {
		var record stewardv1alpha1.RecordContext
		if err := rows.Scan(&record.RecordID, &record.Revision, &record.Kind, &record.Text); err != nil {
			_ = rows.Close()
			return stewardv1alpha1.WorkRequest{}, fmt.Errorf("scan Steward Record context: %w", err)
		}
		request.Records = append(request.Records, record)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return stewardv1alpha1.WorkRequest{}, fmt.Errorf("read Steward Record context: %w", err)
	}
	if err := rows.Close(); err != nil {
		return stewardv1alpha1.WorkRequest{}, fmt.Errorf("close Steward Record context: %w", err)
	}
	for index := range request.Records {
		evidence, err := readSemanticEvidenceIDs(ctx, db, request.Records[index].RecordID, request.Records[index].Revision)
		if err != nil {
			return stewardv1alpha1.WorkRequest{}, fmt.Errorf("read Steward Record evidence: %w", err)
		}
		request.Records[index].EvidenceRefs = evidence
	}
	return request, nil
}

// FailStewardJob releases a valid lease to a delayed retry or terminal state.
func (s *Store) FailStewardJob(ctx context.Context, lease StewardLease, failure StewardFailure) error {
	s.stewardJobMu.Lock()
	defer s.stewardJobMu.Unlock()
	if err := s.requireMutableGeneration(); err != nil {
		return err
	}
	if lease.JobID == "" || lease.Token == "" || !validStewardFailureCode(failure.Code) {
		return fmt.Errorf("invalid Steward failure")
	}
	if failure.RetryAfter < 0 || failure.RetryAfter > time.Hour {
		return fmt.Errorf("Steward retry delay must be within 0..1h")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Steward failure: %w", err)
	}
	defer tx.Rollback()
	job, err := readStewardJob(ctx, tx, lease.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStewardLeaseLost
	}
	if err != nil {
		return fmt.Errorf("read failed Steward job: %w", err)
	}
	if job.state != "leased" || subtle.ConstantTimeCompare([]byte(job.leaseDigest), []byte(digestString(lease.Token))) != 1 {
		return ErrStewardLeaseLost
	}
	now := s.now().UTC()
	if !job.leaseExpiresAt.Valid {
		return ErrStewardLeaseLost
	}
	leaseExpiry, err := parseTime(job.leaseExpiresAt.String)
	if err != nil || !leaseExpiry.After(now) {
		return ErrStewardLeaseLost
	}
	formattedNow := formatTime(now)
	if failure.Terminal {
		if err := failStewardJobTx(ctx, tx, lease.JobID, job.receiptID, formattedNow, failure.Code); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE steward_jobs SET state = 'pending', available_at = ?, lease_expires_at = NULL,
			 lease_token_digest = '', terminal_error_code = '', updated_at = ? WHERE job_id = ?`,
			formatTime(now.Add(failure.RetryAfter)), formattedNow, lease.JobID); err != nil {
			return fmt.Errorf("retry Steward job: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE receipt_processing SET state = 'accepted', terminal_error_code = '' WHERE receipt_id = ?`,
			job.receiptID); err != nil {
			return fmt.Errorf("retry receipt semantic processing: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Steward failure: %w", err)
	}
	return nil
}

// ReportStewardFailure applies the appliance-owned retry ceiling and delay to a
// classified external Worker failure.
func (s *Store) ReportStewardFailure(ctx context.Context, request stewardv1alpha1.FailRequest) error {
	if err := s.requireMutableGeneration(); err != nil {
		return err
	}
	if request.Lease.JobID == "" || request.Lease.Token == "" || !validStewardFailureCode(request.Code) {
		return s.serviceError(v1alpha1.ErrorCodeInvalidArgument, "invalid Steward failure", false)
	}
	var attempts int
	if err := s.db.QueryRowContext(ctx, `SELECT attempts FROM steward_jobs WHERE job_id = ?`, request.Lease.JobID).Scan(&attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStewardLeaseLost
		}
		return fmt.Errorf("read Steward failure attempt: %w", err)
	}
	terminal := !request.Retryable || attempts >= maxStewardAttempts
	failure := StewardFailure{Code: request.Code, Terminal: terminal}
	if !terminal {
		failure.RetryAfter = stewardRetryDelay(attempts)
	}
	return s.FailStewardJob(ctx, request.Lease, failure)
}

func stewardRetryDelay(attempt int) time.Duration {
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 11 {
		shift = 11
	}
	delay := stewardRetryBase * time.Duration(1<<shift)
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func failStewardJobTx(
	ctx context.Context,
	tx *sql.Tx,
	jobID stewardv1alpha1.JobID,
	receiptID v1alpha1.ReceiptID,
	now string,
	code string,
) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE steward_jobs SET state = 'failed', lease_expires_at = NULL,
		 lease_token_digest = '', terminal_error_code = ?, updated_at = ? WHERE job_id = ?`,
		code, now, jobID); err != nil {
		return fmt.Errorf("fail Steward job: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE receipt_processing SET state = 'failed', last_attempt_at = ?, terminal_error_code = ? WHERE receipt_id = ?`,
		now, code, receiptID); err != nil {
		return fmt.Errorf("fail receipt semantic processing: %w", err)
	}
	return nil
}

func validStewardFailureCode(code string) bool {
	if !utf8.ValidString(code) || len(code) == 0 || len(code) > 64 || strings.TrimSpace(code) != code {
		return false
	}
	for _, char := range code {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}
