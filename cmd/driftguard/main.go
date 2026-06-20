// Command driftguard keeps a project's .env.example honest: it statically finds the
// env keys the code actually reads and reports drift against the example, and in CI
// it can seed a placeholder .env so a strict loader doesn't crash on a missing file.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muhammetsafak/driftguard/internal/drift"
	"github.com/muhammetsafak/driftguard/internal/envfile"
	"github.com/muhammetsafak/driftguard/internal/scan"
)

const usage = `driftguard — keep .env.example in sync with the keys your code reads

Usage:
  driftguard check    [flags] [dir]     audit env drift; exit 1 on used-but-undeclared keys
  driftguard seed     [flags] [dir]     CI helper: write a placeholder .env when it is missing
  driftguard sanitize [flags] <files>   strip BOM / zero-width / CRLF / trailing-WS noise

Run "driftguard <command> -h" for command flags.`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:])
	case "seed":
		return runSeed(args[1:])
	case "sanitize":
		return runSanitize(args[1:])
	case "-h", "--help", "help":
		fmt.Println(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "driftguard: unknown command %q\n\n%s\n", args[0], usage)
		return 2
	}
}

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	example := fs.String("example", ".env.example", "path to the example env file")
	allowMissing := fs.Bool("allow-missing", false, "do not exit non-zero on used-but-undeclared keys")
	strictStale := fs.Bool("strict-stale", false, "also exit non-zero when the example has unused keys")
	_ = fs.Parse(args)
	dir := dirArg(fs)

	res, err := scan.Dir(dir)
	if err != nil {
		return fail("scan: %v", err)
	}
	used := res.SortedKeys()

	ef, exists, err := envfile.Parse(resolve(dir, *example))
	if err != nil {
		return fail("read %s: %v", *example, err)
	}

	out := os.Stdout
	fmt.Fprintf(out, "Scanned %s — %d env key(s) referenced in code.\n", dir, len(used))

	if !exists {
		if len(used) == 0 {
			fmt.Fprintf(out, "No %s and no env usage found — nothing to check.\n", *example)
			return 0
		}
		fmt.Fprintf(out, "\nNo %s found. Every used key is undocumented:\n", *example)
		for _, k := range used {
			loc := res.Keys[k]
			fmt.Fprintf(out, "  + %-30s %s:%d\n", k, loc.File, loc.Line)
		}
		fmt.Fprintf(out, "\nCreate %s with these keys to fix.\n", *example)
		if *allowMissing {
			return 0
		}
		return 1
	}

	missing, stale := drift.Diff(used, ef.Keys)

	if len(missing) > 0 {
		fmt.Fprintf(out, "\nMissing from %s (used in code, not documented):\n", *example)
		for _, k := range missing {
			loc := res.Keys[k]
			fmt.Fprintf(out, "  + %-30s %s:%d\n", k, loc.File, loc.Line)
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(out, "\nStale in %s (documented, never used):\n", *example)
		for _, k := range stale {
			fmt.Fprintf(out, "  - %s\n", k)
		}
	}
	if len(missing) == 0 && len(stale) == 0 {
		fmt.Fprintln(out, "\nNo drift: every used key is documented, and every documented key is used.")
		return 0
	}

	if len(missing) > 0 && !*allowMissing {
		return 1
	}
	if len(stale) > 0 && *strictStale {
		return 1
	}
	return 0
}

func runSeed(args []string) int {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	example := fs.String("example", ".env.example", "example env file to union with discovered keys")
	envPath := fs.String("env", ".env", "env file to create when missing")
	force := fs.Bool("force", false, "seed even when not running in CI")
	_ = fs.Parse(args)
	dir := dirArg(fs)

	if !*force && !inCI() {
		fmt.Fprintln(os.Stderr, "driftguard seed: not running in CI (set GITHUB_ACTIONS/CI or pass --force); doing nothing.")
		return 0
	}

	target := resolve(dir, *envPath)
	if _, err := os.Stat(target); err == nil {
		fmt.Fprintf(os.Stdout, "%s already exists; leaving it untouched.\n", *envPath)
		return 0
	} else if !os.IsNotExist(err) {
		return fail("stat %s: %v", *envPath, err)
	}

	res, err := scan.Dir(dir)
	if err != nil {
		return fail("scan: %v", err)
	}
	ef, _, err := envfile.Parse(resolve(dir, *example))
	if err != nil {
		return fail("read %s: %v", *example, err)
	}

	keys := union(res.SortedKeys(), ef.Keys)
	if len(keys) == 0 {
		fmt.Fprintln(os.Stdout, "No env keys discovered; nothing to seed.")
		return 0
	}
	if err := os.WriteFile(target, []byte(renderPlaceholder(keys)), 0o600); err != nil {
		return fail("write %s: %v", *envPath, err)
	}
	fmt.Fprintf(os.Stdout, "Wrote %s with %d placeholder key(s) so the build won't crash on a missing env file.\n", *envPath, len(keys))
	return 0
}

// runSanitize cleans one or more .env-style files of BOM / zero-width / CRLF /
// trailing-whitespace noise. By default it rewrites each dirty file in place; with
// --check it touches nothing and instead reports what WOULD change, exiting 1 if any
// file is dirty so CI can gate on it (mirroring check's "1 = drift found" convention).
func runSanitize(args []string) int {
	fs := flag.NewFlagSet("sanitize", flag.ExitOnError)
	checkOnly := fs.Bool("check", false, "report what would change without writing; exit 1 if any file is dirty")
	_ = fs.Parse(args)

	files := fs.Args()
	if len(files) == 0 {
		return fail("sanitize: no files given; usage: driftguard sanitize [--check] <files...>")
	}

	out := os.Stdout
	dirty := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return fail("read %s: %v", path, err)
		}
		cleaned, fixed := envfile.Sanitize(string(data))
		if fixed == 0 && cleaned == string(data) {
			fmt.Fprintf(out, "clean   %s\n", path)
			continue
		}
		dirty++
		if *checkOnly {
			fmt.Fprintf(out, "would clean %s (%d line(s))\n", path, fixed)
			continue
		}
		if err := os.WriteFile(path, []byte(cleaned), filePerm(path)); err != nil {
			return fail("write %s: %v", path, err)
		}
		fmt.Fprintf(out, "cleaned %s (%d line(s))\n", path, fixed)
	}

	if *checkOnly && dirty > 0 {
		fmt.Fprintf(out, "\n%d file(s) need sanitizing; run without --check to fix.\n", dirty)
		return 1
	}
	return 0
}

// filePerm returns the file's current mode so an in-place rewrite preserves it, falling
// back to a conservative 0600 if the file can't be stat-ed (it was just read, so this is
// only a belt-and-suspenders guard against a race).
func filePerm(path string) os.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().Perm()
	}
	return 0o600
}

// renderPlaceholder emits `KEY=` lines with EMPTY values: present enough to satisfy a
// strict --env-file loader, but never a fabricated secret that could be mistaken for
// a real one.
func renderPlaceholder(keys []string) string {
	var b strings.Builder
	b.WriteString("# Generated by driftguard seed — placeholder values, safe to discard.\n")
	b.WriteString("# These keys are referenced by the code; fill real values locally.\n\n")
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=\n")
	}
	return b.String()
}

func dirArg(fs *flag.FlagSet) string {
	if fs.NArg() > 0 {
		return fs.Arg(0)
	}
	return "."
}

func resolve(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func inCI() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("CI") == "true"
}

func union(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, k := range a {
		seen[k] = struct{}{}
	}
	for _, k := range b {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "driftguard: "+format+"\n", args...)
	return 2
}
