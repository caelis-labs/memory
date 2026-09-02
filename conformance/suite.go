// Package conformance provides reusable M0 semantic tests for memory.v1alpha1
// implementations. Durable crash/restart conformance is added in M1 and is not
// implied by this suite.
package conformance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

type Fixture struct {
	Service v1alpha1.DataPlane

	BotAPrivate        v1alpha1.CallAuthorization
	BotAPrivateRenewed v1alpha1.CallAuthorization
	BotAPrivateLabeled v1alpha1.CallAuthorization
	BotAPrivateOther   v1alpha1.CallAuthorization
	BotBPrivate        v1alpha1.CallAuthorization
	SharedA            v1alpha1.CallAuthorization
	SharedB            v1alpha1.CallAuthorization
	RecallOnly         v1alpha1.CallAuthorization
	Expired            v1alpha1.CallAuthorization
	Revoked            v1alpha1.CallAuthorization
	PrivateSharedWrite v1alpha1.CallAuthorization

	BotAPrivateSpace v1alpha1.SpaceID
	BotBPrivateSpace v1alpha1.SpaceID
	SharedSpace      v1alpha1.SpaceID
	CandidateReads   func(v1alpha1.SpaceID) uint64
	SetAvailable     func(bool)
}

type Factory func(*testing.T) Fixture

