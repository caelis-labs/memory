// Package localtransport owns memoryd's local HTTP composition and private M1
// management endpoints.
package localtransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

const maxRequestBytes = 128 << 10

// ServiceInfo is immutable build identity reported by the compatibility
// handshake. The artifact digest remains owned by the pre-launch manifest.
type ServiceInfo struct {
	Version  string
	Revision string
}

// Handler returns the complete local transport without adding request logs.
func Handler(store *appliance.Store, serviceInfo ...ServiceInfo) http.Handler {
	info := ServiceInfo{Version: "devel", Revision: "unknown"}
	if len(serviceInfo) > 0 {
		if serviceInfo[0].Version != "" {
			info.Version = serviceInfo[0].Version
		}
		if serviceInfo[0].Revision != "" {
			info.Revision = serviceInfo[0].Revision
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(v1alpha1.LocalPathHealth, method(http.MethodGet, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok", "protocol": v1alpha1.LocalTransportProtocol})
	}))
	mux.HandleFunc(v1alpha1.LocalPathReady, method(http.MethodGet, func(writer http.ResponseWriter, request *http.Request) {
		if err := store.Ready(request.Context()); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "protocol": v1alpha1.LocalTransportProtocol})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready", "protocol": v1alpha1.LocalTransportProtocol})
	}))
	mux.HandleFunc(v1alpha1.LocalPathCompatibility, method(http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input v1alpha1.CompatibilityRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		if input != v1alpha1.CurrentCompatibilityRequest() {
			writeDataPlane(writer, nil, &v1alpha1.ServiceError{
				Code: v1alpha1.ErrorCodeIncompatible, Message: "requested Memory compatibility profile is unavailable", RequestID: "local-compatibility",
			})
			return
		}
		writeJSON(writer, http.StatusOK, v1alpha1.CompatibilityResponse{
			Protocol: v1alpha1.LocalTransportProtocol, APIVersion: v1alpha1.ProtocolVersion,
			CoreProfile: v1alpha1.CoreProfile, ServiceVersion: info.Version,
			BuildRevision: info.Revision, SchemaVersion: appliance.CurrentSchemaVersion,
		})
	}))
	mux.HandleFunc(v1alpha1.LocalPathIssue, method(http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		credential := bearer(request.Header.Get("Authorization"))
		if credential == "" {
			writeDataPlane(writer, nil, &v1alpha1.ServiceError{
				Code: v1alpha1.ErrorCodeUnauthorized, Message: "issuer authorization is required", RequestID: "local-issuer",
			})
			return
		}
		var input v1alpha1.CapabilityIssueRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		if err := validateCapabilityIssue(input); err != nil {
			writeDataPlane(writer, nil, &v1alpha1.ServiceError{
				Code: v1alpha1.ErrorCodeInvalidArgument, Message: err.Error(), RequestID: "local-issuer",
			})
			return
		}
		capability, err := store.IssueCapability(request.Context(), appliance.IssueCapabilityRequest{
			Authorization: appliance.IssuerAuthorization{PrincipalRef: input.PrincipalRef, Credential: credential},
			GrantRef:      input.GrantRef, ActorRef: input.ActorRef, Audience: input.Audience,
			Operations: input.Operations, TTLSeconds: input.TTLSeconds,
		})
		if err != nil {
			if errors.Is(err, appliance.ErrCapabilityIssueInvalid) {
				writeDataPlane(writer, nil, &v1alpha1.ServiceError{
					Code: v1alpha1.ErrorCodeInvalidArgument, Message: "capability issue request is invalid", RequestID: "local-issuer",
				})
				return
			}
			if !errors.Is(err, appliance.ErrCapabilityIssueUnauthorized) {
				writeDataPlane(writer, nil, &v1alpha1.ServiceError{
					Code: v1alpha1.ErrorCodeUnavailable, Message: "capability issuer is unavailable", Retryable: true, RequestID: "local-issuer",
				})
				return
			}
			writeDataPlane(writer, nil, &v1alpha1.ServiceError{
				Code: v1alpha1.ErrorCodeUnauthorized, Message: "issuer is not authorized for the requested Runtime capability", RequestID: "local-issuer",
			})
			return
		}
		writeJSON(writer, http.StatusOK, v1alpha1.RuntimeCapability{Token: capability.Token, ExpiresAt: capability.ExpiresAt})
	}))
	mux.HandleFunc(v1alpha1.LocalPathRemember, method(http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input v1alpha1.RememberRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.Remember(request.Context(), callAuthorization(request), input)
		writeDataPlane(writer, response, err)
	}))
	mux.HandleFunc(v1alpha1.LocalPathRecall, method(http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input v1alpha1.RecallRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.Recall(request.Context(), callAuthorization(request), input)
		writeDataPlane(writer, response, err)
	}))
	mux.HandleFunc(v1alpha1.LocalPathReceiptStatus, method(http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input v1alpha1.GetReceiptStatusRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.GetReceiptStatus(request.Context(), callAuthorization(request), input)
		writeDataPlane(writer, response, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathBootstrap, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input managementv1alpha1.BootstrapRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.Bootstrap(request.Context(), input)
		writeManagement(writer, response, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathInspect, admin(store, http.MethodGet, func(writer http.ResponseWriter, request *http.Request) {
		response, err := store.Inspect(request.Context())
		writeManagement(writer, response, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathSearch, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input managementv1alpha1.SearchReceiptsRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.SearchReceipts(request.Context(), input)
		writeManagement(writer, response, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathTrace, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input managementv1alpha1.TraceReceiptRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.TraceReceipt(request.Context(), input)
		writeManagement(writer, response, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathCorrect, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input managementv1alpha1.CorrectReceiptRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.CorrectReceipt(request.Context(), input)
		writeManagement(writer, response, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathDelete, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input managementv1alpha1.DeleteReceiptRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.DeleteReceipt(request.Context(), input)
		writeManagement(writer, response, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathRebuild, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		err := store.RebuildFTS(request.Context())
		writeManagement(writer, managementv1alpha1.RebuildFTSResponse{Rebuilt: err == nil}, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathRevokeGrant, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input managementv1alpha1.RevokeGrantRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		err := store.RevokeGrant(request.Context(), input.GrantID)
		writeManagement(writer, managementv1alpha1.RevokeGrantResponse{Revoked: err == nil}, err)
	}))
	mux.HandleFunc(managementv1alpha1.LocalPathRotateIssuer, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input managementv1alpha1.RotateIssuerRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.RotateIssuerCredential(request.Context(), input.PrincipalRef)
		writeManagement(writer, response, err)
	}))
	return mux
}

func method(expected string, handler http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != expected {
			writer.Header().Set("Allow", expected)
			writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		handler(writer, request)
	}
}

func admin(store *appliance.Store, expected string, handler http.HandlerFunc) http.HandlerFunc {
	return method(expected, func(writer http.ResponseWriter, request *http.Request) {
		if !store.AuthenticateManagement(bearer(request.Header.Get("Authorization"))) {
			writeDataPlane(writer, nil, &v1alpha1.ServiceError{
				Code: v1alpha1.ErrorCodeUnauthorized, Message: "management authorization required", RequestID: "local-management",
			})
			return
		}
		handler(writer, request)
	})
}

func callAuthorization(request *http.Request) v1alpha1.CallAuthorization {
	return v1alpha1.CallAuthorization{
		Capability: v1alpha1.CapabilityToken(bearer(request.Header.Get("Authorization"))),
		ActorRef:   request.Header.Get(v1alpha1.LocalHeaderActor),
		Audience:   v1alpha1.Audience(request.Header.Get(v1alpha1.LocalHeaderAudience)),
	}
}

func bearer(value string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix))
}

func validateCapabilityIssue(input v1alpha1.CapabilityIssueRequest) error {
	if input.PrincipalRef == "" || input.GrantRef == "" || input.ActorRef == "" {
		return fmt.Errorf("principal, Grant, and actor references are required")
	}
	if input.Audience != v1alpha1.AudiencePrivate && input.Audience != v1alpha1.AudienceShared {
		return fmt.Errorf("unsupported Runtime audience")
	}
	if input.TTLSeconds < 1 || input.TTLSeconds > int64((24*time.Hour)/time.Second) {
		return fmt.Errorf("capability TTL must be between 1 second and 24 hours")
	}
	if len(input.Operations) == 0 {
		return fmt.Errorf("at least one operation is required")
	}
	seen := make(map[v1alpha1.Operation]struct{}, len(input.Operations))
	for _, operation := range input.Operations {
		switch operation {
		case v1alpha1.OperationRemember, v1alpha1.OperationRecall, v1alpha1.OperationReceiptStatus:
		default:
			return fmt.Errorf("unsupported capability operation")
		}
		if _, duplicate := seen[operation]; duplicate {
			return fmt.Errorf("capability operations must be unique")
		}
		seen[operation] = struct{}{}
	}
	return nil
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, output any) bool {
	decoder := json.NewDecoder(io.LimitReader(request.Body, maxRequestBytes))
	if err := decoder.Decode(output); err != nil {
		writeDataPlane(writer, nil, &v1alpha1.ServiceError{
			Code: v1alpha1.ErrorCodeInvalidArgument, Message: "request JSON is invalid", RequestID: "local-decode",
		})
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeDataPlane(writer, nil, &v1alpha1.ServiceError{
			Code: v1alpha1.ErrorCodeInvalidArgument, Message: "request contains trailing JSON", RequestID: "local-decode",
		})
		return false
	}
	return true
}

func writeDataPlane(writer http.ResponseWriter, response any, err error) {
	if err == nil {
		writeJSON(writer, http.StatusOK, response)
		return
	}
	var serviceErr *v1alpha1.ServiceError
	if !errors.As(err, &serviceErr) {
		serviceErr = &v1alpha1.ServiceError{
			Code: v1alpha1.ErrorCodeInternal, Message: "memoryd failed to process the request", RequestID: "local-internal",
		}
	}
	writeJSON(writer, statusForCode(serviceErr.Code), serviceErr)
}

func writeManagement(writer http.ResponseWriter, response any, err error) {
	if err == nil {
		writeJSON(writer, http.StatusOK, response)
		return
	}
	var serviceErr *v1alpha1.ServiceError
	if !errors.As(err, &serviceErr) {
		serviceErr = &v1alpha1.ServiceError{
			Code: v1alpha1.ErrorCodeInternal, Message: "memoryd failed to process the management request", RequestID: "local-management",
		}
	}
	writeJSON(writer, statusForCode(serviceErr.Code), serviceErr)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func statusForCode(code v1alpha1.ErrorCode) int {
	switch code {
	case v1alpha1.ErrorCodeInvalidArgument:
		return http.StatusBadRequest
	case v1alpha1.ErrorCodeUnauthorized:
		return http.StatusUnauthorized
	case v1alpha1.ErrorCodeNotFound:
		return http.StatusNotFound
	case v1alpha1.ErrorCodeConflict:
		return http.StatusConflict
	case v1alpha1.ErrorCodeIncompatible:
		return http.StatusUpgradeRequired
	case v1alpha1.ErrorCodeDeadline:
		return http.StatusGatewayTimeout
	case v1alpha1.ErrorCodeUnavailable, v1alpha1.ErrorCodeUnknownOutcome:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// ShutdownServer drains one HTTP server with the caller's deadline.
func ShutdownServer(ctx context.Context, server *http.Server) error {
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown local transport: %w", err)
	}
	return nil
}
