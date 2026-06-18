package scan_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/muhammetsafak/driftguard/internal/scan"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDir_ExtractsKeysPerLanguage(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.go", `package main
import "os"
func main() { _ = os.Getenv("GO_KEY"); _, _ = os.LookupEnv("GO_LOOKUP") }`)
	write(t, dir, "web/app.ts", `const a = process.env.TS_DOT;
const b = process.env['TS_BRACKET'];
const c = Deno.env.get("DENO_KEY");`)
	write(t, dir, "api/app.php", `<?php
$a = getenv('PHP_GETENV');
$b = $_ENV['PHP_ENV'];
$c = env('PHP_LARAVEL');`)
	write(t, dir, "svc/app.py", `import os
a = os.environ['PY_BRACKET']
b = os.environ.get('PY_GET')
c = os.getenv("PY_GETENV")`)
	// Must be ignored: vendored dir + a non-source extension.
	write(t, dir, "node_modules/dep.js", `process.env.SHOULD_BE_IGNORED`)
	write(t, dir, "README.md", `process.env.NOT_SCANNED`)

	res, err := scan.Dir(dir)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}

	want := []string{
		"DENO_KEY", "GO_KEY", "GO_LOOKUP",
		"PHP_ENV", "PHP_GETENV", "PHP_LARAVEL",
		"PY_BRACKET", "PY_GET", "PY_GETENV",
		"TS_BRACKET", "TS_DOT",
	}
	if got := res.SortedKeys(); !reflect.DeepEqual(got, want) {
		t.Errorf("SortedKeys()\n got = %v\nwant = %v", got, want)
	}
	if _, ok := res.Keys["SHOULD_BE_IGNORED"]; ok {
		t.Error("keys under node_modules/ must be ignored")
	}
	if _, ok := res.Keys["NOT_SCANNED"]; ok {
		t.Error("non-source extensions must be skipped")
	}
}

func TestDir_RecordsFirstLocation(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.go", "package main\nimport \"os\"\nvar x = os.Getenv(\"DB_URL\")\nvar y = os.Getenv(\"DB_URL\")\n")

	res, err := scan.Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	loc, ok := res.Keys["DB_URL"]
	if !ok {
		t.Fatal("DB_URL not found")
	}
	if loc.Line != 3 {
		t.Errorf("first location line = %d, want 3", loc.Line)
	}
}
