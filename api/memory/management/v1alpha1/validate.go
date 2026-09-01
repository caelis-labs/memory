package v1alpha1

import (
	"fmt"
	"unicode/utf8"
)

const (
	maxQueryBytes           = 8 << 10
	maxReplacementTextBytes = 64 << 10
	maxReasonBytes          = 1 << 10
	maxIdempotencyKeyBytes  = 128
)

func (r SearchReceiptsRequest) Validate() error {
	if !utf8.ValidString(r.Query) || len(r.Query) == 0 || len(r.Query) > maxQueryBytes {
		return fmt.Errorf("query must be valid UTF-8 and 1..%d bytes", maxQueryBytes)
	}
	if r.Limit < 1 || r.Limit > 100 {
		return fmt.Errorf("limit must be within 1..100")
	}
	return nil
}

func (r TraceReceiptRequest) Validate() error {
	if r.ReceiptID == "" {
		return fmt.Errorf("receipt_id is required")
	}
	return nil
}

func (r CorrectReceiptRequest) Validate() error {
	if r.ReceiptID == "" {
		return fmt.Errorf("receipt_id is required")
	}
	if !utf8.ValidString(r.ReplacementText) || len(r.ReplacementText) == 0 || len(r.ReplacementText) > maxReplacementTextBytes {
		return fmt.Errorf("replacement_text must be valid UTF-8 and 1..%d bytes", maxReplacementTextBytes)
	}
	return validateMutation(r.Reason, r.IdempotencyKey)
}

func (r DeleteReceiptRequest) Validate() error {
	if r.ReceiptID == "" {
		return fmt.Errorf("receipt_id is required")
	}
	return validateMutation(r.Reason, r.IdempotencyKey)
}

func validateMutation(reason, key string) error {
	if !utf8.ValidString(reason) || len(reason) == 0 || len(reason) > maxReasonBytes {
		return fmt.Errorf("reason must be valid UTF-8 and 1..%d bytes", maxReasonBytes)
	}
	if !utf8.ValidString(key) || len(key) == 0 || len(key) > maxIdempotencyKeyBytes {
		return fmt.Errorf("idempotency_key must be valid UTF-8 and 1..%d bytes", maxIdempotencyKeyBytes)
	}
	return nil
}
