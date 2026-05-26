package main

import (
	"reflect"
	"testing"
)

func TestCaseSelectionCLIFlagsKeepsTagState(t *testing.T) {
	selection := newCaseSelectionCLIFlags("case test", "active")
	if err := selection.parse([]string{"--tag", "smoke", "--tag", "regression"}); err != nil {
		t.Fatalf("parse case selection flags: %v", err)
	}

	got := selection.caseListFilter().Tags
	want := []string{"smoke", "regression"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %#v, want %#v", got, want)
	}
}
