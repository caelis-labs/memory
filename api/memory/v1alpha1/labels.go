package v1alpha1

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxLabelsPerSet bounds one Runtime-selected logical partition.
	MaxLabelsPerSet = 16
	// MaxLabelBytes bounds one opaque label without prescribing product meaning.
	MaxLabelBytes = 128
)

// Label is one opaque, exact-match partition value. Memory never interprets
// product concepts such as Workspace or Bot from a label.
type Label string

// LabelSet identifies one logical partition inside an authorized Space. The
// empty set is the backward-compatible default partition.
type LabelSet []Label

// CanonicalLabelSet validates, copies, and sorts a LabelSet. Duplicate labels
// are rejected instead of silently changing a caller's requested partition.
func CanonicalLabelSet(labels LabelSet) (LabelSet, error) {
	if len(labels) > MaxLabelsPerSet {
		return nil, fmt.Errorf("label set exceeds %d labels", MaxLabelsPerSet)
	}
	canonical := append(LabelSet{}, labels...)
	for _, label := range canonical {
		value := string(label)
		if !utf8.ValidString(value) || value == "" || len(value) > MaxLabelBytes || strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("label must be valid UTF-8 and 1..%d bytes without surrounding whitespace", MaxLabelBytes)
		}
		for _, char := range value {
			if unicode.IsControl(char) {
				return nil, fmt.Errorf("label cannot contain control characters")
			}
		}
	}
	slices.Sort(canonical)
	for index := 1; index < len(canonical); index++ {
		if canonical[index] == canonical[index-1] {
			return nil, fmt.Errorf("label set cannot contain duplicates")
		}
	}
	return canonical, nil
}
