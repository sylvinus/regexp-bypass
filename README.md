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

Here is a non-exhaustive list of patterns supported:

 - `^ab` and `ab$` are effectively translated to `strings.HasPrefix` and `strings.HasSuffix`
 - `a.ab$` has a fixed-length size of 4 runes so we can just scan bytes starting at the end of the string.
 - `jpg|png` is a top-level alternation of fixed-length patterns, so they are run separately until one matches
 - `(a*)bb$` has a fixed-length suffix so it is matched first on the string. If it matches, then `(a*)` is executed by the other matchers on the rest of the string.
 - `[^b]` is a single-character exclusion class, so it has a specific optimization that avoids comparing it to a byte range.

Not currently optimized but maybe in scope:

 - Fixed-length unanchored patterns with more than one step (`x.y`) that may need simple backtracking
 - Specific patterns like `[^a]*a` that could use different search algorithms

Out of scope and happily left to the other matchers:

 - Patterns with `*`, `+`, `?`, non-standard flags, nested alternations, word boundaries
 - Streaming input with `inputReader`

## What is missing currently but could still be done in the scope of this proposal

 - Add support for `Find`, `Split`, ...
 - Add support for `[]byte` as input (`re.Match()`)
 - Add support for capturing groups
 - Add a simple backtracker for fixed-length unanchored patterns with more than one step (`x.y`)
 - Avoid factoring patterns like `aa|ab` so that we can use `byPassProgAlternate`
 - Fine-tune types & fix struct alignment
 - Add benchmarks with re2
 - Compile (?:png|jpg) as a single "multi-literal"
 - Some profiling for micro-optimizations
 - Understand why `^(.*)$` is very slow with the standard library (see `Router` benchmarks below) and making our `firstpass` optimization actually slower for now

## Pros & cons

The obvious cons of this proposal are:

 - Adding more code to the standard library
 - Adding more work at compilation time.

The pros are:

 - Constant-time matching for many simple regexps
 - No overhead for regexps that are not supported
 - Go's `regexp` package is usually considered immature performance-wise. This proposal plays a small role in fixing that by adding optimizations that can reasonably be expected from the end-user.
 - This matcher keeps very little state and bypasses the mutex from `regexp.go`
 - There are already 3 different matchers in the standard library (4 with the upcoming DFA), so adding a new one for a specific kind of patterns is not surprising.
 - `regexp.MatchString("^ab")` could obviously be rewritten as `strings.HasPrefix("ab")` but sometimes it is not practical because the pattern to be matched is user-supplied or part of a long list of patterns. Examples include interactive log search or lists of paths in HTTP routers.
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

pattern: ^xxy
BenchmarkRegexpBypass/Prefix/native-8         	300000000	         9.22 ns/op  # strings.HasPrefix
BenchmarkRegexpBypass/Prefix/bypass-8         	100000000	        38.7 ns/op   # New matcher implementation
BenchmarkRegexpBypass/Prefix/std-8            	20000000	       142 ns/op     # Go's current standard library
BenchmarkRegexpBypass/Prefix/pcre-8           	20000000	       204 ns/op     # PCRE with github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre
BenchmarkRegexpBypass/Prefix/regexp2-8        	 2000000	      1817 ns/op     # Regexp2 github.com/dlclark/regexp2
BenchmarkRegexpBypass/Prefix/rust-8           	20000000	       142 ns/op     # Rust regexp engine github.com/BurntSushi/rure-go

pattern: xx
BenchmarkRegexpBypass/Literal/native-8        	100000000	        24.0 ns/op
BenchmarkRegexpBypass/Literal/bypass-8        	100000000	        47.7 ns/op
BenchmarkRegexpBypass/Literal/std-8           	20000000	       145 ns/op
BenchmarkRegexpBypass/Literal/pcre-8          	20000000	       220 ns/op
BenchmarkRegexpBypass/Literal/regexp2-8       	 2000000	      1941 ns/op
BenchmarkRegexpBypass/Literal/rust-8          	20000000	       158 ns/op

