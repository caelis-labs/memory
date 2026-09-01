package appliance

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func (s *Store) PutStewardProfile(
	ctx context.Context,
	request managementv1alpha1.PutStewardProfileRequest,
) (managementv1alpha1.PutStewardProfileResponse, error) {
	if err := s.requireMutableGeneration(); err != nil {
		return managementv1alpha1.PutStewardProfileResponse{}, err
	}
	if err := request.Profile.Validate(); err != nil {
		return managementv1alpha1.PutStewardProfileResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, err.Error(), false)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return managementv1alpha1.PutStewardProfileResponse{}, s.databaseError("begin Steward profile", err)
	}
	defer tx.Rollback()
	existing, err := readStewardProfile(ctx, tx, request.Profile.ProfileID, request.Profile.Version)
	if err == nil {
		if existing.ProfileSpec != request.Profile {
			return managementv1alpha1.PutStewardProfileResponse{}, s.serviceError(v1alpha1.ErrorCodeConflict, "Steward profile version is immutable", false)
		}
		return managementv1alpha1.PutStewardProfileResponse{Profile: existing}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return managementv1alpha1.PutStewardProfileResponse{}, s.databaseError("read Steward profile", err)
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO steward_profiles(
		 profile_id, version, provider_ref, model, system_prompt, max_context_records,
		 max_input_bytes, max_output_bytes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		request.Profile.ProfileID, request.Profile.Version, request.Profile.ProviderRef, request.Profile.Model,
		request.Profile.SystemPrompt, request.Profile.MaxContextRecords, request.Profile.MaxInputBytes,
		request.Profile.MaxOutputBytes, formatTime(now)); err != nil {
		return managementv1alpha1.PutStewardProfileResponse{}, s.databaseError("store Steward profile", err)
	}
	if err := tx.Commit(); err != nil {
		return managementv1alpha1.PutStewardProfileResponse{}, s.serviceError(v1alpha1.ErrorCodeUnknownOutcome, "Steward profile commit outcome is unknown; inspect before retry", true)
	}
	return managementv1alpha1.PutStewardProfileResponse{
		Profile: stewardv1alpha1.Profile{ProfileSpec: request.Profile, CreatedAt: now}, Created: true,
	}, nil
}

