package bridge

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// foldName maps a file name to the key the deny list is matched against.
//
// The deny list must see a name the way the filesystem does. macOS APFS
// (case-insensitive, the default) treats names as equal after Unicode
// normalisation and full case folding, so for example "ſecret.txt" opens
// "secret.txt" and "id_rſa" opens "id_rsa", while strings.ToLower leaves
// "ſ" alone. foldName therefore:
//
//  1. drops combining marks (U+0300-U+036F and other Mn), so a decomposed
//     "e" + U+0301 compares like "e"; deny patterns are ASCII, so this only
//     ever makes matching stricter, never looser;
//  2. applies full case folding for the multi-rune expansions APFS uses
//     (ß -> ss, ligatures ﬀ ﬁ ﬂ ﬃ ﬄ ﬅ ﬆ), then
//  3. simple case folding to the smallest member of each rune's fold orbit
//     and lower case (K Kelvin -> k, ſ long s -> s, etc.).
//
// The expansion table was confirmed by scanning every code point from
// U+0080 to U+2FFFF against ASCII names on APFS (see TestFoldNameCoversAPFSAliases).
func foldName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if exp, ok := fullFold[r]; ok {
			b.WriteString(exp)
			continue
		}
		if r < utf8.RuneSelf {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(simpleFold(r))
	}
	return b.String()
}

// simpleFold returns the lower-cased smallest rune in r's case-fold orbit.
func simpleFold(r rune) rune {
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < m {
			m = f
		}
	}
	return unicode.ToLower(m)
}

// fullFold: Unicode CaseFolding.txt status F entries whose folds are ASCII,
// which are the ones that can produce a denied ASCII name.
var fullFold = map[rune]string{
	'\u00DF': "ss",  // ß
	'\u1E9E': "ss",  // ẞ
	'\uFB00': "ff",  // ﬀ
	'\uFB01': "fi",  // ﬁ
	'\uFB02': "fl",  // ﬂ
	'\uFB03': "ffi", // ﬃ
	'\uFB04': "ffl", // ﬄ
	'\uFB05': "st",  // ﬅ
	'\uFB06': "st",  // ﬆ
	'\u0130': "i",   // İ (folds to i + combining dot; the mark is dropped)
}
