package appliance

import (
	"encoding/json"
	"testing"
	"time"

	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
)

func TestFactsHistoryPagesWithoutChargingDiscardedBackground(t *testing.T) {
	now := time.Now()
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	r := factRequest("first", "coffee", &now)
	first := submitFact(t, s, auth, r)
	later := now.Add(time.Hour)
	r = factRequest("second", "tea", &later)
	r.Mutations[0].Transition = facts.TransitionChange
	r.Mutations[0].TargetRecordID = first.Facts[0].RecordID
	r.Mutations[0].ExpectedRevision = 1
	submitFact(t, s, auth, r)
	request := facts.HistoryRequest{Subject: "user:a", RecordID: first.Facts[0].RecordID, Budget: facts.Budget{MaxFacts: 1, MaxBytes: 8192}}
	page, err := s.FactHistory(t.Context(), auth, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Facts) != 1 || page.NextRevision != 1 || !page.Truncated || page.Background != "" {
		t.Fatalf("first page=%+v", page)
	}
	encoded, _ := json.Marshal(page.Facts)
	if page.BytesUsed != len(encoded) {
		t.Fatalf("history charged nonexistent background: %+v", page)
	}
	request.AfterRevision = page.NextRevision
	page, err = s.FactHistory(t.Context(), auth, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Facts) != 1 || page.NextRevision != 2 || page.Truncated || page.Facts[0].Text != "tea" {
		t.Fatalf("second page=%+v", page)
	}
}
func TestFactsControlledAliasesDoNotMatchEnglishSubstrings(t *testing.T) {
	s, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer s.Close()
	submitFact(t, s, auth, factRequest("drink", "coffee", nil))
	r := factRead()
	r.Query = "team"
	wantFact(t, s, auth, r, "")
	r.Query = "which beverage"
	wantFact(t, s, auth, r, "coffee")
	r.Query = "用户饮品"
	wantFact(t, s, auth, r, "coffee")
}
