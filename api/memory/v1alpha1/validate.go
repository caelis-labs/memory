package v1alpha1

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

const (
	maxSourceScalarBytes  = 256
	maxSourceLabels       = 16
	maxLabelKeyBytes      = 64
	maxSourceContextBytes = 4 << 10
	maxRememberTextBytes  = 64 << 10
	maxRecallQueryBytes   = 8 << 10
	maxIdempotencyBytes   = 128
)

// Validate enforces the public SourceContext bounds.
func (s SourceContext) Validate() error {
	scalars := []struct {
		name  string
		value string
	}{
		{"actor_ref", s.ActorRef},
		{"session_ref", s.SessionRef},
		{"workspace_ref", s.WorkspaceRef},
		{"task_ref", s.TaskRef},
		{"tool_call_ref", s.ToolCallRef},
		{"source_type", s.SourceType},
	}
	for _, scalar := range scalars {
		if !utf8.ValidString(scalar.value) {
			return fmt.Errorf("%s must be valid UTF-8", scalar.name)
		}
		if len(scalar.value) > maxSourceScalarBytes {
			return fmt.Errorf("%s exceeds %d bytes", scalar.name, maxSourceScalarBytes)
		}
	}
	if len(s.ExtensionLabels) > maxSourceLabels {
		return fmt.Errorf("extension_labels exceeds %d entries", maxSourceLabels)
	}
	for key, value := range s.ExtensionLabels {
		if key == "" || !utf8.ValidString(key) || len(key) > maxLabelKeyBytes {
			return fmt.Errorf("extension label key must be valid UTF-8 and 1..%d bytes", maxLabelKeyBytes)
		}
		if !utf8.ValidString(value) || len(value) > maxSourceScalarBytes {
			return fmt.Errorf("extension label %q value exceeds %d bytes or is invalid UTF-8", key, maxSourceScalarBytes)
		}
	}
	normalized, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("normalize source context: %w", err)
	}
	if len(normalized) > maxSourceContextBytes {
		return fmt.Errorf("source_context exceeds %d normalized bytes", maxSourceContextBytes)
	}
	return nil
}

// Validate enforces the public Remember request bounds.
func (r RememberRequest) Validate() error {
	if !utf8.ValidString(r.Text) || len(r.Text) == 0 || len(r.Text) > maxRememberTextBytes {
		return fmt.Errorf("text must be valid UTF-8 and 1..%d bytes", maxRememberTextBytes)
	}
	if !utf8.ValidString(r.IdempotencyKey) || len(r.IdempotencyKey) == 0 || len(r.IdempotencyKey) > maxIdempotencyBytes {
		return fmt.Errorf("idempotency_key must be valid UTF-8 and 1..%d bytes", maxIdempotencyBytes)
	}
	if r.OccurredAt != nil {
		if _, err := r.OccurredAt.MarshalText(); err != nil {
			return fmt.Errorf("occurred_at must be an RFC 3339 timestamp: %w", err)
		}
	}
	return r.SourceContext.Validate()
}

// Validate enforces host-selected Recall budget bounds.
func (b RecallBudget) Validate() error {
	if b.MaxFragments < 1 || b.MaxFragments > 64 {
		return fmt.Errorf("max_fragments must be within 1..64")
	}
	if b.MaxBytes < MinRecallProjectionBytes || b.MaxBytes > 64<<10 {
		return fmt.Errorf("max_bytes must be within %d..65536", MinRecallProjectionBytes)
	}
	if b.DeadlineMS < 1 || b.DeadlineMS > 30_000 {
		return fmt.Errorf("deadline_ms must be within 1..30000")
	}
	return nil
}

// Validate enforces the public Recall request bounds.
func (r RecallRequest) Validate() error {
	if !utf8.ValidString(r.Query) || len(r.Query) == 0 || len(r.Query) > maxRecallQueryBytes {
		return fmt.Errorf("query must be valid UTF-8 and 1..%d bytes", maxRecallQueryBytes)
	}
	if err := r.SourceContext.Validate(); err != nil {
		return err
	}
	return r.Budget.Validate()
}
