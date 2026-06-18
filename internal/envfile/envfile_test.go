package envfile_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/muhammetsafak/driftguard/internal/envfile"
)

func TestParse(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env.example")
	body := "# a comment\nFOO=1\nexport BAR=two\n\nBAZ=\nFOO=dup\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	f, exists, err := envfile.Parse(p)
	if err != nil || !exists {
		t.Fatalf("Parse: exists=%v err=%v", exists, err)
	}
	if want := []string{"FOO", "BAR", "BAZ"}; !reflect.DeepEqual(f.Keys, want) {
		t.Errorf("Keys = %v, want %v", f.Keys, want)
	}
	if !f.Has("BAR") || f.Has("NOPE") {
		t.Error("Has returned wrong membership")
	}
	if f.Values["BAR"] != "two" || f.Values["BAZ"] != "" {
		t.Errorf("values = %v", f.Values)
	}
}

func TestParse_MissingIsNotAnError(t *testing.T) {
	f, exists, err := envfile.Parse(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if exists {
		t.Error("exists should be false for a missing file")
	}
	if len(f.Keys) != 0 {
		t.Errorf("missing file should yield no keys, got %v", f.Keys)
	}
}

func TestSanitize(t *testing.T) {
	in := "A=1 \r\nB=2\t\nC=3\n"
	out, fixed := envfile.Sanitize(in)
	if out != "A=1\nB=2\nC=3\n" {
		t.Errorf("Sanitize output = %q", out)
	}
	if fixed != 2 {
		t.Errorf("fixed = %d, want 2 (A and B lines)", fixed)
	}
}