// RunSemantic executes the non-durable M0 semantic contract against a fresh
// fixture per case.
func RunSemantic(t *testing.T, factory Factory) {
	t.Helper()
	t.Run("GoldenPathPrivateAndShared", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		privateWrite, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, rememberRequest(
			"commit does not authorize push",
			"golden-private",
		))
		if err != nil {
			t.Fatal(err)
		}
		privateRecall, err := fixture.Service.Recall(ctx, fixture.BotAPrivate, recallRequest(
			"commit push",
			privateWrite.ConsistencyToken,
		))
		if err != nil {
			t.Fatal(err)
		}
		assertFragmentText(t, privateRecall, "commit does not authorize push")

		botBRecall, err := fixture.Service.Recall(ctx, fixture.BotBPrivate, recallRequest("commit push", ""))
		if err != nil {
			t.Fatal(err)
		}
		if len(botBRecall.Fragments) != 0 {
			t.Fatalf("Bot-B recalled Bot-A private memory: %+v", botBRecall.Fragments)
		}

		if _, err := fixture.Service.Remember(ctx, fixture.SharedA, rememberRequest(
			"the project uses Go",
			"golden-shared",
		)); err != nil {
			t.Fatal(err)
		}
		for name, auth := range map[string]v1alpha1.CallAuthorization{
			"Bot-A": fixture.BotAPrivate,
			"Bot-B": fixture.BotBPrivate,
		} {
			t.Run(name, func(t *testing.T) {
				response, err := fixture.Service.Recall(ctx, auth, recallRequest("project Go", ""))
				if err != nil {
					t.Fatal(err)
				}
				assertFragmentText(t, response, "the project uses Go")
			})
		}
	})

	t.Run("AuthorizationFailsClosed", func(t *testing.T) {
		cases := map[string]struct {
			auth      func(Fixture) v1alpha1.CallAuthorization
			operation string
		}{
			"missing": {
				auth:      func(Fixture) v1alpha1.CallAuthorization { return v1alpha1.CallAuthorization{} },
				operation: "recall",
			},
			"wrong actor": {
				auth: func(f Fixture) v1alpha1.CallAuthorization {
					auth := f.BotAPrivate
					auth.ActorRef = "actor-intruder"
					return auth
				},
				operation: "recall",
			},
			"wrong audience": {
				auth: func(f Fixture) v1alpha1.CallAuthorization {
					auth := f.BotAPrivate
					auth.Audience = v1alpha1.AudienceShared
					return auth
				},
				operation: "recall",
			},
			"wrong operation": {
				auth:      func(f Fixture) v1alpha1.CallAuthorization { return f.RecallOnly },
				operation: "remember",
			},
			"expired": {
				auth:      func(f Fixture) v1alpha1.CallAuthorization { return f.Expired },
				operation: "recall",
			},
			"revoked": {
				auth:      func(f Fixture) v1alpha1.CallAuthorization { return f.Revoked },
				operation: "recall",
			},
			"private to shared write": {
				auth:      func(f Fixture) v1alpha1.CallAuthorization { return f.PrivateSharedWrite },
				operation: "remember",
			},
		}
		for name, testCase := range cases {
			t.Run(name, func(t *testing.T) {
				fixture := factory(t)
				var err error
				if testCase.operation == "remember" {
					_, err = fixture.Service.Remember(context.Background(), testCase.auth(fixture), rememberRequest("fact", "auth-case"))
				} else {
					_, err = fixture.Service.Recall(context.Background(), testCase.auth(fixture), recallRequest("fact", ""))
				}
				assertCode(t, err, v1alpha1.ErrorCodeUnauthorized)
			})
		}
	})

	t.Run("IdempotencySurvivesCapabilityRenewal", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		request := rememberRequest("stable effect", "idempotency-renewal")
		first, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, request)
		if err != nil {
			t.Fatal(err)
		}
		second, err := fixture.Service.Remember(ctx, fixture.BotAPrivateRenewed, request)
		if err != nil {
			t.Fatal(err)
		}
		if first.ReceiptID != second.ReceiptID || !second.DeduplicatedRetry {
			t.Fatalf("renewed retry = %+v, first = %+v", second, first)
		}
	})

	t.Run("LabelSetPartitionsOneSpace", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		remembered, err := fixture.Service.Remember(ctx, fixture.BotAPrivateLabeled, rememberRequest(
			"demo workspace uses React",
			"label-partition",
		))
		if err != nil {
			t.Fatal(err)
		}
		own, err := fixture.Service.Recall(ctx, fixture.BotAPrivateLabeled, recallRequest("React", remembered.ConsistencyToken))
		if err != nil {
			t.Fatal(err)
		}
		assertFragmentText(t, own, "demo workspace uses React")

		other, err := fixture.Service.Recall(ctx, fixture.BotAPrivateOther, recallRequest("React", ""))
		if err != nil {
			t.Fatal(err)
		}
		if len(other.Fragments) != 0 {
			t.Fatalf("other LabelSet recalled partitioned memory: %+v", other.Fragments)
		}
		_, err = fixture.Service.GetReceiptStatus(ctx, fixture.BotAPrivateOther, v1alpha1.GetReceiptStatusRequest{ReceiptID: remembered.ReceiptID})
		assertCode(t, err, v1alpha1.ErrorCodeNotFound)
		_, err = fixture.Service.Recall(ctx, fixture.BotAPrivateOther, recallRequest("React", remembered.ConsistencyToken))
		assertCode(t, err, v1alpha1.ErrorCodeUnauthorized)
		_, err = fixture.Service.Remember(ctx, fixture.BotAPrivateOther, rememberRequest(
			"demo workspace uses React",
			"label-partition",
		))
		assertCode(t, err, v1alpha1.ErrorCodeConflict)
	})

	t.Run("IdempotencyConflict", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		if _, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, rememberRequest("first", "conflict-key")); err != nil {
			t.Fatal(err)
		}
		_, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, rememberRequest("changed", "conflict-key"))
		assertCode(t, err, v1alpha1.ErrorCodeConflict)
		shared, err := fixture.Service.Remember(ctx, fixture.SharedA, rememberRequest("first", "conflict-key"))
		if err != nil {
			t.Fatalf("same key in an isolated Space: %v", err)
		}
		if shared.ReceiptID == "" {
			t.Fatal("isolated Space write did not create a receipt")
		}
	})

	t.Run("EqualTextWithDifferentKeysPreservesEvidence", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		first, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, rememberRequest("repeated evidence", "evidence-1"))
		if err != nil {
			t.Fatal(err)
		}
		second, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, rememberRequest("repeated evidence", "evidence-2"))
		if err != nil {
			t.Fatal(err)
		}
		if first.ReceiptID == second.ReceiptID {
			t.Fatal("different effect identities collapsed into one receipt")
		}
		response, err := fixture.Service.Recall(ctx, fixture.BotAPrivate, recallRequest("repeated evidence", ""))
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Fragments) != 2 {
			t.Fatalf("Recall returned %d fragments, want 2", len(response.Fragments))
		}
	})

	t.Run("ConsistencyTokenIsNotAuthority", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		remembered, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, rememberRequest("private causal fact", "causal-private"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = fixture.Service.Recall(ctx, fixture.SharedA, recallRequest("private", remembered.ConsistencyToken))
		assertCode(t, err, v1alpha1.ErrorCodeUnauthorized)
		_, err = fixture.Service.Recall(ctx, fixture.BotAPrivate, recallRequest("private", "unknown-generation-token"))
		assertCode(t, err, v1alpha1.ErrorCodeStaleConsistencyToken)
	})

	t.Run("SharedRecallNeverReadsPrivateCandidates", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		if _, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, rememberRequest("needle private", "isolation-private")); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.Service.Remember(ctx, fixture.SharedA, rememberRequest("needle shared", "isolation-shared")); err != nil {
			t.Fatal(err)
		}
		beforePrivate := fixture.CandidateReads(fixture.BotAPrivateSpace)
		beforeShared := fixture.CandidateReads(fixture.SharedSpace)
		response, err := fixture.Service.Recall(ctx, fixture.SharedB, recallRequest("needle", ""))
		if err != nil {
			t.Fatal(err)
		}
		if fixture.CandidateReads(fixture.BotAPrivateSpace) != beforePrivate {
			t.Fatal("shared Recall entered Bot-A private candidate source")
		}
		if fixture.CandidateReads(fixture.SharedSpace) <= beforeShared {
			t.Fatal("shared Recall did not enter the shared candidate source")
		}
		if len(response.Fragments) != 1 || response.Fragments[0].SpaceClass != v1alpha1.SpaceClassShared {
			t.Fatalf("shared Recall returned unexpected fragments: %+v", response.Fragments)
		}
	})

	t.Run("ReceiptStatusUsesReadableSpace", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		remembered, err := fixture.Service.Remember(ctx, fixture.BotAPrivate, rememberRequest("status fact", "status-key"))
		if err != nil {
			t.Fatal(err)
		}
		status, err := fixture.Service.GetReceiptStatus(ctx, fixture.BotAPrivate, v1alpha1.GetReceiptStatusRequest{ReceiptID: remembered.ReceiptID})
		if err != nil {
			t.Fatal(err)
		}
		if status.State != v1alpha1.ProcessingStateAccepted {
			t.Fatalf("status = %q, want accepted", status.State)
		}
		_, err = fixture.Service.GetReceiptStatus(ctx, fixture.BotBPrivate, v1alpha1.GetReceiptStatusRequest{ReceiptID: remembered.ReceiptID})
		assertCode(t, err, v1alpha1.ErrorCodeNotFound)
		_, err = fixture.Service.GetReceiptStatus(ctx, fixture.RecallOnly, v1alpha1.GetReceiptStatusRequest{ReceiptID: remembered.ReceiptID})
		assertCode(t, err, v1alpha1.ErrorCodeUnauthorized)
	})

	t.Run("RecallBudgetsAreEnforced", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		if _, err := fixture.Service.Remember(ctx, fixture.SharedA, rememberRequest("\n\n\n\nsearchable memory", "budget-key")); err != nil {
			t.Fatal(err)
		}
		request := recallRequest("searchable", "")
		request.Budget.MaxBytes = v1alpha1.MinRecallProjectionBytes + 8
		response, err := fixture.Service.Recall(ctx, fixture.SharedA, request)
		if err != nil {
			t.Fatal(err)
		}
		encoded := modelVisibleRecall(t, response)
		if len(encoded) > request.Budget.MaxBytes {
			t.Fatalf("model-visible Recall uses %d bytes, budget is %d: %s", len(encoded), request.Budget.MaxBytes, encoded)
		}
		if len(response.Fragments) != 1 || !response.Truncated {
			t.Fatalf("budgeted Recall = %+v", response)
		}
	})

	t.Run("FragmentCountBudgetIsEnforced", func(t *testing.T) {
		fixture := factory(t)
		ctx := context.Background()
		for i, text := range []string{"countable first", "countable second"} {
			if _, err := fixture.Service.Remember(ctx, fixture.SharedA, rememberRequest(text, "count-key-"+string(rune('a'+i)))); err != nil {
				t.Fatal(err)
			}
		}
		request := recallRequest("countable", "")
		request.Budget.MaxFragments = 1
		response, err := fixture.Service.Recall(ctx, fixture.SharedA, request)
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Fragments) != 1 || !response.Truncated {
			t.Fatalf("fragment-count Recall = %+v", response)
		}
	})

	t.Run("SourceContextAndBudgetBounds", func(t *testing.T) {
		fixture := factory(t)
		request := rememberRequest("bounded fact", "bounded-key")
		request.SourceContext.ActorRef = strings.Repeat("a", 257)
		_, err := fixture.Service.Remember(context.Background(), fixture.BotAPrivate, request)
		assertCode(t, err, v1alpha1.ErrorCodeInvalidArgument)

		recall := recallRequest("bounded", "")
		recall.Budget.MaxFragments = 0
		_, err = fixture.Service.Recall(context.Background(), fixture.BotAPrivate, recall)
		assertCode(t, err, v1alpha1.ErrorCodeInvalidArgument)
	})

	t.Run("CanceledRecallReturnsDeadline", func(t *testing.T) {
		fixture := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := fixture.Service.Recall(ctx, fixture.SharedA, recallRequest("anything", ""))
		assertCode(t, err, v1alpha1.ErrorCodeDeadline)
	})

	t.Run("EmptyRecallIsSuccessful", func(t *testing.T) {
		fixture := factory(t)
		response, err := fixture.Service.Recall(context.Background(), fixture.SharedA, recallRequest("absent", ""))
		if err != nil {
			t.Fatal(err)
		}
		if response.Fragments == nil || len(response.Fragments) != 0 {
			t.Fatalf("empty Recall = %+v, want non-nil empty fragments", response.Fragments)
		}
		fixture.SetAvailable(false)
		_, err = fixture.Service.Recall(context.Background(), fixture.SharedA, recallRequest("absent", ""))
		assertCode(t, err, v1alpha1.ErrorCodeUnavailable)
	})
}

