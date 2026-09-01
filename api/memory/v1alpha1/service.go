package v1alpha1

import (
	"context"
	"errors"
	"fmt"
)

// DataPlane is the complete Core Profile service boundary.
type DataPlane interface {
	Remember(context.Context, CallAuthorization, RememberRequest) (RememberResponse, error)
	Recall(context.Context, CallAuthorization, RecallRequest) (RecallResponse, error)
	GetReceiptStatus(context.Context, CallAuthorization, GetReceiptStatusRequest) (ReceiptStatus, error)
}

// ErrorCode is a stable machine-readable failure class.
type ErrorCode string

const (
	ErrorCodeInvalidArgument       ErrorCode = "invalid_argument"
	ErrorCodeUnauthorized          ErrorCode = "unauthorized"
	ErrorCodeIncompatible          ErrorCode = "incompatible"
	ErrorCodeNotFound              ErrorCode = "not_found"
	ErrorCodeConflict              ErrorCode = "conflict"
	ErrorCodeDeadline              ErrorCode = "deadline"
	ErrorCodeUnavailable           ErrorCode = "unavailable"
	ErrorCodeUnknownOutcome        ErrorCode = "unknown_outcome"
	ErrorCodeStaleConsistencyToken ErrorCode = "stale_consistency_token"
	ErrorCodeInternal              ErrorCode = "internal"
)

// ErrorDetail is one bounded typed error attribute.
type ErrorDetail struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ServiceError is the stable error envelope returned by a DataPlane.
type ServiceError struct {
	Code         ErrorCode     `json:"code"`
	Message      string        `json:"message"`
	Retryable    bool          `json:"retryable"`
	RequestID    string        `json:"request_id"`
	RetryAfterMS int           `json:"retry_after_ms,omitempty"`
	Details      []ErrorDetail `json:"details,omitempty"`
}

func (e *ServiceError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("memory %s: %s", e.Code, e.Message)
}

// ErrorCodeOf returns the stable code for a ServiceError.
func ErrorCodeOf(err error) (ErrorCode, bool) {
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		return "", false
	}
	return serviceErr.Code, true
}

// IsCode reports whether err contains the given stable service code.
func IsCode(err error, code ErrorCode) bool {
	got, ok := ErrorCodeOf(err)
	return ok && got == code
}
