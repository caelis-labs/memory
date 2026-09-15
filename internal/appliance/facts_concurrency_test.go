package appliance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	memory "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestFactsCanceledCallsReturnVersionedDeadline(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.SubmitEvidence(ctx, auth, factRequest("canceled", "coffee", nil))
	if !memory.IsCode(err, memory.ErrorCodeDeadline) {
		t.Fatalf("submit error = %v", err)
	}
	_, err = s.ReadFacts(ctx, auth, factRead())
	if !memory.IsCode(err, memory.ErrorCodeDeadline) {
		t.Fatalf("read error = %v", err)
	}
	_, err = s.Changes(ctx, auth, facts.ChangesRequest{Limit: 1})
	if !memory.IsCode(err, memory.ErrorCodeDeadline) {
		t.Fatalf("changes error = %v", err)
	}
}

func TestFactsConcurrentSourceRetryDoesNotMutateCallerRequest(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	// Isolate concurrent normalization/dedup from race-instrumented dictionary
	// cold-start time, which can exceed the production SQLite busy timeout.
	if _, err := sharedBaseSegmenter(); err != nil {
		t.Fatal(err)
	}
	request := factRequest("concurrent-source", "coffee", nil)
	request.Mutations[0].Conditions = []facts.Condition{{Key: "z", Value: "last"}, {Key: "a", Value: "first"}}
	before, _ := json.Marshal(request)
	ctx := t.Context()
	start := make(chan struct{})
	type result struct {
		out facts.SubmitEvidenceResponse
		err error
	}
	results := make(chan result, 8)
	for range cap(results) {
		go func() { <-start; out, err := s.SubmitEvidence(ctx, auth, request); results <- result{out, err} }()
	}
	close(start)
	var id string
	for range cap(results) {
		r := <-results
		if r.err != nil {
			t.Error(r.err)
			continue
		}
		if !r.out.Accepted {
			t.Errorf("not accepted: %+v", r.out)
			continue
		}
		if id == "" {
			id = string(r.out.ReceiptID)
		} else if id != string(r.out.ReceiptID) {
			t.Error("source retry forked receipts")
		}
	}
	after, _ := json.Marshal(request)
	if string(before) != string(after) {
		t.Fatal("normalization mutated caller request")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM receipts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("receipt count=%d", count)
	}
}