pattern: xxy
BenchmarkRegexpBypass/LiteralN/native-8       	20000000	       216 ns/op
BenchmarkRegexpBypass/LiteralN/bypass-8       	10000000	       243 ns/op
BenchmarkRegexpBypass/LiteralN/std-8          	10000000	       286 ns/op
BenchmarkRegexpBypass/LiteralN/pcre-8         	 5000000	       687 ns/op
BenchmarkRegexpBypass/LiteralN/regexp2-8      	  500000	      6086 ns/op
BenchmarkRegexpBypass/LiteralN/rust-8         	20000000	       157 ns/op

pattern: xxy$
BenchmarkRegexpBypass/Suffix/native-8         	300000000	         9.69 ns/op
BenchmarkRegexpBypass/Suffix/bypass-8         	50000000	        53.8 ns/op
BenchmarkRegexpBypass/Suffix/std-8            	10000000	       463 ns/op
BenchmarkRegexpBypass/Suffix/pcre-8           	  100000	     37899 ns/op
BenchmarkRegexpBypass/Suffix/regexp2-8        	  500000	      6527 ns/op
BenchmarkRegexpBypass/Suffix/rust-8           	20000000	       156 ns/op

pattern: xxy$
BenchmarkRegexpBypass/SuffixN/native-8        	300000000	         9.68 ns/op
BenchmarkRegexpBypass/SuffixN/bypass-8        	50000000	        51.9 ns/op
BenchmarkRegexpBypass/SuffixN/std-8           	10000000	       385 ns/op
BenchmarkRegexpBypass/SuffixN/pcre-8          	 5000000	       790 ns/op
BenchmarkRegexpBypass/SuffixN/regexp2-8       	  500000	      6368 ns/op
BenchmarkRegexpBypass/SuffixN/rust-8          	20000000	       152 ns/op

pattern: ^xxxxy$
BenchmarkRegexpBypass/Exact/native-8          	1000000000	         4.05 ns/op
BenchmarkRegexpBypass/Exact/bypass-8          	300000000	         8.50 ns/op
BenchmarkRegexpBypass/Exact/std-8             	30000000	        85.0 ns/op
BenchmarkRegexpBypass/Exact/pcre-8            	20000000	       215 ns/op
BenchmarkRegexpBypass/Exact/regexp2-8         	 2000000	      1904 ns/op
BenchmarkRegexpBypass/Exact/rust-8            	20000000	       167 ns/op

pattern: x{2}
BenchmarkRegexpBypass/Repeat/bypass-8         	50000000	        70.8 ns/op
BenchmarkRegexpBypass/Repeat/std-8            	20000000	       166 ns/op
BenchmarkRegexpBypass/Repeat/pcre-8           	 5000000	       578 ns/op
BenchmarkRegexpBypass/Repeat/regexp2-8        	 1000000	      4157 ns/op
BenchmarkRegexpBypass/Repeat/rust-8           	20000000	       174 ns/op

pattern: x.xy$
BenchmarkRegexpBypass/DotSuffix/bypass-8      	30000000	       109 ns/op
BenchmarkRegexpBypass/DotSuffix/std-8         	   50000	     69882 ns/op
BenchmarkRegexpBypass/DotSuffix/pcre-8        	  100000	     41359 ns/op
BenchmarkRegexpBypass/DotSuffix/regexp2-8     	   20000	    119813 ns/op
BenchmarkRegexpBypass/DotSuffix/rust-8        	20000000	       157 ns/op

pattern: x.xy$
BenchmarkRegexpBypass/DotSuffixN/bypass-8     	100000000	        46.0 ns/op
BenchmarkRegexpBypass/DotSuffixN/std-8        	   50000	     70092 ns/op
BenchmarkRegexpBypass/DotSuffixN/pcre-8       	  100000	     40759 ns/op
BenchmarkRegexpBypass/DotSuffixN/regexp2-8    	   30000	    121721 ns/op
BenchmarkRegexpBypass/DotSuffixN/rust-8       	20000000	       166 ns/op

