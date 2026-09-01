package appliance

import (
	"fmt"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// TestM5FixedRetrievalCorpus200 freezes the RC retrieval set. Changing its
// markers, expected rank, or provenance requires a new release-candidate run.
func TestM5FixedRetrievalCorpus200(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	t.Cleanup(func() { _ = store.Close() })
	type corpusCase struct {
		name      string
		query     string
		text      string
		receiptID v1alpha1.ReceiptID
		recordID  stewardv1alpha1.RecordID
	}
	corpus := make([]corpusCase, 0, 200)
	for index := range 100 {
		marker := fmt.Sprintf("receiptmarker%03d", index)
		text := "Release receipt " + marker + " evidence."
		receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: text, IdempotencyKey: fmt.Sprintf("m5-corpus-receipt-%03d", index),
		})
		if err != nil {
			t.Fatal(err)
		}
		corpus = append(corpus, corpusCase{
			name: fmt.Sprintf("receipt/%03d", index), query: marker, text: text, receiptID: receipt.ReceiptID,
		})
	}
	for index := range 100 {
		marker := fmt.Sprintf("semanticmarker%03d", index)
		receipt, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: fmt.Sprintf("rawrelease%03d", index), IdempotencyKey: fmt.Sprintf("m5-corpus-semantic-%03d", index),
		})
		if err != nil {
			t.Fatal(err)
		}
		lease := leaseStewardReceipt(t, store, receipt.ReceiptID, stewardv1alpha1.JobID(fmt.Sprintf("job-m5-corpus-%03d", index)))
		text := "Release semantic " + marker + " assertion."
		result, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
			Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: text,
			EvidenceRefs: []v1alpha1.ReceiptID{receipt.ReceiptID},
		})
		if err != nil {
			t.Fatal(err)
		}
		corpus = append(corpus, corpusCase{
			name: fmt.Sprintf("semantic/%03d", index), query: marker, text: text,
			receiptID: receipt.ReceiptID, recordID: result.RecordID,
		})
	}
	for _, test := range corpus {
		t.Run(test.name, func(t *testing.T) {
			response, err := store.Recall(t.Context(), auth, testRecall(test.query, ""))
			if err != nil {
				t.Fatal(err)
			}
			if response.Degraded || len(response.Fragments) != 1 {
				t.Fatalf("Recall = %+v", response)
			}
			fragment := response.Fragments[0]
			if fragment.Text != test.text || len(fragment.EvidenceRefs) != 1 || fragment.EvidenceRefs[0] != test.receiptID {
				t.Fatalf("fragment = %+v", fragment)
			}
			if test.recordID == "" && len(fragment.RecordRefs) != 0 {
				t.Fatalf("receipt fragment has Record refs %+v", fragment.RecordRefs)
			}
			if test.recordID != "" && (len(fragment.RecordRefs) != 1 || fragment.RecordRefs[0] != string(test.recordID)) {
				t.Fatalf("semantic fragment Record refs = %+v", fragment.RecordRefs)
			}
		})
	}
}
