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

func TestSanitize_StripsLeadingBOM(t *testing.T) {
	// A leading UTF-8 BOM makes the first key parse as "\uFEFF FOO"; strip it.
	in := "\uFEFFFOO=1\nBAR=2\n"
	out, fixed := envfile.Sanitize(in)
	if out != "FOO=1\nBAR=2\n" {
		t.Errorf("BOM not stripped: out = %q", out)
	}
	if fixed != 1 {
		t.Errorf("fixed = %d, want 1 (the BOM-bearing first line)", fixed)
	}
}

func TestSanitize_StripsZeroWidth(t *testing.T) {
	// Zero-width space (U+200B), ZWNJ (U+200C), ZWJ (U+200D), word joiner (U+2060),
	// and a mid-line U+FEFF all vanish without disturbing visible characters.
	in := "FO\u200BO=ba\u200Cr\nBAZ=\u2060q\uFEFFux\u200D\n"
	out, fixed := envfile.Sanitize(in)
	if out != "FOO=bar\nBAZ=qux\n" {
		t.Errorf("zero-width not stripped: out = %q", out)
	}
	if fixed != 2 {
		t.Errorf("fixed = %d, want 2", fixed)
	}
}

func TestSanitize_KeepsNBSP(t *testing.T) {
	// NBSP (U+00A0) is visible as a space and may be intentional inside a value, so we
	// preserve it rather than silently altering content the author can see (the value here holds a literal U+00A0).
	in := "GREETING=hello\u00A0world\n"
	out, fixed := envfile.Sanitize(in)
	if out != in {
		t.Errorf("NBSP should be preserved: out = %q", out)
	}
	if fixed != 0 {
		t.Errorf("fixed = %d, want 0 (NBSP is left untouched)", fixed)
	}
}

func TestSanitize_NoOpOnCleanFile(t *testing.T) {
	in := "FOO=1\nBAR=two\n\n# comment\n"
	out, fixed := envfile.Sanitize(in)
	if out != in {
		t.Errorf("clean file should be unchanged: out = %q", out)
	}
	if fixed != 0 {
		t.Errorf("fixed = %d, want 0 for an already-clean file", fixed)
	}
}
