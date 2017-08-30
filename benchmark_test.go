package regexpbypass

import (
	"fmt"
	rust "github.com/BurntSushi/rure-go"
	"github.com/dlclark/regexp2"
	"github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre"
	"github.com/gobwas/glob"
	regexpb "github.com/sylvinus/regexp-bypass/regexp"
	regexpdfa "matloob.io/regexp"
	"regexp"
	"strings"
	"testing"
)

func NativeHasPrefix(pattern string, text string) bool {
	return strings.HasPrefix(text, pattern[1:])
}

func NativeContains(pattern string, text string) bool {
	return strings.Contains(text, pattern)
}

func NativeHasSuffix(pattern string, text string) bool {
	return strings.HasSuffix(text, pattern[:len(pattern)-1])
}

func NativeEquals(pattern string, text string) bool {
	return pattern == text
}

var N = 1000

var benchmarks = []struct {
	name        string
	pattern     string
	text        string
	isMatch     bool
	nativeFunc  func(pattern string, text string) bool
	globPattern string
}{
	// Some of those cases are not optimized yet but could potentially be
	{"Prefix", "^xxy", strings.Repeat("x", N) + "y", false, NativeHasPrefix, "xxy*"},
	{"Literal", "xx", "y" + strings.Repeat("x", N), true, NativeContains, "*xx*"},
	{"LiteralN", "xxy", strings.Repeat("x", N), false, NativeContains, "*xxy*"},
	{"Suffix", "xxy$", "xxy" + strings.Repeat("x", N) + "y", true, NativeHasSuffix, "*xxy"},
	{"SuffixN", "xxy$", "xxy" + strings.Repeat("x", N), false, NativeHasSuffix, "*xxy"},
	{"Exact", "^xxxxy$", strings.Repeat("x", N), false, NativeEquals, "xxxxy"},
	{"Repeat", "x{2}", strings.Repeat("y", N) + "xx", true, nil, "*xx*"},
	{"DotSuffix", "x.xy$", strings.Repeat("x", N) + "y", true, nil, "*x?xy"},
	{"DotSuffixN", "x.xy$", strings.Repeat("x", N) + "yxxy", false, nil, "*x?xy"},
	{"DotStarSuffix", ".*yxx$", strings.Repeat("x", N), false, nil, "\n*yxx"},
	{"PrefixDotStarSuffix", "^xxxx.*yxx$", strings.Repeat("x", N), false, nil, "\nxxxx*yxx"},
	{"PrefixDotStar", "^xxxy.*", strings.Repeat("x", N), false, nil, "\nxxxy*"},
	{"LateDot", "x.y", strings.Repeat("x", N) + "y", true, nil, "*x?y*"},
	{"LateDotN", "x.y", strings.Repeat("x", N), false, nil, "*x?y*"},
	{"LateDotHard", "x.y", strings.Repeat("xy", N/2) + "y", true, nil, "*x?y*"},
	{"LateDotHardN", "x.y", strings.Repeat("xy", N/2), false, nil, "*x?y*"},
	{"LateDotHarder", "x....y", strings.Repeat("xxxxy", N/5) + "y", true, nil, "*x????y*"},
	{"LateDotUnicode", "☺....y", strings.Repeat("☺☺☺☺y", N/5) + "y", true, nil, ""},
	{"LateFail", "a.+b.+c", strings.Repeat("a", N/10) + "cccc" + strings.Repeat("b", N/10), false, nil, "\n*a?*b?*c*"},
	{"LateDotPlus", "x.+y", strings.Repeat("x", N) + "\nxxy", true, nil, "\n**x?*y"},
	{"LateDotPlusN", "x.+y", strings.Repeat("x", N) + "\nxy", false, nil, "\n**x?*y"},
	{"LateClass", "[0-9a-z]", strings.Repeat("_", N) + "k", true, nil, ""},
	{"LateClassHard", "xxxx[0-9a-z]", strings.Repeat("xxxx_", N/5) + "xxxx0", true, nil, ""},
	{"LateClassHarder", "[0-9a-z]{5}", strings.Repeat("xxxx_", N/5) + "xxxx0", true, nil, ""},
	{"NegativeClass", "[^b]", strings.Repeat("b", N) + "a", true, nil, "*[!b]*"},
	{"NegativeClassN", "[^b]", strings.Repeat("b", N), false, nil, "*[!b]*"},
	{"NegativeClassSuffixN", "[^b]$", strings.Repeat("a", N) + "b", false, nil, "*[!b]"},
	{"NegativeClass2", "[^a][^b]", strings.Repeat("b", N) + "a", true, nil, "*[!a][!b]*"},
	{"Unmatchable", "a$a$", strings.Repeat("a", N), false, nil, ""},
	{"SimpleAltPrefix", "abc|abd", strings.Repeat("ab", N/2) + "abc", true, nil, "{*abc*,*abd*}"},
	{"SimpleAlt", "png|jpg", strings.Repeat("a", N) + ".png", true, nil, "{*png*,*jpg*}"},
	{"SimpleAltN", "png|jpg", strings.Repeat("a", N), false, nil, "{*png*, *jpg*}"},
	{"SimpleSuffixAlt", "(?:png|jpg)$", strings.Repeat("a", N) + ".png", true, nil, "{*png,*jpg}"},
	{"SimpleAltSuffix", "(?:png$)|(?:jpg$)", strings.Repeat("a", N) + ".png", true, nil, "{*png,*jpg}"},
	{"CharExclude", "[^a]*a", strings.Repeat("b", N) + "aba", true, nil, ""},
	{"RouterSlow", `^(.*)$`, strings.Repeat("b", N) + "/index.htm", true, nil, ""},
	{"RouterSlowFirstPass", `^(.*)/index\.[a-z]{3}$`, strings.Repeat("b", N) + "/index.htm", true, nil, "\n*/index.[a-z][a-z][a-z]"},
	{"RouterFastFirstPass", `^([^/]*)/index\.[a-z]{3}$`, strings.Repeat("b", N) + "/index.htm", true, nil, ""},
	{"RouterFastFirstPassN", `^([^/]*)/index\.[a-z]{3}$`, strings.Repeat("b", N) + "/index", false, nil, ""},
}

