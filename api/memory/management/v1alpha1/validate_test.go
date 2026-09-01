package v1alpha1

import "testing"

func TestGovernanceRequestValidation(t *testing.T) {
	if err := (SearchReceiptsRequest{Query: "fact", Limit: 20}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (SearchReceiptsRequest{Query: "fact"}).Validate(); err == nil {
		t.Fatal("search accepted a zero limit")
	}
	if err := (CorrectReceiptRequest{ReceiptID: "receipt-a", ReplacementText: "correct", Reason: "operator correction", IdempotencyKey: "correct-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (DeleteReceiptRequest{ReceiptID: "receipt-a", Reason: "erasure request", IdempotencyKey: "delete-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (DeleteReceiptRequest{ReceiptID: "receipt-a", IdempotencyKey: "delete-1"}).Validate(); err == nil {
		t.Fatal("delete accepted an empty audit reason")
	}
}
