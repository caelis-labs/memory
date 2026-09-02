package appliance

import (
	"encoding/json"
	"fmt"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

const emptyLabelSetDigest = "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"

type storedLabelSet struct {
	labels  v1alpha1.LabelSet
	encoded string
	digest  string
}

func normalizeLabelSet(labels v1alpha1.LabelSet) (storedLabelSet, error) {
	canonical, err := v1alpha1.CanonicalLabelSet(labels)
	if err != nil {
		return storedLabelSet{}, err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return storedLabelSet{}, err
	}
	return storedLabelSet{
		labels:  canonical,
		encoded: string(encoded),
		digest:  digestString(string(encoded)),
	}, nil
}

func decodeStoredLabelSet(encoded, expectedDigest string) (storedLabelSet, error) {
	var labels v1alpha1.LabelSet
	if err := json.Unmarshal([]byte(encoded), &labels); err != nil {
		return storedLabelSet{}, fmt.Errorf("decode LabelSet: %w", err)
	}
	stored, err := normalizeLabelSet(labels)
	if err != nil {
		return storedLabelSet{}, fmt.Errorf("validate LabelSet: %w", err)
	}
	if stored.encoded != encoded || stored.digest != expectedDigest {
		return storedLabelSet{}, fmt.Errorf("stored LabelSet is not canonical")
	}
	return stored, nil
}
