package regexpbypass

import (
	"fmt"
	rust "github.com/BurntSushi/rure-go"
	"github.com/dlclark/regexp2"
	"github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre"
	regexpb "github.com/sylvinus/regexp-bypass/regexp"
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
	name       string
	pattern    string
	text       string
	isMatch    bool
	nativeFunc func(pattern string, text string) bool
}{
	// Some of those cases are not optimized yet but could potentially be
	{"Prefix", "^xxy", strings.Repeat("x", N) + "y", false, NativeHasPrefix},
	{"Literal", "xx", "y" + strings.Repeat("x", N), true, NativeContains},
	{"LiteralN", "xxy", strings.Repeat("x", N), false, NativeContains},
	{"Suffix", "xxy$", "xxy" + strings.Repeat("x", N) + "y", true, NativeHasSuffix},
	{"SuffixN", "xxy$", "xxy" + strings.Repeat("x", N), false, NativeHasSuffix},
	{"Exact", "^xxxxy$", strings.Repeat("x", N), false, NativeEquals},
	{"Repeat", "x{2}", strings.Repeat("y", N) + "xx", true, nil},
	{"DotSuffix", "x.xy$", strings.Repeat("x", N) + "y", true, nil},
	{"DotSuffixN", "x.xy$", strings.Repeat("x", N) + "yxxy", false, nil},
	{"DotStarSuffix", ".*yxx$", strings.Repeat("x", N), false, nil},
	{"PrefixDotStarSuffix", "^xxxx.*yxx$", strings.Repeat("x", N), false, nil},
	{"PrefixDotStar", "^xxxy.*", strings.Repeat("x", N), false, nil},
	{"LateDot", "x.y", strings.Repeat("x", N) + "y", true, nil},
	{"LateDotN", "x.y", strings.Repeat("x", N), false, nil},
	{"LateClass", "[0-9a-z]", strings.Repeat("_", N) + "k", true, nil},
	{"LateFail", "a.+b.+c", strings.Repeat("a", N/10) + "cccc" + strings.Repeat("b", N/10), false, nil},
	{"LateDotPlus", "x.+y", strings.Repeat("x", N) + "\nxxy", true, nil},
	{"LateDotPlusN", "x.+y", strings.Repeat("x", N) + "\nxy", false, nil},
	{"NegativeClass", "[^b]", strings.Repeat("b", N) + "a", true, nil},
	{"NegativeClassN", "[^b]", strings.Repeat("b", N), false, nil},
	{"NegativeClassSuffixN", "[^b]$", strings.Repeat("a", N) + "b", false, nil},
	{"NegativeClass2", "[^a][^b]", strings.Repeat("b", N) + "a", true, nil},
	{"Unmatchable", "a$a$", strings.Repeat("a", N), false, nil},
	{"SimpleAltPrefix", "abc|abd", strings.Repeat("ab", N/2) + "abc", true, nil},
	{"SimpleAlt", "png|jpg", strings.Repeat("a", N) + ".png", true, nil},
	{"SimpleAltN", "png|jpg", strings.Repeat("a", N), false, nil},
	{"SimpleSuffixAlt", "(?:png|jpg)$", strings.Repeat("a", N) + ".png", true, nil},
	{"SimpleAltSuffix", "(?:png$)|(?:jpg$)", strings.Repeat("a", N) + ".png", true, nil},
	{"CharExclude", "[^a]*a", strings.Repeat("b", N) + "aba", true, nil},
	{"RouterSlow", `^(.*)$`, strings.Repeat("b", N) + "/index.htm", true, nil},
	{"RouterSlowFirstPass", `^(.*)/index\.[a-z]{3}$`, strings.Repeat("b", N) + "/index.htm", true, nil},
	{"RouterFastFirstPass", `^([^/]*)/index\.[a-z]{3}$`, strings.Repeat("b", N) + "/index.htm", true, nil},
	{"RouterFastFirstPassN", `^([^/]*)/index\.[a-z]{3}$`, strings.Repeat("b", N) + "/index", false, nil},
}

func BenchmarkRegexpBypass(b *testing.B) {

	// TODO benchmark the fuzzdata corpus

	for _, bm := range benchmarks {

		println("\npattern:", bm.pattern)

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

		// PCRE
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
