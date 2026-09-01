package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestGovernanceMutationWireFixtures(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "correction",
			value: CorrectReceiptRequest{
				ReceiptID: "receipt-a", ReplacementText: "correct fact",
				Reason: "verified", IdempotencyKey: "correct-1",
			},
			want: `{"receipt_id":"receipt-a","replacement_text":"correct fact","reason":"verified","idempotency_key":"correct-1"}`,
		},
		{
			name: "deletion",
			value: DeleteReceiptRequest{
				ReceiptID: "receipt-a", Reason: "approved erasure", IdempotencyKey: "delete-1",
			},
			want: `{"receipt_id":"receipt-a","reason":"approved erasure","idempotency_key":"delete-1"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.want {
				t.Fatalf("wire fixture = %s, want %s", encoded, test.want)
			}
			for _, secretField := range []string{"authorization", "credential", "capability"} {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(encoded, &fields); err != nil {
					t.Fatal(err)
				}
				if _, exists := fields[secretField]; exists {
					t.Fatalf("management request body contains %q", secretField)
				}
			}
		})
	}
}

func TestManagementProtocolAndPathsAreVersioned(t *testing.T) {
	if ProtocolVersion != "memory.management.v1alpha1" {
		t.Fatalf("ProtocolVersion = %q", ProtocolVersion)
	}
	for name, path := range map[string]string{
		"search": LocalPathSearch, "trace": LocalPathTrace,
		"correct": LocalPathCorrect, "delete": LocalPathDelete,
	} {
		if len(path) <= len("/memory.management.v1alpha1/") || path[:len("/memory.management.v1alpha1/")] != "/memory.management.v1alpha1/" {
			t.Fatalf("%s path = %q", name, path)
		}
	}
}