pattern: .*yxx$
BenchmarkRegexpBypass/DotStarSuffix/bypass-8  	50000000	        55.8 ns/op
BenchmarkRegexpBypass/DotStarSuffix/std-8     	   50000	     52516 ns/op
BenchmarkRegexpBypass/DotStarSuffix/pcre-8    	  200000	     15466 ns/op
BenchmarkRegexpBypass/DotStarSuffix/regexp2-8 	     200	  19131652 ns/op
BenchmarkRegexpBypass/DotStarSuffix/rust-8    	20000000	       164 ns/op

pattern: ^xxxx.*yxx$
BenchmarkRegexpBypass/PrefixDotStarSuffix/bypass-8         	20000000	       120 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/std-8            	  100000	     37506 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/pcre-8           	  200000	     14080 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/regexp2-8        	  100000	     40032 ns/op
BenchmarkRegexpBypass/PrefixDotStarSuffix/rust-8           	 1000000	      2518 ns/op

pattern: ^xxxy.*
BenchmarkRegexpBypass/PrefixDotStar/bypass-8               	50000000	        47.7 ns/op
BenchmarkRegexpBypass/PrefixDotStar/std-8                  	20000000	       152 ns/op
BenchmarkRegexpBypass/PrefixDotStar/pcre-8                 	20000000	       212 ns/op
BenchmarkRegexpBypass/PrefixDotStar/regexp2-8              	 2000000	      1897 ns/op
BenchmarkRegexpBypass/PrefixDotStar/rust-8                 	20000000	       166 ns/op

pattern: x.y
BenchmarkRegexpBypass/LateDot/bypass-8                     	   50000	     61497 ns/op
BenchmarkRegexpBypass/LateDot/std-8                        	   50000	     59360 ns/op
BenchmarkRegexpBypass/LateDot/pcre-8                       	  100000	     37885 ns/op
BenchmarkRegexpBypass/LateDot/regexp2-8                    	   20000	    119353 ns/op
BenchmarkRegexpBypass/LateDot/rust-8                       	 1000000	      2570 ns/op

pattern: x.y
BenchmarkRegexpBypass/LateDotN/bypass-8                    	   50000	     60496 ns/op
BenchmarkRegexpBypass/LateDotN/std-8                       	   50000	     60218 ns/op
BenchmarkRegexpBypass/LateDotN/pcre-8                      	 5000000	       692 ns/op
BenchmarkRegexpBypass/LateDotN/regexp2-8                   	   20000	    118873 ns/op
BenchmarkRegexpBypass/LateDotN/rust-8                      	 1000000	      2554 ns/op

pattern: [0-9a-z]
BenchmarkRegexpBypass/LateClass/bypass-8                   	  500000	      6667 ns/op
BenchmarkRegexpBypass/LateClass/std-8                      	  100000	     32792 ns/op
BenchmarkRegexpBypass/LateClass/pcre-8                     	  100000	     28316 ns/op
BenchmarkRegexpBypass/LateClass/regexp2-8                  	  200000	     18434 ns/op
BenchmarkRegexpBypass/LateClass/rust-8                     	 1000000	      2534 ns/op

pattern: a.+b.+c
BenchmarkRegexpBypass/LateFail/bypass-8                    	  200000	     15945 ns/op
BenchmarkRegexpBypass/LateFail/std-8                       	  200000	     16327 ns/op
BenchmarkRegexpBypass/LateFail/pcre-8                      	     500	   7407837 ns/op
BenchmarkRegexpBypass/LateFail/regexp2-8                   	     200	  17600353 ns/op
BenchmarkRegexpBypass/LateFail/rust-8                      	 5000000	       671 ns/op

pattern: x.+y
BenchmarkRegexpBypass/LateDotPlus/bypass-8                 	   30000	     81439 ns/op
BenchmarkRegexpBypass/LateDotPlus/std-8                    	   30000	     81715 ns/op
BenchmarkRegexpBypass/LateDotPlus/pcre-8                   	     500	   6785593 ns/op
BenchmarkRegexpBypass/LateDotPlus/regexp2-8                	     200	  16680981 ns/op
BenchmarkRegexpBypass/LateDotPlus/rust-8                   	 1000000	      2615 ns/op

