// Package envfile parses .env-style files into the set of keys they declare and
// sanitizes their content. It intentionally ignores values for the drift check —
// only the declared key set matters for "is every used key documented?".
package envfile

import (
	"bufio"
	"os"
	"strings"
)

// File is the parsed contents of a .env-style file.
type File struct {
	// Keys are the declared keys, in first-seen file order.
	Keys   []string
	Values map[string]string
	set    map[string]struct{}
}

// Has reports whether key is declared in the file.
func (f *File) Has(key string) bool {
	_, ok := f.set[key]
	return ok
}

// Parse reads a .env-style file. A missing file is not an error: it returns an
// empty File and exists=false so callers can treat "no .env.example yet" as a
// distinct, reportable state rather than a failure.
func Parse(path string) (file *File, exists bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newFile(), false, nil
		}
		return nil, false, err
	}
	f := newFile()
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		if key == "" {
			continue
		}
		val := strings.TrimSpace(trimmed[eq+1:])
		if _, ok := f.set[key]; !ok {
			f.Keys = append(f.Keys, key)
		}
		f.set[key] = struct{}{}
		f.Values[key] = val
	}
	return f, true, sc.Err()
}

func newFile() *File {
	return &File{Values: map[string]string{}, set: map[string]struct{}{}}
}

// invisibleRunes are codepoints that carry no visible meaning in a .env file but
// routinely sneak in via copy-paste from chat apps, docs, and rich-text editors, then
// silently corrupt a key or value so a strict loader either rejects the file or reads
// a subtly wrong name. We strip these outright wherever they appear on a line:
//
//	U+FEFF  ZERO WIDTH NO-BREAK SPACE / BOM (also handled as a leading BOM below)
//	U+200B  ZERO WIDTH SPACE
//	U+200C  ZERO WIDTH NON-JOINER
//	U+200D  ZERO WIDTH JOINER
//	U+2060  WORD JOINER
//
// NBSP (U+00A0) is deliberately NOT stripped: unlike the zero-widths it is visible as a
// space, a user may have typed it intentionally inside a value, and silently deleting it
// would change content the author can see. We leave NBSP untouched on purpose.
// Written as \u escapes on purpose: literal invisible bytes in source are unreviewable
// and a raw U+FEFF is rejected by the Go tokenizer as an "illegal byte order mark".
var invisibleRunes = map[rune]struct{}{
	'\uFEFF': {}, // ZERO WIDTH NO-BREAK SPACE / BOM
	'\u200B': {}, // ZERO WIDTH SPACE
	'\u200C': {}, // ZERO WIDTH NON-JOINER
	'\u200D': {}, // ZERO WIDTH JOINER
	'\u2060': {}, // WORD JOINER
}

// bom is the UTF-8 byte-order mark. A leading BOM is invisible but makes the first key
// parse as "<BOM>KEY", so a strict loader never sees the variable the author intended.
const bom = "\uFEFF"

// Sanitize normalizes a .env file's bytes so a strict loader will accept it without
// changing any visible content. It reports how many lines it altered. Three classes of
// damage are cleaned, each one a real-world copy-paste/hosting hazard:
//
//   - A leading UTF-8 BOM, stripped once from the very start of the file.
//   - Zero-width / invisible codepoints anywhere on a line (see invisibleRunes), which
//     corrupt keys and values without being visible to the person who pasted them.
//   - Stray carriage returns and trailing space/tab, the classic CRLF / hand-edit noise.
//
// It is intentionally conservative: it only removes a fixed allow-list of known-invisible
// codepoints and trailing whitespace, never touching legitimate visible content (NBSP
// included — see invisibleRunes).
func Sanitize(content string) (cleaned string, fixed int) {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		clean := l
		if i == 0 {
			// A leading BOM lives at the very start of the file, i.e. the head of the
			// first line; strip it here so a BOM-only change still counts as one fixed
			// line rather than slipping past the per-line comparison below.
			clean = strings.TrimPrefix(clean, bom)
		}
		clean = stripInvisible(clean)
		clean = strings.TrimRight(clean, " \t\r")
		if clean != l {
			fixed++
		}
		lines[i] = clean
	}
	return strings.Join(lines, "\n"), fixed
}

// stripInvisible removes every zero-width / invisible rune from a single line, leaving
// all other characters (including NBSP and ordinary whitespace) exactly as they were.
func stripInvisible(line string) string {
	if !strings.ContainsFunc(line, isInvisible) {
		return line
	}
	return strings.Map(func(r rune) rune {
		if isInvisible(r) {
			return -1
		}
		return r
	}, line)
}

func isInvisible(r rune) bool {
	_, ok := invisibleRunes[r]
	return ok
}
