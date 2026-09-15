package v1alpha1

import (
	"fmt"
	"strings"
	"unicode/utf8"

	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func bounded(s string, n int, required bool) bool {
	return utf8.ValidString(s) && len(s) <= n && (!required || s != "") && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\r\n\x00")
}
func (s Source) Validate() error {
	for _, v := range []string{s.Producer, s.EventID, s.Revision, s.Fragment, s.Subject} {
		if !bounded(v, 256, true) {
			return fmt.Errorf("source identity and subject must be 1..256 bounded UTF-8 bytes")
		}
	}
	if !bounded(s.FactKey, 128, false) {
		return fmt.Errorf("invalid source fact key")
	}
	switch s.Role {
	case RoleUserQuote, RoleConfirmation, RoleObservation, RoleInference:
	default:
		return fmt.Errorf("unsupported source role")
	}
	return nil
}
func (r SubmitEvidenceRequest) Validate() error {
	if err := safelyValidateEvidence(r); err != nil {
		return err
	}
	if len(r.Mutations) > MaxMutations {
		return fmt.Errorf("at most %d mutations are supported", MaxMutations)
	}
	targets := map[string]bool{}
	for _, m := range r.Mutations {
		if err := m.Validate(); err != nil {
			return err
		}
		if m.Subject != r.Source.Subject {
			return fmt.Errorf("mutation subject must equal trusted source subject")
		}
		if m.TargetRecordID != "" && targets[m.TargetRecordID] {
			return fmt.Errorf("batch cannot target a record twice")
		}
		targets[m.TargetRecordID] = true
		if m.Transition != TransitionEstablish && r.Source.Role != RoleConfirmation {
			return fmt.Errorf("lifecycle edits require structured confirmation")
		}
	}
	return nil
}
func safelyValidateEvidence(r SubmitEvidenceRequest) error {
	if err := r.Source.Validate(); err != nil {
		return err
	}
	return (memory.RememberRequest{Text: r.Text, OccurredAt: r.OccurredAt, IdempotencyKey: r.IdempotencyKey}).Validate()
}
func (m Mutation) Validate() error {
	if !bounded(m.Subject, 256, true) || !bounded(m.Key, 128, false) || !bounded(m.TargetRecordID, 256, false) {
		return fmt.Errorf("invalid subject, key or target")
	}
	if !utf8.ValidString(m.Text) || strings.TrimSpace(m.Text) == "" || len(m.Text) > 32<<10 {
		return fmt.Errorf("fact text must be 1..32768 UTF-8 bytes")
	}
	switch m.Transition {
	case TransitionEstablish:
		if m.TargetRecordID != "" || m.ExpectedRevision != 0 {
			return fmt.Errorf("establish cannot target a record")
		}
	case TransitionChange, TransitionException, TransitionCorrect, TransitionConfirm, TransitionDeny:
		if m.TargetRecordID == "" || m.ExpectedRevision == 0 {
			return fmt.Errorf("lifecycle edit requires target and revision")
		}
	default:
		return fmt.Errorf("unsupported fact transition")
	}
	if m.ValidFrom != nil && m.ValidFrom.IsZero() || m.ValidUntil != nil && m.ValidUntil.IsZero() {
		return fmt.Errorf("zero effective time is invalid")
	}
	if m.ValidFrom != nil && m.ValidUntil != nil && !m.ValidUntil.After(*m.ValidFrom) {
		return fmt.Errorf("valid_until must follow valid_from")
	}
	if m.Transition == TransitionChange && m.ValidFrom == nil {
		return fmt.Errorf("change requires explicit effective time")
	}
	if m.Transition == TransitionException && (m.ValidFrom == nil || m.ValidUntil == nil || m.Key == "") {
		return fmt.Errorf("exception requires fact key and finite interval")
	}
	if len(m.Conditions) > 8 {
		return fmt.Errorf("at most 8 exact conditions")
	}
	seen := map[string]bool{}
	for _, c := range m.Conditions {
		if !bounded(c.Key, 128, true) || !bounded(c.Value, 256, true) || seen[c.Key] {
			return fmt.Errorf("conditions require unique bounded keys and exact values")
		}
		seen[c.Key] = true
	}
	return nil
}
func (b Budget) Validate() error {
	if b.MaxFacts < 1 || b.MaxFacts > 64 || b.MaxBytes < 512 || b.MaxBytes > 1<<20 {
		return fmt.Errorf("budget requires 1..64 facts and 512..1048576 bytes")
	}
	return nil
}
func (r ReadRequest) Validate() error {
	if !bounded(r.Subject, 256, true) || !bounded(r.Key, 128, false) || !utf8.ValidString(r.Query) || len(r.Query) > 4096 {
		return fmt.Errorf("invalid subject, key or query")
	}
	if r.AsOf != nil && r.AsOf.IsZero() {
		return fmt.Errorf("zero as_of is invalid")
	}
	if len(r.Context) > 8 {
		return fmt.Errorf("at most 8 context values")
	}
	for k, v := range r.Context {
		if !bounded(k, 128, true) || !bounded(v, 256, true) {
			return fmt.Errorf("invalid exact context")
		}
	}
	return r.Budget.Validate()
}
func (r HistoryRequest) Validate() error {
	if !bounded(r.Subject, 256, true) || !bounded(r.RecordID, 256, true) {
		return fmt.Errorf("subject and record required")
	}
	return r.Budget.Validate()
}
func (p IngestionPolicy) Validate() error {
	if !bounded(string(p.Scope.SpaceID), 256, true) || !bounded(p.Producer, 256, true) || !bounded(p.Subject, 256, true) || !bounded(p.Reason, 512, false) {
		return fmt.Errorf("policy requires exact scope, producer, subject and bounded reason")
	}
	_, err := memory.CanonicalLabelSet(p.Scope.Labels)
	return err
}