pattern: x.+y
BenchmarkRegexpBypass/LateDotPlusN/bypass-8                	   30000	     81512 ns/op
BenchmarkRegexpBypass/LateDotPlusN/std-8                   	   30000	     81933 ns/op
BenchmarkRegexpBypass/LateDotPlusN/pcre-8                  	     500	   6879664 ns/op
BenchmarkRegexpBypass/LateDotPlusN/regexp2-8               	     200	  16822620 ns/op
BenchmarkRegexpBypass/LateDotPlusN/rust-8                  	 1000000	      2665 ns/op

pattern: [^b]
BenchmarkRegexpBypass/NegativeClass/bypass-8               	 5000000	       735 ns/op
BenchmarkRegexpBypass/NegativeClass/std-8                  	  100000	     33254 ns/op
BenchmarkRegexpBypass/NegativeClass/pcre-8                 	  100000	     29988 ns/op
BenchmarkRegexpBypass/NegativeClass/regexp2-8              	  200000	     18552 ns/op
BenchmarkRegexpBypass/NegativeClass/rust-8                 	 1000000	      2555 ns/op

pattern: [^b]
BenchmarkRegexpBypass/NegativeClassN/bypass-8              	 5000000	       718 ns/op
BenchmarkRegexpBypass/NegativeClassN/std-8                 	  100000	     33642 ns/op
BenchmarkRegexpBypass/NegativeClassN/pcre-8                	  100000	     30110 ns/op
BenchmarkRegexpBypass/NegativeClassN/regexp2-8             	  200000	     18011 ns/op
BenchmarkRegexpBypass/NegativeClassN/rust-8                	 1000000	      2604 ns/op

pattern: [^b]$
BenchmarkRegexpBypass/NegativeClassSuffixN/bypass-8        	100000000	        30.7 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/std-8           	   50000	     55941 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/pcre-8          	  100000	     33119 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/regexp2-8       	   30000	    119525 ns/op
BenchmarkRegexpBypass/NegativeClassSuffixN/rust-8          	20000000	       168 ns/op

pattern: [^a][^b]
BenchmarkRegexpBypass/NegativeClass2/bypass-8              	   50000	     51650 ns/op
BenchmarkRegexpBypass/NegativeClass2/std-8                 	   50000	     51212 ns/op
BenchmarkRegexpBypass/NegativeClass2/pcre-8                	  100000	     35892 ns/op
BenchmarkRegexpBypass/NegativeClass2/regexp2-8             	   20000	    130207 ns/op
BenchmarkRegexpBypass/NegativeClass2/rust-8                	 1000000	      2544 ns/op

pattern: a$a$
BenchmarkRegexpBypass/Unmatchable/bypass-8                 	500000000	         7.17 ns/op
BenchmarkRegexpBypass/Unmatchable/std-8                    	   50000	     62295 ns/op
BenchmarkRegexpBypass/Unmatchable/pcre-8                   	  100000	     33563 ns/op
BenchmarkRegexpBypass/Unmatchable/regexp2-8                	   30000	    101360 ns/op
BenchmarkRegexpBypass/Unmatchable/rust-8                   	20000000	       168 ns/op

pattern: abc|abd
BenchmarkRegexpBypass/SimpleAltPrefix/bypass-8             	  100000	     38079 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/std-8                	  100000	     37399 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/pcre-8               	  100000	     29498 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/regexp2-8            	   50000	     73445 ns/op
BenchmarkRegexpBypass/SimpleAltPrefix/rust-8               	 1000000	      2504 ns/op

pattern: png|jpg
BenchmarkRegexpBypass/SimpleAlt/bypass-8                   	30000000	        82.0 ns/op
BenchmarkRegexpBypass/SimpleAlt/std-8                      	   50000	     53849 ns/op
BenchmarkRegexpBypass/SimpleAlt/pcre-8                     	  100000	     42187 ns/op
BenchmarkRegexpBypass/SimpleAlt/regexp2-8                  	  200000	     17376 ns/op
BenchmarkRegexpBypass/SimpleAlt/rust-8                     	10000000	       341 ns/op