func (s *Store) BindStewardProfile(
	ctx context.Context,
	request managementv1alpha1.BindStewardProfileRequest,
) (managementv1alpha1.BindStewardProfileResponse, error) {
	if err := s.requireMutableGeneration(); err != nil {
		return managementv1alpha1.BindStewardProfileResponse{}, err
	}
	if request.ProfileID == "" || request.Version == 0 || len(request.SpaceIDs) == 0 || len(request.SpaceIDs) > 256 {
		return managementv1alpha1.BindStewardProfileResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, "profile, version, and 1..256 Spaces are required", false)
	}
	if hasDuplicateSpaceIDs(request.SpaceIDs) {
		return managementv1alpha1.BindStewardProfileResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, "Steward binding Spaces must be unique", false)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return managementv1alpha1.BindStewardProfileResponse{}, s.databaseError("begin Steward binding", err)
	}
	defer tx.Rollback()
	if _, err := readStewardProfile(ctx, tx, request.ProfileID, request.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return managementv1alpha1.BindStewardProfileResponse{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "Steward profile not found", false)
		}
		return managementv1alpha1.BindStewardProfileResponse{}, s.databaseError("read Steward binding profile", err)
	}
	now := formatTime(s.now().UTC())
	for _, spaceID := range request.SpaceIDs {
		if exists, err := rowExists(ctx, tx, `SELECT EXISTS(SELECT 1 FROM spaces WHERE id = ?)`, spaceID); err != nil {
			return managementv1alpha1.BindStewardProfileResponse{}, s.databaseError("read Steward binding Space", err)
		} else if !exists {
			return managementv1alpha1.BindStewardProfileResponse{}, s.serviceError(v1alpha1.ErrorCodeNotFound, "Steward binding Space not found", false)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO space_steward_bindings(space_id, profile_id, profile_version, bound_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(space_id) DO UPDATE SET
			 profile_id = excluded.profile_id, profile_version = excluded.profile_version, bound_at = excluded.bound_at`,
			spaceID, request.ProfileID, request.Version, now); err != nil {
			return managementv1alpha1.BindStewardProfileResponse{}, s.databaseError("store Steward binding", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return managementv1alpha1.BindStewardProfileResponse{}, s.serviceError(v1alpha1.ErrorCodeUnknownOutcome, "Steward binding commit outcome is unknown; inspect before retry", true)
	}
	return managementv1alpha1.BindStewardProfileResponse{Bound: len(request.SpaceIDs)}, nil
}

func (s *Store) DisableSteward(
	ctx context.Context,
	request managementv1alpha1.DisableStewardRequest,
) (managementv1alpha1.DisableStewardResponse, error) {
	if err := s.requireMutableGeneration(); err != nil {
		return managementv1alpha1.DisableStewardResponse{}, err
	}
	if len(request.SpaceIDs) == 0 || len(request.SpaceIDs) > 256 || hasDuplicateSpaceIDs(request.SpaceIDs) {
		return managementv1alpha1.DisableStewardResponse{}, s.serviceError(v1alpha1.ErrorCodeInvalidArgument, "1..256 unique Spaces are required", false)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return managementv1alpha1.DisableStewardResponse{}, s.databaseError("begin Steward disable", err)
	}
	defer tx.Rollback()
	response := managementv1alpha1.DisableStewardResponse{}
	now := formatTime(s.now().UTC())
	for _, spaceID := range request.SpaceIDs {
		result, err := tx.ExecContext(ctx, `DELETE FROM space_steward_bindings WHERE space_id = ?`, spaceID)
		if err != nil {
			return managementv1alpha1.DisableStewardResponse{}, s.databaseError("remove Steward binding", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return managementv1alpha1.DisableStewardResponse{}, s.databaseError("inspect Steward binding removal", err)
		}
		response.Disabled += int(affected)
		if _, err := tx.ExecContext(ctx,
			`UPDATE receipt_processing
			 SET state = 'failed', last_attempt_at = ?, terminal_error_code = 'steward_disabled'
			 WHERE receipt_id IN (
				 SELECT receipt_id FROM steward_jobs WHERE space_id = ? AND state IN ('pending', 'leased')
			 )`, now, spaceID); err != nil {
			return managementv1alpha1.DisableStewardResponse{}, s.databaseError("fail disabled Steward receipts", err)
		}
		result, err = tx.ExecContext(ctx,
			`UPDATE steward_jobs
			 SET state = 'failed', lease_expires_at = NULL, lease_token_digest = '',
			 terminal_error_code = 'steward_disabled', updated_at = ?
			 WHERE space_id = ? AND state IN ('pending', 'leased')`, now, spaceID)
		if err != nil {
			return managementv1alpha1.DisableStewardResponse{}, s.databaseError("cancel disabled Steward jobs", err)
		}
		affected, err = result.RowsAffected()
		if err != nil {
			return managementv1alpha1.DisableStewardResponse{}, s.databaseError("inspect disabled Steward jobs", err)
		}
		response.CanceledJobs += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return managementv1alpha1.DisableStewardResponse{}, s.serviceError(v1alpha1.ErrorCodeUnknownOutcome, "Steward disable commit outcome is unknown; inspect before retry", true)
	}
	return response, nil
}

func (s *Store) GetStewardConfiguration(ctx context.Context) (managementv1alpha1.StewardConfiguration, error) {
	var result managementv1alpha1.StewardConfiguration
	rows, err := s.db.QueryContext(ctx,
		`SELECT profile_id, version, provider_ref, model, system_prompt, max_context_records,
		 max_input_bytes, max_output_bytes, created_at
		 FROM steward_profiles ORDER BY profile_id, version`)
	if err != nil {
		return result, s.databaseError("list Steward profiles", err)
	}
	for rows.Next() {
		var profile stewardv1alpha1.Profile
		var createdAt string
		if err := rows.Scan(&profile.ProfileID, &profile.Version, &profile.ProviderRef, &profile.Model,
			&profile.SystemPrompt, &profile.MaxContextRecords, &profile.MaxInputBytes, &profile.MaxOutputBytes, &createdAt); err != nil {
			_ = rows.Close()
			return managementv1alpha1.StewardConfiguration{}, s.databaseError("read Steward profile", err)
		}
		profile.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			_ = rows.Close()
			return managementv1alpha1.StewardConfiguration{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored Steward profile time is invalid", false)
		}
		result.Profiles = append(result.Profiles, profile)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return managementv1alpha1.StewardConfiguration{}, s.databaseError("list Steward profiles", err)
	}
	if err := rows.Close(); err != nil {
		return managementv1alpha1.StewardConfiguration{}, s.databaseError("close Steward profiles", err)
	}
	rows, err = s.db.QueryContext(ctx,
		`SELECT space_id, profile_id, profile_version, bound_at
		 FROM space_steward_bindings ORDER BY space_id`)
	if err != nil {
		return result, s.databaseError("list Steward bindings", err)
	}
	defer rows.Close()
	for rows.Next() {
		var binding managementv1alpha1.StewardBinding
		var boundAt string
		if err := rows.Scan(&binding.SpaceID, &binding.ProfileID, &binding.ProfileVersion, &boundAt); err != nil {
			return managementv1alpha1.StewardConfiguration{}, s.databaseError("read Steward binding", err)
		}
		binding.BoundAt, err = parseTime(boundAt)
		if err != nil {
			return managementv1alpha1.StewardConfiguration{}, s.serviceError(v1alpha1.ErrorCodeInternal, "stored Steward binding time is invalid", false)
		}
		result.Bindings = append(result.Bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return managementv1alpha1.StewardConfiguration{}, s.databaseError("list Steward bindings", err)
	}
	return result, nil
}

func readStewardProfile(
	ctx context.Context,
	db databaseExecutor,
	profileID stewardv1alpha1.ProfileID,
	version uint64,
) (stewardv1alpha1.Profile, error) {
	var profile stewardv1alpha1.Profile
	var createdAt string
	err := db.QueryRowContext(ctx,
		`SELECT profile_id, version, provider_ref, model, system_prompt, max_context_records,
		 max_input_bytes, max_output_bytes, created_at
		 FROM steward_profiles WHERE profile_id = ? AND version = ?`, profileID, version).Scan(
		&profile.ProfileID, &profile.Version, &profile.ProviderRef, &profile.Model,
		&profile.SystemPrompt, &profile.MaxContextRecords, &profile.MaxInputBytes,
		&profile.MaxOutputBytes, &createdAt)
	if err != nil {
		return stewardv1alpha1.Profile{}, err
	}
	profile.CreatedAt, err = parseTime(createdAt)
	return profile, err
}

func (s *Store) enqueueStewardJob(
	ctx context.Context,
	tx *sql.Tx,
	receiptID v1alpha1.ReceiptID,
	spaceID v1alpha1.SpaceID,
	now time.Time,
) error {
	var profileID stewardv1alpha1.ProfileID
	var profileVersion uint64
	err := tx.QueryRowContext(ctx,
		`SELECT profile_id, profile_version FROM space_steward_bindings WHERE space_id = ?`, spaceID).Scan(
		&profileID, &profileVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	jobID := stewardv1alpha1.JobID("job-" + digestString(string(receiptID))[:32])
	formattedNow := formatTime(now.UTC())
	_, err = tx.ExecContext(ctx,
		`INSERT INTO steward_jobs(
		 job_id, receipt_id, space_id, profile_id, profile_version, state, attempts,
		 available_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'pending', 0, ?, ?, ?)`,
		jobID, receiptID, spaceID, profileID, profileVersion, formattedNow, formattedNow, formattedNow)
	return err
}

func hasDuplicateSpaceIDs(spaceIDs []v1alpha1.SpaceID) bool {
	seen := make([]v1alpha1.SpaceID, 0, len(spaceIDs))
	for _, spaceID := range spaceIDs {
		if spaceID == "" || slices.Contains(seen, spaceID) {
			return true
		}
		seen = append(seen, spaceID)
	}
	return false
}
