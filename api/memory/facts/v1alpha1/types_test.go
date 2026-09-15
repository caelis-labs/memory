package v1alpha1

import (
	"testing"
	"time"
)

func TestEvidenceValidationTrustAndBoundedLifecycle(t *testing.T) {
	now := time.Now()
	base := SubmitEvidenceRequest{Source: Source{Producer: "host", EventID: "event", Revision: "1", Fragment: "0", Subject: "person", Role: RoleConfirmation}, Text: "text", IdempotencyKey: "effect", Mutations: []Mutation{{Transition: TransitionEstablish, Subject: "person", Key: "key", Text: "fact"}}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*SubmitEvidenceRequest)
	}{
		{"unknown trust", func(r *SubmitEvidenceRequest) { r.Source.Role = "model_says_confirmed" }},
		{"wrong subject", func(r *SubmitEvidenceRequest) { r.Mutations[0].Subject = "friend" }},
		{"implicit change time", func(r *SubmitEvidenceRequest) {
			r.Mutations[0].Transition = TransitionChange
			r.Mutations[0].TargetRecordID = "record"
			r.Mutations[0].ExpectedRevision = 1
		}},
		{"unbounded exception", func(r *SubmitEvidenceRequest) {
			r.Mutations[0].Transition = TransitionException
			r.Mutations[0].TargetRecordID = "record"
			r.Mutations[0].ExpectedRevision = 1
			r.Mutations[0].ValidFrom = &now
		}},
		{"duplicate condition", func(r *SubmitEvidenceRequest) {
			r.Mutations[0].Conditions = []Condition{{"location", "home"}, {"location", "office"}}
		}},
		{"too many edits", func(r *SubmitEvidenceRequest) {
			for len(r.Mutations) <= MaxMutations {
				r.Mutations = append(r.Mutations, r.Mutations[0])
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			r.Mutations = append([]Mutation(nil), base.Mutations...)
			tc.edit(&r)
			if r.Validate() == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}
func TestReadValidationIsBounded(t *testing.T) {
	good := ReadRequest{Subject: "person", Budget: Budget{MaxFacts: 8, MaxBytes: 4096}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	good.Context = map[string]string{"expression": ""}
	if good.Validate() == nil {
		t.Fatal("empty exact condition accepted")
	}
	if (Budget{MaxFacts: 65, MaxBytes: 4096}).Validate() == nil {
		t.Fatal("unbounded facts accepted")
	}
	if (IngestionPolicy{Scope: Scope{SpaceID: "space"}, Producer: "host", Subject: "person"}).Validate() != nil {
		t.Fatal("valid policy rejected")
	}
}