func BenchmarkRegexpBypass(b *testing.B) {

	b.ReportAllocs()

	// TODO benchmark the fuzzdata corpus

	for _, bm := range benchmarks {

		fmt.Println("\nPattern:", bm.pattern)

		// bypass
		b.Run(bm.name+"/bypass", func(b *testing.B) {
			re := regexpb.MustCompile(bm.pattern)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if re.MatchString(bm.text) != bm.isMatch {
					b.Fatal("")
				}
			}
		})

		// Native if it exists
		if bm.nativeFunc != nil {
			b.Run(bm.name+"/native", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					if bm.nativeFunc(bm.pattern, bm.text) != bm.isMatch {
						b.Fatal("")
					}
				}
			})
		}

		// Standard library, no bypass
		b.Run(bm.name+"/std", func(b *testing.B) {
			re := regexp.MustCompile(bm.pattern)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if re.MatchString(bm.text) != bm.isMatch {
					b.Fatal("")
				}
			}
		})

		// DFA branch from https://github.com/matloob/regexp
		b.Run(bm.name+"/stddfa", func(b *testing.B) {
			re := regexpdfa.MustCompile(bm.pattern)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if re.MatchString(bm.text) != bm.isMatch {
					b.Fatal("")
				}
			}
		})

		// Glob if it exists
		if bm.globPattern != "" {
			b.Run(bm.name+"/glob", func(b *testing.B) {

				g := glob.MustCompile(bm.globPattern)
				if bm.globPattern[0:1] == "\n" {
					// glob * is not the same as regexp .* if we don't have \n as a separator
					g = glob.MustCompile(bm.globPattern[1:], '\n')
				}

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if g.Match(bm.text) != bm.isMatch {
						b.Fatal("")
					}
				}
			})
		}

		// PCRE
		if !strings.Contains(bm.pattern, "☺") {
			b.Run(bm.name+"/pcre", func(b *testing.B) {
				re := pcre.MustCompile(bm.pattern, 0)
				matcher := re.MatcherString("", 0)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					matcher.ResetString(re, bm.text, 0)
					if matcher.Matches() != bm.isMatch {
						b.Fatal("")
					}
				}
			})
		}

		// regexp2
		b.Run(bm.name+"/regexp2", func(b *testing.B) {
			re := regexp2.MustCompile(bm.pattern, 0)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				isMatch, _ := re.MatchString(bm.text)
				if isMatch != bm.isMatch {
					b.Fatal("")
				}
			}
		})

		// Rust
		b.Run(bm.name+"/rust", func(b *testing.B) {
			re := rust.MustCompile(bm.pattern)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				isMatch := re.IsMatch(bm.text)
				if isMatch != bm.isMatch {
					b.Fatal("")
				}
			}
		})

		// SRE2 was tested but performed much slower than all the implementations, and some patterns
		// failed to compile at all.
		// https://github.com/samthor/sre2

		// TODO: test https://github.com/moovweb/rubex (failing to compile)
		// TODO: test https://github.com/opennota/re2dfa

	}
}

func BenchmarkStringsSuffix(b *testing.B) {

	println("")

	lengths := []int{10, 100, 1000, 1000000, 10000000}

	for _, length := range lengths {

		text := strings.Repeat("a", length)

		b.Run(fmt.Sprintf("%d", length), func(b *testing.B) {

			for i := 0; i < b.N; i++ {
				if !strings.HasSuffix(text, "a") {
					panic("")
				}
			}
		})
	}
}
