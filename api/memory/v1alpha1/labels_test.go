package v1alpha1

import (
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalLabelSet(t *testing.T) {
	got, err := CanonicalLabelSet(LabelSet{"workspace:demo", "identity:bot-a"})
	if err != nil {
		t.Fatal(err)
	}
	want := LabelSet{"identity:bot-a", "workspace:demo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical labels = %v, want %v", got, want)
	}
	empty, err := CanonicalLabelSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("canonical empty LabelSet = %#v", empty)
	}
}

func TestCanonicalLabelSetRejectsAmbiguity(t *testing.T) {
	tests := []LabelSet{
		{"duplicate", "duplicate"},
		{" surrounding "},
		{"line\nbreak"},
		{Label(strings.Repeat("x", MaxLabelBytes+1))},
	}
	for _, labels := range tests {
		if _, err := CanonicalLabelSet(labels); err == nil {
			t.Fatalf("CanonicalLabelSet(%q) succeeded", labels)
		}
	}
}
