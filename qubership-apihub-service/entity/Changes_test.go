package entity

import "testing"

func TestVersionComparisonEntityGetChangesRefs(t *testing.T) {
	tests := []struct {
		name       string
		oldRefs    []string
		newRefs    []string
		wantChange bool
	}{
		{name: "same refs in the same order", oldRefs: []string{"a", "b"}, newRefs: []string{"a", "b"}, wantChange: false},
		{name: "same refs in another order", oldRefs: []string{"a", "b", "c"}, newRefs: []string{"c", "a", "b"}, wantChange: false},
		{name: "different refs", oldRefs: []string{"a", "b"}, newRefs: []string{"a", "c"}, wantChange: true},
		{name: "refs removed", oldRefs: []string{"a", "b"}, newRefs: nil, wantChange: true},
		{name: "refs added", oldRefs: nil, newRefs: []string{"a"}, wantChange: true},
		{name: "nil and empty are equal", oldRefs: nil, newRefs: []string{}, wantChange: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldComparison := VersionComparisonEntity{Refs: tt.oldRefs}
			newComparison := VersionComparisonEntity{Refs: tt.newRefs}
			changes := oldComparison.GetChanges(newComparison)
			if _, changed := changes["Refs"]; changed != tt.wantChange {
				t.Errorf("GetChanges() refs change = %v, want %v (changes: %v)", changed, tt.wantChange, changes)
			}
		})
	}
}
