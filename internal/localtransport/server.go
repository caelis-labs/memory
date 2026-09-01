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

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/internal/appliance"
)

const (
	AdminPathBootstrap = "/admin/m1/bootstrap"
	AdminPathIssue     = "/admin/m1/issue-capability"
	AdminPathInspect   = "/admin/m1/inspect"
	AdminPathRebuild   = "/admin/m1/rebuild-fts"
	AdminPathRevoke    = "/admin/m1/revoke-grant"
	AdminPathRotate    = "/admin/m1/rotate-issuer"
	maxRequestBytes    = 128 << 10
)

// Handler returns the complete M1 local transport without adding request logs.
func Handler(store *appliance.Store) http.Handler {
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
	mux.HandleFunc(AdminPathBootstrap, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input appliance.BootstrapRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.Bootstrap(request.Context(), input)
		writeAdmin(writer, response, err)
	}))
	mux.HandleFunc(AdminPathIssue, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input appliance.IssueCapabilityRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.IssueCapability(request.Context(), input)
		writeAdmin(writer, response, err)
	}))
	mux.HandleFunc(AdminPathInspect, admin(store, http.MethodGet, func(writer http.ResponseWriter, request *http.Request) {
		response, err := store.Inspect(request.Context())
		writeAdmin(writer, response, err)
	}))
	mux.HandleFunc(AdminPathRebuild, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		err := store.RebuildFTS(request.Context())
		writeAdmin(writer, map[string]bool{"rebuilt": err == nil}, err)
	}))
	mux.HandleFunc(AdminPathRevoke, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			GrantID v1alpha1.GrantID `json:"grant_id"`
		}
		if !decodeJSON(writer, request, &input) {
			return
		}
		err := store.RevokeGrant(request.Context(), input.GrantID)
		writeAdmin(writer, map[string]bool{"revoked": err == nil}, err)
	}))
	mux.HandleFunc(AdminPathRotate, admin(store, http.MethodPost, func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			PrincipalRef string `json:"principal_ref"`
		}
		if !decodeJSON(writer, request, &input) {
			return
		}
		response, err := store.RotateIssuerCredential(request.Context(), input.PrincipalRef)
		writeAdmin(writer, response, err)
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
			writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "management authorization required"})
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

func writeAdmin(writer http.ResponseWriter, response any, err error) {
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response)
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
