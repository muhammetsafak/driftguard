package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// project builds a temp dir with a Go source that reads usedKeys via os.Getenv, plus an
// optional .env.example listing exampleKeys (nil = no example file).
func project(t *testing.T, exampleKeys, usedKeys []string) string {
	t.Helper()
	dir := t.TempDir()

	var src strings.Builder
	src.WriteString("package main\n\nimport \"os\"\n\nfunc main() {\n")
	for _, k := range usedKeys {
		src.WriteString("\t_ = os.Getenv(\"" + k + "\")\n")
	}
	src.WriteString("}\n")
	write(t, filepath.Join(dir, "app.go"), src.String())

	if exampleKeys != nil {
		var ex strings.Builder
		for _, k := range exampleKeys {
			ex.WriteString(k + "=\n")
		}
		write(t, filepath.Join(dir, ".env.example"), ex.String())
	}
	return dir
}

func TestRun_Dispatch(t *testing.T) {
	if run(nil) != 2 {
		t.Error("no command → exit 2")
	}
	if run([]string{"help"}) != 0 {
		t.Error("help → exit 0")
	}
	if run([]string{"bogus"}) != 2 {
		t.Error("unknown command → exit 2")
	}
}

func TestCheck_NoDrift(t *testing.T) {
	dir := project(t, []string{"FOO"}, []string{"FOO"})
	if c := run([]string{"check", dir}); c != 0 {
		t.Fatalf("no drift → 0, got %d", c)
	}
}

func TestCheck_MissingKey(t *testing.T) {
	dir := project(t, []string{"FOO"}, []string{"FOO", "BAR"})
	if c := runCheck([]string{dir}); c != 1 {
		t.Fatalf("used-but-undeclared → 1, got %d", c)
	}
	if c := runCheck([]string{"--allow-missing", dir}); c != 0 {
		t.Fatalf("--allow-missing → 0, got %d", c)
	}
}

func TestCheck_NoExample(t *testing.T) {
	dir := project(t, nil, []string{"FOO"})
	if c := runCheck([]string{dir}); c != 1 {
		t.Fatalf("no example + usage → 1, got %d", c)
	}
	if c := runCheck([]string{"--allow-missing", dir}); c != 0 {
		t.Fatalf("no example + --allow-missing → 0, got %d", c)
	}
	empty := project(t, nil, nil)
	if c := runCheck([]string{empty}); c != 0 {
		t.Fatalf("no example + no usage → 0, got %d", c)
	}
}

func TestCheck_Stale(t *testing.T) {
	dir := project(t, []string{"FOO", "UNUSED"}, []string{"FOO"})
	if c := runCheck([]string{dir}); c != 0 {
		t.Fatalf("stale alone → 0, got %d", c)
	}
	if c := runCheck([]string{"--strict-stale", dir}); c != 1 {
		t.Fatalf("--strict-stale → 1, got %d", c)
	}
}

func TestCheck_ReadError(t *testing.T) {
	dir := project(t, nil, []string{"FOO"})
	// point --example at the directory itself → reading it as a file errors → exit 2
	if c := runCheck([]string{"--example", ".", dir}); c != 2 {
		t.Fatalf("unreadable example → 2, got %d", c)
	}
}

func TestSeed_NotInCINoForce(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("CI", "")
	dir := project(t, []string{"FOO"}, []string{"FOO"})
	if c := runSeed([]string{dir}); c != 0 {
		t.Fatalf("not CI, no --force → 0 (noop), got %d", c)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatal("should not have written .env outside CI")
	}
}

func TestSeed_ForceWrites(t *testing.T) {
	dir := project(t, []string{"FOO"}, []string{"FOO", "BAR"})
	if c := runSeed([]string{"--force", dir}); c != 0 {
		t.Fatalf("--force seed → 0, got %d", c)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "FOO=") || !strings.Contains(string(body), "BAR=") {
		t.Fatalf("placeholder missing keys:\n%s", body)
	}
}

func TestSeed_ExistingEnvUntouched(t *testing.T) {
	dir := project(t, []string{"FOO"}, []string{"FOO"})
	write(t, filepath.Join(dir, ".env"), "FOO=real\n")
	if c := runSeed([]string{"--force", dir}); c != 0 {
		t.Fatalf("existing .env → 0, got %d", c)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if string(body) != "FOO=real\n" {
		t.Fatalf("existing .env was modified: %s", body)
	}
}

func TestSeed_CIDetectedWrites(t *testing.T) {
	t.Setenv("CI", "true")
	dir := project(t, nil, []string{"FOO"})
	if c := runSeed([]string{dir}); c != 0 {
		t.Fatalf("CI seed → 0, got %d", c)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatal(".env should be written under CI")
	}
}

func TestSeed_NoKeys(t *testing.T) {
	dir := project(t, nil, nil)
	if c := runSeed([]string{"--force", dir}); c != 0 {
		t.Fatalf("no keys → 0, got %d", c)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatal("no keys → no .env written")
	}
}

func TestSanitize_NoFiles(t *testing.T) {
	if c := runSanitize(nil); c != 2 {
		t.Fatalf("no files → 2 (usage error), got %d", c)
	}
}

func TestSanitize_ReadError(t *testing.T) {
	// point at a path that does not exist → read error → exit 2
	if c := runSanitize([]string{filepath.Join(t.TempDir(), "nope.env")}); c != 2 {
		t.Fatalf("unreadable file → 2, got %d", c)
	}
}

func TestSanitize_WritesInPlace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	write(t, p, "\uFEFFFOO=1 \r\nBAR=2\t\n")
	if c := runSanitize([]string{p}); c != 0 {
		t.Fatalf("sanitize dirty file → 0, got %d", c)
	}
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "FOO=1\nBAR=2\n" {
		t.Fatalf("file not cleaned in place: %q", body)
	}
}

func TestSanitize_CheckDirtyIsNonZeroAndDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	original := "FOO=1 \r\n"
	write(t, p, original)
	if c := runSanitize([]string{"--check", p}); c != 1 {
		t.Fatalf("--check on dirty file → 1, got %d", c)
	}
	body, _ := os.ReadFile(p)
	if string(body) != original {
		t.Fatalf("--check must not modify the file, got %q", body)
	}
}

func TestSanitize_CleanFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	clean := "FOO=1\nBAR=2\n"
	write(t, p, clean)
	if c := run([]string{"sanitize", p}); c != 0 {
		t.Fatalf("clean file → 0, got %d", c)
	}
	if c := runSanitize([]string{"--check", p}); c != 0 {
		t.Fatalf("--check on clean file → 0, got %d", c)
	}
	body, _ := os.ReadFile(p)
	if string(body) != clean {
		t.Fatalf("clean file must be untouched, got %q", body)
	}
}

func TestSanitize_PreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("FOO=1 \n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if c := runSanitize([]string{p}); c != 0 {
		t.Fatalf("sanitize → 0, got %d", c)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o640 {
		t.Fatalf("file mode not preserved: got %o, want 0640", got)
	}
}

func TestHelpers(t *testing.T) {
	if got := union([]string{"a", "b"}, []string{"b", "c"}); len(got) != 3 {
		t.Fatalf("union dedupe: %v", got)
	}
	if !strings.Contains(renderPlaceholder([]string{"K"}), "K=") {
		t.Fatal("renderPlaceholder should emit KEY=")
	}
	if resolve("/abs", "x") != filepath.Join("/abs", "x") {
		t.Fatal("resolve relative")
	}
	if resolve("/abs", "/other") != "/other" {
		t.Fatal("resolve absolute passes through")
	}
}
