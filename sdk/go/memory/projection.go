package memory

import (
	"encoding/json"
	"errors"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

var ErrProjectionBudgetExceeded = errors.New("model-visible Recall exceeds byte budget")

// RememberToolInput is the complete model-visible Remember input.
type RememberToolInput struct {
	Text string `json:"text"`
}

// RecallToolInput is the complete model-visible Recall input.
type RecallToolInput struct {
	Query string `json:"query"`
}

// ProjectRemember returns the exact model-visible success result.
func ProjectRemember(response v1alpha1.RememberResponse) ([]byte, error) {
	return json.Marshal(struct {
		Accepted bool `json:"accepted"`
	}{Accepted: response.Accepted})
}

// ProjectRecall returns only ordered fragment text to the model and enforces a
// hard bound over the final JSON encoding, including escaping and the envelope.
func ProjectRecall(response v1alpha1.RecallResponse, maxBytes int) ([]byte, error) {
	fragments := make([]string, len(response.Fragments))
	for i, fragment := range response.Fragments {
		fragments[i] = fragment.Text
	}
	projected, err := json.Marshal(struct {
		Fragments []string `json:"fragments"`
	}{Fragments: fragments})
	if err != nil {
		return nil, err
	}
	if len(projected) > maxBytes {
		return nil, ErrProjectionBudgetExceeded
	}
	return projected, nil
}
