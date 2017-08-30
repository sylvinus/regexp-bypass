## Proposal: regexp: Optimize fixed-length patterns

This is a proof-of-concept repository for a [proposed improvement](https://github.com/golang/go/issues/21463) to the Go `regexp` package.

[View the current patch](https://github.com/sylvinus/regexp-bypass/compare/aec5cc64208771a29c61fa76e80c0dc264c4220f...master)

The `regexp` package has 3 different matchers (NFA, onepass, backtrack). One of them is selected depending on the pattern to be matched.

This proposal adds a [4th matcher](https://github.com/sylvinus/regexp-bypass/blob/master/regexp/bypass.go) named `bypass`. It is optimized for fixed-length patterns like `a.ab$` on strings. It provides near constant-time matching whereas the current matchers from `regexp` perform linearly with the size of the input string. Performance becomes close to what can be achieved with methods like `strings.Index`.

Here is a sample benchmark result of `regexp.MatchString("a.ab$", strings.Repeat("a", 1000) + "b")`. Full benchmark results are attached at the end of this README.

```
pattern: a.ab$
BenchmarkRegexpBypass/DotSuffix/bypass-8      	30000000	       109 ns/op  # New matcher implementation
BenchmarkRegexpBypass/DotSuffix/std-8         	   50000	     69882 ns/op  # Go's current standard library
BenchmarkRegexpBypass/DotSuffix/pcre-8        	  100000	     41359 ns/op  # PCRE
BenchmarkRegexpBypass/DotSuffix/rust-8        	20000000	       157 ns/op  # Rust regexp engine
```

## Supported patterns

"Fixed-length" patterns don't contain any of `*`, `+`, `?`: the length of the string they match can be computed at compile-time.

Pattern type | Examples | Supported? | Comment
--- | --- | --- | ---
Anchored literals | `^ab`, `ab$` | Yes, with `byPassProgAnchored` | Effectively translated to `strings.HasPrefix` and `strings.HasSuffix`
Anchored fixed-length | `^a[^b][0-9]\w`, `a.ab$` | Yes, with `byPassProgAnchored` | Because of the anchors we can scan the minimum number of bytes in the string
Unanchored fixed-length single-step | `a`, `[^b]`, `.` | Yes, with `byPassProgUnanchored` | String is scanned until a match is found, possibly with `strings.Index`
Unanchored fixed-length multi-step | `a.b`, `[^a][^b]` | Yes, with `byPassProgUnanchored` | Implemented with simple backtracking
Top-level alternations of the above | `jpg\|png`, `(?:[a-z]{3}$)\|(?:[0-9]$)` | Yes, with `byPassProgAlternate` | Each part is run with `byPassProgLinear` until one matches
Fixed-length prefixes or suffixes in any pattern | `(a*)bb$`, `^[0-9](\w?)` | Yes, with `byPassProgFirstPass` | The prefix or suffix is first run through `byPassProgLinear`. If it matches, the rest of the pattern (`(a*)$` & `^(\w?)` in the examples) is then executed by the regular matchers on the rest of the string.
Capturing groups | `^(ab)` | Not yet |
Variable-length patterns | `.*`, `.+`, `.?` | No |
Nested alternations | `a(b.\|c.)` | No | Some might be transformed to fixed-length top-level alternations
Word boundaries | `\b[a-z]\b` | No |

Streaming input with `inputReader` is not supported. `[]byte` input with `inputBytes` is not yet supported but could be added.

## Stats from GitHub

[Steren Giannini](https://github.com/steren) made a [BigQuery](https://gist.github.com/steren/4e8784ba782c624be48f97a4ea808f28) to extract [all regexps](https://gist.github.com/steren/4e8784ba782c624be48f97a4ea808f28) used in Go files on GitHub.

Here are the stats on their support with the current implementation:

```
Total occurences                                76341
Invalid                                         2545 (3.33%)
Unsupported                                     45922 (60.15%)
Supported total                                 27874 (36.51%)
 Supported with byPassProgAnchored              6833 (8.95%)
 Supported with byPassProgUnanchored            9876 (12.94%)
 Supported with byPassProgAlternate             490 (0.64%)
 Supported with byPassProgFirstPass             10626 (13.92%)
 Supported with byPassProgUnmatchable           49 (0.06%)
```

Note that the supported total will grow when we add support for capturing groups and multi-step unanchored patterns.

## What is missing currently but could be done in the scope of this proposal

 - Add support for `Find`, `Split`, ...
 - Add support for `[]byte` as input (`re.Match()`)
 - Add support for capturing groups
 - Avoid factoring patterns like `aa|ab` so that we can use `byPassProgAlternate`
 - Fine-tune types & fix struct alignment
 - Narrow down the exact list of flags that are supported
 - Add benchmarks with re2
 - Compile (?:png|jpg) as a single "multi-literal"
 - Some profiling for micro-optimizations
 - Understand why `^(.*)$` is very slow with the standard library (see `Router` benchmarks below) and making our `firstpass` optimization actually slower for now
 - Optimize class matching

## Pros & cons

The obvious cons of this proposal are:

 - Adding more code to the standard library
 - Adding more work at compilation time.

The pros are:

 - Constant-time matching for many simple regexps (relative to the size of the input string)
 - No overhead for regexps that are not supported
 - Go's `regexp` package is usually considered immature performance-wise. This proposal plays a small role in fixing that by adding optimizations that can reasonably be expected from the end-user.
 - This matcher keeps very little state and bypasses the mutex from `regexp.go`
 - There are already 3 different matchers in the standard library (4 with the upcoming DFA), so adding a new one for a specific kind of patterns is not surprising.
 - `regexp.MatchString("(?:png|jpg)$")` could obviously be rewritten as `strings.HasSuffix("png") or strings.HasSuffix("jpg")` but sometimes it is not practical because the pattern to be matched is user-supplied or part of a long list of patterns. Examples include interactive log search or lists of paths in HTTP routers.
 - Limited risk due to exhaustive tests in the standard library and additional fuzzing

Feedback would be highly appreciated!

Credits go to Russ Cox for his many implementations and [articles](https://swtch.com/~rsc/regexp/) on regular expressions.



## Available commands

```
# Run the full test suite from the standard library
$ make test

# Compare bypass results with the standard library using random patterns from go-fuzz
$ make fuzz

# Generate a chart from one benchmark
$ make chart

# Fast benchmarks
$ make bench
```


## Full benchmark results:

Benchmarks with an input string of length N=~1000 characters to detect linear behaviour easily:

```
$ make benchlong

Pattern: ^xxy
BenchmarkRegexpBypass/Prefix/bypass-8         	  500000	        34.4 ns/op   # New matcher implementation
BenchmarkRegexpBypass/Prefix/native-8         	 2000000	         8.68 ns/op  # strings.HasPrefix
BenchmarkRegexpBypass/Prefix/std-8            	  100000	       138 ns/op     # Go's current standard library
BenchmarkRegexpBypass/Prefix/stddfa-8         	  100000	       134 ns/op     # DFA branch from http://github.com/matloob/regexp
BenchmarkRegexpBypass/Prefix/glob-8           	 2000000	         7.32 ns/op  # Equivalent glob pattern with http://github.com/gobwas/glob
BenchmarkRegexpBypass/Prefix/pcre-8           	  100000	       186 ns/op     # PCRE http://github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre
BenchmarkRegexpBypass/Prefix/regexp2-8        	   10000	      1480 ns/op     # http://github.com/dlclark/regexp2
BenchmarkRegexpBypass/Prefix/rust-8           	  100000	       128 ns/op     # Rust regexp engine http://github.com/BurntSushi/rure-go

Pattern: xx
BenchmarkRegexpBypass/Literal/bypass-8        	  500000	        28.1 ns/op
BenchmarkRegexpBypass/Literal/native-8        	 1000000	        21.7 ns/op
BenchmarkRegexpBypass/Literal/std-8           	  100000	       139 ns/op
BenchmarkRegexpBypass/Literal/stddfa-8        	  100000	       143 ns/op
BenchmarkRegexpBypass/Literal/glob-8          	  500000	        31.2 ns/op
BenchmarkRegexpBypass/Literal/pcre-8          	   50000	       213 ns/op
BenchmarkRegexpBypass/Literal/regexp2-8       	   10000	      1734 ns/op
BenchmarkRegexpBypass/Literal/rust-8          	  100000	       144 ns/op

Pattern: xxy
BenchmarkRegexpBypass/LiteralN/bypass-8       	  100000	       205 ns/op
BenchmarkRegexpBypass/LiteralN/native-8       	  100000	       197 ns/op
BenchmarkRegexpBypass/LiteralN/std-8          	   50000	       301 ns/op
BenchmarkRegexpBypass/LiteralN/stddfa-8       	   50000	       276 ns/op
BenchmarkRegexpBypass/LiteralN/glob-8         	  100000	       202 ns/op
BenchmarkRegexpBypass/LiteralN/pcre-8         	   20000	       625 ns/op
BenchmarkRegexpBypass/LiteralN/regexp2-8      	    2000	      5383 ns/op
BenchmarkRegexpBypass/LiteralN/rust-8         	  100000	       153 ns/op

Pattern: xxy$
BenchmarkRegexpBypass/Suffix/bypass-8         	  300000	        47.3 ns/op
BenchmarkRegexpBypass/Suffix/native-8         	 2000000	         8.98 ns/op
BenchmarkRegexpBypass/Suffix/std-8            	   30000	       466 ns/op
BenchmarkRegexpBypass/Suffix/stddfa-8         	   30000	       425 ns/op
BenchmarkRegexpBypass/Suffix/glob-8           	 2000000	         8.47 ns/op
BenchmarkRegexpBypass/Suffix/pcre-8           	     500	     35238 ns/op
BenchmarkRegexpBypass/Suffix/regexp2-8        	    3000	      6142 ns/op
BenchmarkRegexpBypass/Suffix/rust-8           	  100000	       130 ns/op

Pattern: xxy$
BenchmarkRegexpBypass/SuffixN/bypass-8        	  300000	        48.2 ns/op
BenchmarkRegexpBypass/SuffixN/native-8        	 1000000	        10.5 ns/op
BenchmarkRegexpBypass/SuffixN/std-8           	   50000	       398 ns/op
BenchmarkRegexpBypass/SuffixN/stddfa-8        	   30000	       403 ns/op
BenchmarkRegexpBypass/SuffixN/glob-8          	 1000000	        11.1 ns/op
BenchmarkRegexpBypass/SuffixN/pcre-8          	   20000	       800 ns/op
BenchmarkRegexpBypass/SuffixN/regexp2-8       	    3000	      6480 ns/op
BenchmarkRegexpBypass/SuffixN/rust-8          	  100000	       139 ns/op

Pattern: ^xxxxy$
BenchmarkRegexpBypass/Exact/bypass-8          	 2000000	         7.71 ns/op
BenchmarkRegexpBypass/Exact/native-8          	 3000000	         3.72 ns/op
BenchmarkRegexpBypass/Exact/std-8             	  200000	        74.5 ns/op
BenchmarkRegexpBypass/Exact/stddfa-8          	  200000	        86.3 ns/op
BenchmarkRegexpBypass/Exact/glob-8            	 2000000	         5.92 ns/op
BenchmarkRegexpBypass/Exact/pcre-8            	  100000	       208 ns/op
BenchmarkRegexpBypass/Exact/regexp2-8         	   10000	      1628 ns/op
BenchmarkRegexpBypass/Exact/rust-8            	  100000	       169 ns/op

Pattern: x{2}
BenchmarkRegexpBypass/Repeat/bypass-8         	  200000	        65.6 ns/op
BenchmarkRegexpBypass/Repeat/std-8            	  100000	       165 ns/op
BenchmarkRegexpBypass/Repeat/stddfa-8         	  100000	       169 ns/op
BenchmarkRegexpBypass/Repeat/glob-8           	  300000	        50.6 ns/op
BenchmarkRegexpBypass/Repeat/pcre-8           	   20000	       677 ns/op
BenchmarkRegexpBypass/Repeat/regexp2-8        	    5000	      3880 ns/op
BenchmarkRegexpBypass/Repeat/rust-8           	  100000	       170 ns/op

Pattern: x.xy$
BenchmarkRegexpBypass/DotSuffix/bypass-8      	  200000	       119 ns/op
BenchmarkRegexpBypass/DotSuffix/std-8         	     200	     69215 ns/op
BenchmarkRegexpBypass/DotSuffix/stddfa-8      	     200	     74374 ns/op
BenchmarkRegexpBypass/DotSuffix/glob-8        	     200	     56987 ns/op
BenchmarkRegexpBypass/DotSuffix/pcre-8        	     300	     45922 ns/op
BenchmarkRegexpBypass/DotSuffix/regexp2-8     	     100	    132169 ns/op
BenchmarkRegexpBypass/DotSuffix/rust-8        	  100000	       138 ns/op

Pattern: x.xy$
BenchmarkRegexpBypass/DotSuffixN/bypass-8     	  500000	        41.1 ns/op
BenchmarkRegexpBypass/DotSuffixN/std-8        	     200	     66794 ns/op
BenchmarkRegexpBypass/DotSuffixN/stddfa-8     	     200	     72523 ns/op
BenchmarkRegexpBypass/DotSuffixN/glob-8       	     300	     59528 ns/op
BenchmarkRegexpBypass/DotSuffixN/pcre-8       	     500	     39492 ns/op
BenchmarkRegexpBypass/DotSuffixN/regexp2-8    	     100	    106040 ns/op
BenchmarkRegexpBypass/DotSuffixN/rust-8       	  100000	       153 ns/op

Pattern: .*yxx$
BenchmarkRegexpBypass/DotStarSuffix/bypass-8  	  300000	        53.9 ns/op
BenchmarkRegexpBypass/DotStarSuffix/std-8     	     300	     51752 ns/op
BenchmarkRegexpBypass/DotStarSuffix/stddfa-8  	     200	     50095 ns/op
BenchmarkRegexpBypass/DotStarSuffix/glob-8    	  200000	        53.2 ns/op
BenchmarkRegexpBypass/DotStarSuffix/pcre-8    	    1000	     14832 ns/op
BenchmarkRegexpBypass/DotStarSuffix/regexp2-8 	       1	  20395684 ns/op
BenchmarkRegexpBypass/DotStarSuffix/rust-8    	  100000	       175 ns/op

Pattern: ^xxxx.*yxx$
BenchmarkRegexpBypass/PrefixDotStarSuffix/bypass-8         	  100000	       127 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/std-8            	     500	     32925 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/stddfa-8         	     500	     37920 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/glob-8           	     500	     25808 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/pcre-8           	    1000	     13135 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/regexp2-8        	     500	     38218 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/rust-8           	    5000	      2362 ns/op

Pattern: ^xxxy.*
BenchmarkRegexpBypass/PrefixDotStar/bypass-8               	  300000	        42.8 ns/op
BenchmarkRegexpBypass/PrefixDotStar/std-8                  	  100000	       147 ns/op
BenchmarkRegexpBypass/PrefixDotStar/stddfa-8               	  100000	       156 ns/op
BenchmarkRegexpBypass/PrefixDotStar/glob-8                 	  100000	       226 ns/op
BenchmarkRegexpBypass/PrefixDotStar/pcre-8                 	  100000	       235 ns/op
BenchmarkRegexpBypass/PrefixDotStar/regexp2-8              	   10000	      1522 ns/op
BenchmarkRegexpBypass/PrefixDotStar/rust-8                 	  100000	       170 ns/op

Pattern: x.y
BenchmarkRegexpBypass/LateDot/bypass-8                     	  200000	       104 ns/op
BenchmarkRegexpBypass/LateDot/std-8                        	     300	     57363 ns/op
BenchmarkRegexpBypass/LateDot/stddfa-8                     	     200	     65799 ns/op
BenchmarkRegexpBypass/LateDot/glob-8                       	     300	     60086 ns/op
BenchmarkRegexpBypass/LateDot/pcre-8                       	     500	     35793 ns/op
BenchmarkRegexpBypass/LateDot/regexp2-8                    	     100	    106637 ns/op
BenchmarkRegexpBypass/LateDot/rust-8                       	   10000	      2764 ns/op

Pattern: x.y
BenchmarkRegexpBypass/LateDotN/bypass-8                    	  200000	        61.0 ns/op
BenchmarkRegexpBypass/LateDotN/std-8                       	     300	     58058 ns/op
BenchmarkRegexpBypass/LateDotN/stddfa-8                    	     300	     61222 ns/op
BenchmarkRegexpBypass/LateDotN/glob-8                      	     300	     59346 ns/op
BenchmarkRegexpBypass/LateDotN/pcre-8                      	   20000	       724 ns/op
BenchmarkRegexpBypass/LateDotN/regexp2-8                   	     100	    116886 ns/op
BenchmarkRegexpBypass/LateDotN/rust-8                      	    5000	      2751 ns/op

Pattern: x.y
BenchmarkRegexpBypass/LateDotHard/bypass-8                 	    1000	     17863 ns/op
BenchmarkRegexpBypass/LateDotHard/std-8                    	     500	     27023 ns/op
BenchmarkRegexpBypass/LateDotHard/stddfa-8                 	     500	     31670 ns/op
BenchmarkRegexpBypass/LateDotHard/glob-8                   	     300	     39981 ns/op
BenchmarkRegexpBypass/LateDotHard/pcre-8                   	    1000	     18614 ns/op
BenchmarkRegexpBypass/LateDotHard/regexp2-8                	     300	     58746 ns/op
BenchmarkRegexpBypass/LateDotHard/rust-8                   	    5000	      2886 ns/op

Pattern: x.y
BenchmarkRegexpBypass/LateDotHardN/bypass-8                	    1000	     19399 ns/op
BenchmarkRegexpBypass/LateDotHardN/std-8                   	     500	     29259 ns/op
BenchmarkRegexpBypass/LateDotHardN/stddfa-8                	     500	     29612 ns/op
BenchmarkRegexpBypass/LateDotHardN/glob-8                  	     300	     39950 ns/op
BenchmarkRegexpBypass/LateDotHardN/pcre-8                  	    1000	     19235 ns/op
BenchmarkRegexpBypass/LateDotHardN/regexp2-8               	     300	     57233 ns/op
BenchmarkRegexpBypass/LateDotHardN/rust-8                  	    5000	      2425 ns/op

Pattern: x....y
BenchmarkRegexpBypass/LateDotHarder/bypass-8               	    1000	     16617 ns/op
BenchmarkRegexpBypass/LateDotHarder/std-8                  	     200	     78353 ns/op
BenchmarkRegexpBypass/LateDotHarder/stddfa-8               	     200	     71691 ns/op
BenchmarkRegexpBypass/LateDotHarder/glob-8                 	     200	    101385 ns/op
BenchmarkRegexpBypass/LateDotHarder/pcre-8                 	     500	     44048 ns/op
BenchmarkRegexpBypass/LateDotHarder/regexp2-8              	     100	    122998 ns/op
BenchmarkRegexpBypass/LateDotHarder/rust-8                 	    5000	      2407 ns/op

Pattern: ☺....y
BenchmarkRegexpBypass/LateDotUnicode/bypass-8              	     500	     29597 ns/op
BenchmarkRegexpBypass/LateDotUnicode/std-8                 	     100	    107833 ns/op
BenchmarkRegexpBypass/LateDotUnicode/stddfa-8              	     100	    113315 ns/op
BenchmarkRegexpBypass/LateDotUnicode/regexp2-8             	     100	    123206 ns/op
BenchmarkRegexpBypass/LateDotUnicode/rust-8                	    2000	      6485 ns/op

Pattern: a.+b.+c
BenchmarkRegexpBypass/LateFail/bypass-8                    	    1000	     15707 ns/op
BenchmarkRegexpBypass/LateFail/std-8                       	    1000	     17666 ns/op
BenchmarkRegexpBypass/LateFail/stddfa-8                    	    1000	     17619 ns/op
BenchmarkRegexpBypass/LateFail/glob-8                      	     500	     23591 ns/op
BenchmarkRegexpBypass/LateFail/pcre-8                      	       2	   6658587 ns/op
BenchmarkRegexpBypass/LateFail/regexp2-8                   	       1	  15896990 ns/op
BenchmarkRegexpBypass/LateFail/rust-8                      	   20000	       600 ns/op

Pattern: x.+y
BenchmarkRegexpBypass/LateDotPlus/bypass-8                 	     200	     75648 ns/op
BenchmarkRegexpBypass/LateDotPlus/std-8                    	     200	     86419 ns/op
BenchmarkRegexpBypass/LateDotPlus/stddfa-8                 	     200	     82993 ns/op
BenchmarkRegexpBypass/LateDotPlus/glob-8                   	     100	    170544 ns/op
BenchmarkRegexpBypass/LateDotPlus/pcre-8                   	       2	   6565913 ns/op
BenchmarkRegexpBypass/LateDotPlus/regexp2-8                	       1	  16193609 ns/op
BenchmarkRegexpBypass/LateDotPlus/rust-8                   	    5000	      2448 ns/op

Pattern: x.+y
BenchmarkRegexpBypass/LateDotPlusN/bypass-8                	     200	     79618 ns/op
BenchmarkRegexpBypass/LateDotPlusN/std-8                   	     200	     75249 ns/op
BenchmarkRegexpBypass/LateDotPlusN/stddfa-8                	     200	     90363 ns/op
BenchmarkRegexpBypass/LateDotPlusN/glob-8                  	     100	    181905 ns/op
BenchmarkRegexpBypass/LateDotPlusN/pcre-8                  	       2	   7292755 ns/op
BenchmarkRegexpBypass/LateDotPlusN/regexp2-8               	       1	  17288483 ns/op
BenchmarkRegexpBypass/LateDotPlusN/rust-8                  	    5000	      2710 ns/op

Pattern: [0-9a-z]
BenchmarkRegexpBypass/LateClass/bypass-8                   	    5000	      3361 ns/op
BenchmarkRegexpBypass/LateClass/std-8                      	     500	     31498 ns/op
BenchmarkRegexpBypass/LateClass/stddfa-8                   	     500	     31792 ns/op
BenchmarkRegexpBypass/LateClass/pcre-8                     	     500	     25573 ns/op
BenchmarkRegexpBypass/LateClass/regexp2-8                  	    1000	     13540 ns/op
BenchmarkRegexpBypass/LateClass/rust-8                     	   10000	      2758 ns/op

Pattern: xxxx[0-9a-z]
BenchmarkRegexpBypass/LateClassHard/bypass-8               	    2000	      7868 ns/op
BenchmarkRegexpBypass/LateClassHard/std-8                  	    1000	     21710 ns/op
BenchmarkRegexpBypass/LateClassHard/stddfa-8               	     500	     23715 ns/op
BenchmarkRegexpBypass/LateClassHard/pcre-8                 	     500	     29782 ns/op
BenchmarkRegexpBypass/LateClassHard/regexp2-8              	     500	     24984 ns/op
BenchmarkRegexpBypass/LateClassHard/rust-8                 	    5000	      2798 ns/op

Pattern: [0-9a-z]{5}
BenchmarkRegexpBypass/LateClassHarder/bypass-8             	     500	     43753 ns/op
BenchmarkRegexpBypass/LateClassHarder/std-8                	     200	     64930 ns/op
BenchmarkRegexpBypass/LateClassHarder/stddfa-8             	     200	     67180 ns/op
BenchmarkRegexpBypass/LateClassHarder/pcre-8               	     500	     35226 ns/op
BenchmarkRegexpBypass/LateClassHarder/regexp2-8            	     100	    143314 ns/op
BenchmarkRegexpBypass/LateClassHarder/rust-8               	    5000	      2846 ns/op

Pattern: [^b]
BenchmarkRegexpBypass/NegativeClass/bypass-8               	   20000	       799 ns/op
BenchmarkRegexpBypass/NegativeClass/std-8                  	     500	     29913 ns/op
BenchmarkRegexpBypass/NegativeClass/stddfa-8               	     500	     29681 ns/op
BenchmarkRegexpBypass/NegativeClass/glob-8                 	    5000	      3556 ns/op
BenchmarkRegexpBypass/NegativeClass/pcre-8                 	     500	     32610 ns/op
BenchmarkRegexpBypass/NegativeClass/regexp2-8              	    1000	     14334 ns/op
BenchmarkRegexpBypass/NegativeClass/rust-8                 	    5000	      2686 ns/op

Pattern: [^b]
BenchmarkRegexpBypass/NegativeClassN/bypass-8              	   20000	       873 ns/op
BenchmarkRegexpBypass/NegativeClassN/std-8                 	     500	     32551 ns/op
BenchmarkRegexpBypass/NegativeClassN/stddfa-8              	     500	     36151 ns/op
BenchmarkRegexpBypass/NegativeClassN/glob-8                	    5000	      3259 ns/op
BenchmarkRegexpBypass/NegativeClassN/pcre-8                	     500	     30482 ns/op
BenchmarkRegexpBypass/NegativeClassN/regexp2-8             	    1000	     14101 ns/op
BenchmarkRegexpBypass/NegativeClassN/rust-8                	   10000	      2583 ns/op

Pattern: [^b]$
BenchmarkRegexpBypass/NegativeClassSuffixN/bypass-8        	  500000	        30.6 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/std-8           	     300	     41114 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/stddfa-8        	     300	     61502 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/glob-8          	     500	     25574 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/pcre-8          	     500	     35580 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/regexp2-8       	     100	    104536 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/rust-8          	  100000	       163 ns/op

Pattern: [^a][^b]
BenchmarkRegexpBypass/NegativeClass2/bypass-8              	    1000	     16108 ns/op
BenchmarkRegexpBypass/NegativeClass2/std-8                 	     300	     57757 ns/op
BenchmarkRegexpBypass/NegativeClass2/stddfa-8              	     300	     57009 ns/op
BenchmarkRegexpBypass/NegativeClass2/glob-8                	     300	     40766 ns/op
BenchmarkRegexpBypass/NegativeClass2/pcre-8                	     500	     33185 ns/op
BenchmarkRegexpBypass/NegativeClass2/regexp2-8             	     100	    106490 ns/op
BenchmarkRegexpBypass/NegativeClass2/rust-8                	    5000	      2773 ns/op

Pattern: a$a$
BenchmarkRegexpBypass/Unmatchable/bypass-8                 	 3000000	         6.78 ns/op
BenchmarkRegexpBypass/Unmatchable/std-8                    	     300	     57765 ns/op
BenchmarkRegexpBypass/Unmatchable/stddfa-8                 	     200	     70359 ns/op
BenchmarkRegexpBypass/Unmatchable/pcre-8                   	     500	     32670 ns/op
BenchmarkRegexpBypass/Unmatchable/regexp2-8                	     100	    106580 ns/op
BenchmarkRegexpBypass/Unmatchable/rust-8                   	  100000	       167 ns/op

Pattern: abc|abd
BenchmarkRegexpBypass/SimpleAltPrefix/bypass-8             	    2000	     14311 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/std-8                	     500	     34378 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/stddfa-8             	     500	     44655 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/glob-8               	   30000	       659 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/pcre-8               	     500	     31229 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/regexp2-8            	     200	     66690 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/rust-8               	   10000	      2368 ns/op

Pattern: png|jpg
BenchmarkRegexpBypass/SimpleAlt/bypass-8                   	  300000	        62.2 ns/op
BenchmarkRegexpBypass/SimpleAlt/std-8                      	     200	     58183 ns/op
BenchmarkRegexpBypass/SimpleAlt/stddfa-8                   	     300	     56126 ns/op
BenchmarkRegexpBypass/SimpleAlt/glob-8                     	  100000	       195 ns/op
BenchmarkRegexpBypass/SimpleAlt/pcre-8                     	     500	     44139 ns/op
BenchmarkRegexpBypass/SimpleAlt/regexp2-8                  	    1000	     17955 ns/op
BenchmarkRegexpBypass/SimpleAlt/rust-8                     	   50000	       348 ns/op

Pattern: png|jpg
BenchmarkRegexpBypass/SimpleAltN/bypass-8                  	  200000	       100 ns/op
BenchmarkRegexpBypass/SimpleAltN/std-8                     	     300	     50876 ns/op
BenchmarkRegexpBypass/SimpleAltN/stddfa-8                  	     300	     50976 ns/op
BenchmarkRegexpBypass/SimpleAltN/glob-8                    	  100000	       185 ns/op
BenchmarkRegexpBypass/SimpleAltN/pcre-8                    	   20000	       650 ns/op
BenchmarkRegexpBypass/SimpleAltN/regexp2-8                 	    1000	     15948 ns/op
BenchmarkRegexpBypass/SimpleAltN/rust-8                    	   50000	       355 ns/op

Pattern: (?:png|jpg)$
BenchmarkRegexpBypass/SimpleSuffixAlt/bypass-8             	     300	     53622 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/std-8                	     300	     54620 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/stddfa-8             	     300	     52643 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/glob-8               	  100000	       189 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/pcre-8               	     300	     54433 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/regexp2-8            	    1000	     14755 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/rust-8               	  100000	       142 ns/op

Pattern: (?:png$)|(?:jpg$)
BenchmarkRegexpBypass/SimpleAltSuffix/bypass-8             	  300000	        52.2 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/std-8                	     300	     55278 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/stddfa-8             	     300	     49597 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/glob-8               	  100000	       199 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/pcre-8               	     300	     53032 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/regexp2-8            	    1000	     14994 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/rust-8               	  100000	       156 ns/op

Pattern: [^a]*a
BenchmarkRegexpBypass/CharExclude/bypass-8                 	     500	     26639 ns/op
BenchmarkRegexpBypass/CharExclude/std-8                    	     500	     27210 ns/op
BenchmarkRegexpBypass/CharExclude/stddfa-8                 	     500	     28405 ns/op
BenchmarkRegexpBypass/CharExclude/pcre-8                   	   20000	       719 ns/op
BenchmarkRegexpBypass/CharExclude/regexp2-8                	    2000	      6274 ns/op
BenchmarkRegexpBypass/CharExclude/rust-8                   	    5000	      2606 ns/op

Pattern: ^(.*)$
BenchmarkRegexpBypass/RouterSlow/bypass-8                  	     500	     25224 ns/op
BenchmarkRegexpBypass/RouterSlow/std-8                     	     500	     25486 ns/op
BenchmarkRegexpBypass/RouterSlow/stddfa-8                  	     500	     29704 ns/op
BenchmarkRegexpBypass/RouterSlow/pcre-8                    	   10000	      2126 ns/op
BenchmarkRegexpBypass/RouterSlow/regexp2-8                 	    3000	      6142 ns/op
BenchmarkRegexpBypass/RouterSlow/rust-8                    	   10000	      2416 ns/op

Pattern: ^(.*)/index\.[a-z]{3}$
BenchmarkRegexpBypass/RouterSlowFirstPass/bypass-8         	    1000	     24885 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/std-8            	    1000	     20593 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/stddfa-8         	    1000	     20422 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/glob-8           	     500	     31704 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/pcre-8           	    5000	      3049 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/regexp2-8        	    2000	      7958 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/rust-8           	    5000	      2354 ns/op

Pattern: ^([^/]*)/index\.[a-z]{3}$
BenchmarkRegexpBypass/RouterFastFirstPass/bypass-8         	     500	     33215 ns/op
BenchmarkRegexpBypass/RouterFastFirstPass/std-8            	     300	     35011 ns/op
BenchmarkRegexpBypass/RouterFastFirstPass/stddfa-8         	     500	     34666 ns/op
BenchmarkRegexpBypass/RouterFastFirstPass/pcre-8           	   20000	       840 ns/op
BenchmarkRegexpBypass/RouterFastFirstPass/regexp2-8        	    2000	      6158 ns/op
BenchmarkRegexpBypass/RouterFastFirstPass/rust-8           	    5000	      2723 ns/op

Pattern: ^([^/]*)/index\.[a-z]{3}$
BenchmarkRegexpBypass/RouterFastFirstPassN/bypass-8        	  200000	       103 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/std-8           	     500	     36012 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/stddfa-8        	     500	     39198 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/pcre-8          	    1000	     21429 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/regexp2-8       	     200	     81623 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/rust-8          	    5000	      2797 ns/op
```
