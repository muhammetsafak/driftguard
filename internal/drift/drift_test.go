package drift_test

import (
	"reflect"
	"testing"

	"github.com/muhammetsafak/driftguard/internal/drift"
)

func TestDiff(t *testing.T) {
	used := []string{"DB_URL", "API_KEY", "NEW_KEY"}
	declared := []string{"DB_URL", "API_KEY", "OLD_KEY"}

	missing, stale := drift.Diff(used, declared)

	if want := []string{"NEW_KEY"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
	if want := []string{"OLD_KEY"}; !reflect.DeepEqual(stale, want) {
		t.Errorf("stale = %v, want %v", stale, want)
	}
}

func TestDiff_NoDrift(t *testing.T) {
	missing, stale := drift.Diff([]string{"A", "B"}, []string{"B", "A"})
	if len(missing) != 0 || len(stale) != 0 {
		t.Errorf("expected no drift; missing=%v stale=%v", missing, stale)
	}
}

func TestDiff_EmptyDeclared(t *testing.T) {
	missing, stale := drift.Diff([]string{"A", "B"}, nil)
	if want := []string{"A", "B"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
	if len(stale) != 0 {
		t.Errorf("stale = %v, want empty", stale)
	}
}
