package appliance

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// scheduleTimeLayout is the fixed-width UTC nanosecond text encoding used for
// every mutable scheduler field (steward_jobs.available_at and
// lease_expires_at). RFC3339Nano trims trailing zero fractions, so text order
// does not equal time order: ".12Z" sorts after ".123Z", and a whole second
// ("...00Z") sorts after "...00.001Z". A fixed nine-digit fraction makes
// lexicographic comparison exact, so `available_at <= now` and
// `lease_expires_at <= now` are correct without a julianday/millis round trip.
const scheduleTimeLayout = "2006-01-02T15:04:05.000000000Z"

// formatScheduleTime encodes one instant for a mutable scheduler field.
func formatScheduleTime(value time.Time) string {
	return value.UTC().Format(scheduleTimeLayout)
}

// normalizeStewardSchedule rewrites only the mutable scheduler text fields to
// the fixed-width encoding. Each value is parsed and re-emitted as the exact
// same instant, so no scheduling decision changes; immutable receipt/revision
// timestamps are never touched. It runs once inside the schema2 migration.
func normalizeStewardSchedule(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT job_id, available_at, lease_expires_at FROM steward_jobs ORDER BY job_id`)
	if err != nil {
		return fmt.Errorf("read Steward schedule: %w", err)
	}
	type storedSchedule struct {
		jobID     string
		available string
		expires   sql.NullString
	}
	var schedules []storedSchedule
	for rows.Next() {
		var schedule storedSchedule
		if err := rows.Scan(&schedule.jobID, &schedule.available, &schedule.expires); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan Steward schedule: %w", err)
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read Steward schedule: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close Steward schedule: %w", err)
	}
	for _, schedule := range schedules {
		available, err := parseTime(schedule.available)
		if err != nil {
			return fmt.Errorf("parse Steward available_at: %w", err)
		}
		var expires any
		if schedule.expires.Valid && schedule.expires.String != "" {
			parsed, err := parseTime(schedule.expires.String)
			if err != nil {
				return fmt.Errorf("parse Steward lease_expires_at: %w", err)
			}
			expires = formatScheduleTime(parsed)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE steward_jobs SET available_at = ?, lease_expires_at = ? WHERE job_id = ?`,
			formatScheduleTime(available), expires, schedule.jobID); err != nil {
			return fmt.Errorf("normalize Steward schedule: %w", err)
		}
	}
	return nil
}