pattern: png|jpg
BenchmarkRegexpBypass/SimpleAltN/bypass-8                  	20000000	       139 ns/op
BenchmarkRegexpBypass/SimpleAltN/std-8                     	   50000	     55150 ns/op
BenchmarkRegexpBypass/SimpleAltN/pcre-8                    	 5000000	       723 ns/op
BenchmarkRegexpBypass/SimpleAltN/regexp2-8                 	  200000	     17021 ns/op
BenchmarkRegexpBypass/SimpleAltN/rust-8                    	10000000	       338 ns/op

pattern: (?:png|jpg)$
BenchmarkRegexpBypass/SimpleSuffixAlt/bypass-8             	   50000	     55871 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/std-8                	   50000	     55329 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/pcre-8               	   50000	     50039 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/regexp2-8            	  200000	     17375 ns/op
BenchmarkRegexpBypass/SimpleSuffixAlt/rust-8               	20000000	       151 ns/op

pattern: (?:png$)|(?:jpg$)
BenchmarkRegexpBypass/SimpleAltSuffix/bypass-8             	50000000	        57.8 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/std-8                	   50000	     55472 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/pcre-8               	   50000	     54576 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/regexp2-8            	  200000	     17256 ns/op
BenchmarkRegexpBypass/SimpleAltSuffix/rust-8               	20000000	       147 ns/op

pattern: [^a]*a
BenchmarkRegexpBypass/CharExclude/bypass-8                 	  100000	     27710 ns/op
BenchmarkRegexpBypass/CharExclude/std-8                    	  100000	     27675 ns/op
BenchmarkRegexpBypass/CharExclude/pcre-8                   	 5000000	       747 ns/op
BenchmarkRegexpBypass/CharExclude/regexp2-8                	  500000	      6447 ns/op
BenchmarkRegexpBypass/CharExclude/rust-8                   	 1000000	      2533 ns/op

pattern: ^(.*)$
BenchmarkRegexpBypass/RouterSlow/bypass-8                  	  100000	     29002 ns/op
BenchmarkRegexpBypass/RouterSlow/std-8                     	  100000	     26753 ns/op
BenchmarkRegexpBypass/RouterSlow/pcre-8                    	 1000000	      2342 ns/op
BenchmarkRegexpBypass/RouterSlow/regexp2-8                 	  500000	      6460 ns/op
BenchmarkRegexpBypass/RouterSlow/rust-8                    	 1000000	      2589 ns/op

pattern: ^(.*)/index\.[a-z]{3}$
BenchmarkRegexpBypass/RouterSlowFirstPass/bypass-8         	  100000	     28802 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/std-8            	  200000	     21837 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/pcre-8           	 1000000	      2632 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/regexp2-8        	  500000	      7370 ns/op
BenchmarkRegexpBypass/RouterSlowFirstPass/rust-8           	 1000000	      2557 ns/op

pattern: ^([^/]*)/index\.[a-z]{3}$
BenchmarkRegexpBypass/RouterFastFirstPass/bypass-8         	  100000	     36118 ns/op
BenchmarkRegexpBypass/RouterFastFirstPass/std-8            	  100000	     36023 ns/op
BenchmarkRegexpBypass/RouterFastFirstPass/pcre-8           	 3000000	       851 ns/op
BenchmarkRegexpBypass/RouterFastFirstPass/regexp2-8        	  500000	      6578 ns/op

pattern: ^([^/]*)/index\.[a-z]{3}$
BenchmarkRegexpBypass/RouterFastFirstPassN/bypass-8         	30000000	       102 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/std-8            	  100000	     36414 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/pcre-8           	  200000	     21449 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/regexp2-8        	   30000	     78986 ns/op
BenchmarkRegexpBypass/RouterFastFirstPassN/rust-8           	 1000000	      2626 ns/op
```
