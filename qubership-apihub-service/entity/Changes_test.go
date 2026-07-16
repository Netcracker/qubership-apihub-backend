package entity

import (
	"testing"
)

func TestVersionComparisonEntityGetChanges_refsComparedAsSets(t *testing.T) {
	tests := []struct {
		name       string
		oldRefs    []string
		newRefs    []string
		wantChange bool
	}{
		{name: "same refs in the same order", oldRefs: []string{"a", "b"}, newRefs: []string{"a", "b"}, wantChange: false},
		{name: "same refs in a different order", oldRefs: []string{"b", "a"}, newRefs: []string{"a", "b"}, wantChange: false},
		{name: "different refs", oldRefs: []string{"a", "b"}, newRefs: []string{"a", "c"}, wantChange: true},
		{name: "ref removed", oldRefs: []string{"a", "b"}, newRefs: []string{"a"}, wantChange: true},
		{name: "refs added to an empty list", oldRefs: nil, newRefs: []string{"a"}, wantChange: true},
		{name: "both empty", oldRefs: nil, newRefs: nil, wantChange: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := VersionComparisonEntity{Refs: tt.oldRefs}
			changes := old.GetChanges(VersionComparisonEntity{Refs: tt.newRefs})
			_, hasRefsChange := changes["Refs"]
			if hasRefsChange != tt.wantChange {
				t.Errorf("GetChanges() refs change reported = %v, want %v (changes: %v)", hasRefsChange, tt.wantChange, changes)
			}
		})
	}
}
