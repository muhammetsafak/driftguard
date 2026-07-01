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
$c = env('PHP_LARAVEL');
$d = array_key_exists('PHP_AKE_ENV', $_ENV);
$e = array_key_exists("PHP_AKE_SRV", $_SERVER);`)
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
		"PHP_AKE_ENV", "PHP_AKE_SRV", "PHP_ENV", "PHP_GETENV", "PHP_LARAVEL",
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

func TestDir_FlagsDynamicPHPAccess(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.php", `<?php
$k = 'DB';
$$k = $_ENV[$k];
$val = getenv($name);
$x = $_SERVER[$idx];
$plain = $$unrelated;
$real = getenv('REAL');`)

	res, err := scan.Dir(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Only the literal key is recorded as drift; nothing dynamic leaks into Keys.
	if got := res.SortedKeys(); !reflect.DeepEqual(got, []string{"REAL"}) {
		t.Errorf("SortedKeys() = %v, want [REAL]", got)
	}

	var lines []int
	for _, d := range res.SortedDynamic() {
		lines = append(lines, d.Line)
	}
	// 3: $$k with $_ENV, 4: getenv($name), 5: $_SERVER[$idx].
	// 6 (bare $$unrelated, no env token) and 7 (literal getenv) must NOT be flagged.
	if !reflect.DeepEqual(lines, []int{3, 4, 5}) {
		t.Errorf("dynamic lines = %v, want [3 4 5]", lines)
	}
}

func TestDir_FlagsDynamicAccessGoJSPython(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		content  string
		keys     []string
		dynLines []int
	}{
		{
			name: "go",
			file: "app.go",
			content: `package main
import "os"
var a = os.Getenv("GO_LIT")
var b = os.Getenv(name)
var c, _ = os.LookupEnv(key)
var d = os.Getenv(pfx + "_X")`,
			keys:     []string{"GO_LIT"},
			dynLines: []int{4, 5, 6},
		},
		{
			name: "js",
			file: "app.ts",
			content: "const a = process.env.JS_DOT;\n" +
				"const b = process.env['JS_BRACKET'];\n" +
				"const c = process.env[name];\n" +
				"const d = process.env[`${svc}_URL`];\n" +
				"const e = Deno.env.get(key);",
			keys:     []string{"JS_BRACKET", "JS_DOT"},
			dynLines: []int{3, 4, 5},
		},
		{
			name: "python",
			file: "app.py",
			content: `import os
a = os.environ['PY_LIT']
b = os.environ[name]
c = os.environ.get(key)
d = os.getenv(f"PRE_{x}")`,
			keys:     []string{"PY_LIT"},
			dynLines: []int{3, 4, 5},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tc.file, tc.content)

			res, err := scan.Dir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := res.SortedKeys(); !reflect.DeepEqual(got, tc.keys) {
				t.Errorf("keys = %v, want %v", got, tc.keys)
			}
			var lines []int
			for _, d := range res.SortedDynamic() {
				lines = append(lines, d.Line)
			}
			if !reflect.DeepEqual(lines, tc.dynLines) {
				t.Errorf("dynamic lines = %v, want %v", lines, tc.dynLines)
			}
		})
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