func modelVisibleRecall(t *testing.T, response v1alpha1.RecallResponse) []byte {
	t.Helper()
	fragments := make([]string, len(response.Fragments))
	for i, fragment := range response.Fragments {
		fragments[i] = fragment.Text
	}
	encoded, err := json.Marshal(struct {
		Fragments []string `json:"fragments"`
	}{Fragments: fragments})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func rememberRequest(text, key string) v1alpha1.RememberRequest {
	return v1alpha1.RememberRequest{
		Text:           text,
		IdempotencyKey: key,
		SourceContext: v1alpha1.SourceContext{
			ActorRef:    "untrusted-audit-value",
			SessionRef:  "session-reference",
			ToolCallRef: "tool-call-reference",
		},
	}
}

func recallRequest(query string, token v1alpha1.ConsistencyToken) v1alpha1.RecallRequest {
	return v1alpha1.RecallRequest{
		Query:               query,
		MinConsistencyToken: token,
		Budget: v1alpha1.RecallBudget{
			MaxFragments: 8,
			MaxBytes:     4096,
			DeadlineMS:   int((2 * time.Second) / time.Millisecond),
		},
	}
}

func assertFragmentText(t *testing.T, response v1alpha1.RecallResponse, text string) {
	t.Helper()
	for _, fragment := range response.Fragments {
		if fragment.Text == text {
			if len(fragment.EvidenceRefs) == 0 {
				t.Fatalf("fragment %q has no evidence", text)
			}
			return
		}
	}
	t.Fatalf("Recall fragments %+v do not contain %q", response.Fragments, text)
}

func assertCode(t *testing.T, err error, code v1alpha1.ErrorCode) {
	t.Helper()
	if !v1alpha1.IsCode(err, code) {
		t.Fatalf("error = %v, want code %q", err, code)
	}
}
