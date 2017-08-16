// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package regexp

import (
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

// byPassOp is a custom Op understood by the byPassProgLinear matcher
type byPassOp uint8

const (
	byPassOpLiteral           byPassOp = iota + 1 // `abc`
	byPassOpCharClass                             // `[a-z]`
	byPassOpNegativeCharClass                     // `[^a]` (supports only a single character)
	byPassOpAnyChar                               // [\w\W]
)

// byPassStep is a step in the matching algorithm of byPassProgLinear
type byPassStep struct {
	op          byPassOp
	classes     []rune // storage for byPassOpCharClass
	literal     string // storage for byPassOpLiteral
	char        rune   // storage for byPassOpNegativeCharClass
	length      int    // number of Runes to match
	minWidth    int    // minimum number of bytes
	maxWidth    int    // maximum number of bytes, -1 if unknown
	anchored    bool   // true if we are anchored from the beginning or from the end
	anchorIndex int    // number of runes, can be negative if starting from the end

}

// byPassProg is the main interface we expose to the rest of the package.
// TODO: add other methods
type byPassProg interface {
	MatchString(s string) (matched bool)
}

var notByPass byPassProg = nil

// byPassProgLinear is the main matcher for fixed-size anchored patterns, or fixed-size single-step unanchored patterns
type byPassProgLinear struct {
	steps         []*byPassStep // Steps to execute
	anchoredBegin bool
	anchoredEnd   bool
	unmatchable   bool // if true, this pattern will never match (e.g. `a$a`)
	length        int  // number of Runes
	minWidth      int  // minimum number of bytes
	maxWidth      int  // maximum number of bytes, -1 if unknown
}

// byPassProgAlternate can match top-level alternations like `jpg|png`
type byPassProgAlternate struct {
	progs []byPassProg // one byPassProg for each part of the alternation
}

// byPassProgFirstPass can match fixed-length prefixes and suffixes in a complex regexp (e.g. `^aa(c*)bb$`)
type byPassProgFirstPass struct {
	prefixProg *byPassProgLinear
	suffixProg *byPassProgLinear
	regexp     *Regexp // A new Regexp that matches the rest of the pattern after prefix & suffix were matched.
}

// nextRunesWidth returns the number of bytes that encode the next `n` runes
func nextRunesWidth(s string, n int) (width int) {
	for i := 0; i < n && width < len(s); i++ {
		_, w := utf8.DecodeRuneInString(s[width:])
		width += w
	}
	return width
}

// lastRunesWidth returns the number of bytes that encode the last `n` runes
func lastRunesWidth(s string, n int) (width int) {
	for i := 0; i < n; i++ {
		if width >= len(s) {
			return -1
		}
		_, w := utf8.DecodeLastRuneInString(s[:len(s)-width])
		width += w
	}
	return width
}

// hasOps returns true if the tree contains any of these operations
func hasOps(trees []*syntax.Regexp, ops []syntax.Op) bool {
	for _, child := range trees {
		for _, op := range ops {
			if child.Op == op {
				return true
			}
		}
		if hasOps(child.Sub, ops) {
			return true
		}
	}
	return false
}

// compileByPass transforms a tree into a byPassProg if possible
func compileByPass(tree *syntax.Regexp) byPassProg {

	// In case the first level is an alternate, we compile multiple sub-progs.
	if tree.Op == syntax.OpAlternate {
		progalt := &byPassProgAlternate{}
		for _, alt := range tree.Sub {
			subprog := compileByPass(alt)
			if subprog == notByPass {
				return notByPass
			}
			progalt.progs = append(progalt.progs, subprog)
		}
		return progalt
	}

	// Try to compile the regexp as a single fixed-size pattern
	prog := &byPassProgLinear{}

	bailout := prog.traverseTree(tree)

	// We only use bypass when we are sure not to need any backtracking
	if !bailout && !prog.anchoredBegin && !prog.anchoredEnd && len(prog.steps) > 1 {
		bailout = true
	}

	// In some cases we can still extract a fixed-length anchored prefix & suffix to run as first pass
	if bailout && tree.Op == syntax.OpConcat && len(tree.Sub) > 1 {

		firstpassprog := &byPassProgFirstPass{}

		compileByPassPartialPrefix(firstpassprog, tree)
		compileByPassPartialSuffix(firstpassprog, tree)

		if firstpassprog.prefixProg != nil || firstpassprog.suffixProg != nil {
			return firstpassprog
		}

	}

	// None of the optimizations are available, bailout to the other matchers.
	if bailout {
		return notByPass
	}

	prog.computeWidth()
	return prog
}

// compileByPassPartialPrefix finds out if a fixed-length prefix can be extracted from the tree
func compileByPassPartialPrefix(firstpassprog *byPassProgFirstPass, tree *syntax.Regexp) {

	if tree.Sub[0].Op == syntax.OpBeginText {

		prefixProg := &byPassProgLinear{}
		i := 0
		validsteps := 0

		// Find the longest fixed-length prefix supported by a byPassProgLinear
		for ; i < len(tree.Sub); i++ {
			if prefixProg.traverseTree(tree.Sub[i]) {
				break
			}
			validsteps = len(prefixProg.steps)
		}

		if validsteps > 0 && i > 1 {
			if !hasOps(tree.Sub[i:], []syntax.Op{syntax.OpBeginText, syntax.OpBeginLine, syntax.OpWordBoundary, syntax.OpNoWordBoundary}) {

				prefixProg.steps = prefixProg.steps[:validsteps]
				prefixProg.computeWidth()
				firstpassprog.prefixProg = prefixProg

				// Build and compile a new Regexp for the rest of the pattern (`^aa(c*)` => `^(c*)`)
				tree.Sub = append(tree.Sub[0:1], tree.Sub[i:]...)
				// TODO longest?
				// Error is safe to ignore because it was already compiled earlier
				re, err := compileParsed(tree, false)
				if err != nil {
					panic(err)
				}
				firstpassprog.regexp = re

			}
		}
	}
}

// compileByPassPartialPrefix finds out if a fixed-length suffix can be extracted from the tree
func compileByPassPartialSuffix(firstpassprog *byPassProgFirstPass, tree *syntax.Regexp) {

	if len(tree.Sub) > 1 && tree.Sub[len(tree.Sub)-1].Op == syntax.OpEndText {

		suffixProg := &byPassProgLinear{}
		lastInvalid := 0

		// Find the longest fixed-length suffix supported by a byPassProgLinear
		for i := 0; i < len(tree.Sub); i++ {
			// Reset the prog until we have a full suffix
			if suffixProg.traverseTree(tree.Sub[i]) {
				suffixProg = &byPassProgLinear{}
				lastInvalid = i
			}
		}

		if len(suffixProg.steps) > 0 && lastInvalid < len(tree.Sub)-1 {
			if !hasOps(tree.Sub[:lastInvalid+1], []syntax.Op{syntax.OpEndText, syntax.OpEndLine, syntax.OpWordBoundary, syntax.OpNoWordBoundary}) {

				suffixProg.computeWidth()
				firstpassprog.suffixProg = suffixProg

				// Build and compile a new Regexp for the rest of the pattern (`(c*)bb$` => `(c*)$`)
				tree.Sub = append(tree.Sub[:lastInvalid+1], tree.Sub[len(tree.Sub)-1])
				// TODO longest?
				// Error is safe to ignore because it was already compiled earlier
				re, err := compileParsed(tree, false)
				if err != nil {
					panic(err)
				}
				firstpassprog.regexp = re
			}
		}
	}
}

// compileParsed is a shorter version of compile() that takes a parsed tree as input
// TODO: factorize this with the regular compile() function
func compileParsed(re *syntax.Regexp, longest bool) (*Regexp, error) {

	maxCap := re.MaxCap()
	capNames := re.CapNames()

	re = re.Simplify()
	prog, err := syntax.Compile(re)
	if err != nil {
		return nil, err
	}
	regexp := &Regexp{
		regexpRO: regexpRO{
			// expr:        expr,
			prog:        prog,
			onepass:     compileOnePass(prog),
			numSubexp:   maxCap,
			subexpNames: capNames,
			cond:        prog.StartCond(),
			longest:     longest,
		},
	}
	if regexp.onepass == notOnePass {
		regexp.prefix, regexp.prefixComplete = prog.Prefix()
	} else {
		regexp.prefix, regexp.prefixComplete, regexp.prefixEnd = onePassPrefix(prog)
	}
	if regexp.prefix != "" {
		// TODO(rsc): Remove this allocation by adding
		// IndexString to package bytes.
		regexp.prefixBytes = []byte(regexp.prefix)
		regexp.prefixRune, _ = utf8.DecodeRuneInString(regexp.prefix)
	}
	return regexp, nil
}

// traverseTree visits each node of the parsed regexp to detect fixed-size patterns
func (prog *byPassProgLinear) traverseTree(tree *syntax.Regexp) (bailout bool) {

	// TODO make sure other flag combinations can't be supported too
	if tree.Flags != syntax.Perl && tree.Flags != syntax.POSIX && tree.Flags != syntax.Perl|syntax.WasDollar {
		return true
	}

	var step *byPassStep

	// https://golang.org/pkg/regexp/syntax/#Op
	switch tree.Op {

	case syntax.OpConcat:
		for _, sub := range tree.Sub {
			if prog.traverseTree(sub) {
				return true
			}
		}
	case syntax.OpRepeat:
		// We only support fixed-length patterns
		if tree.Min != tree.Max {
			return true
		}
		for i := 0; i < tree.Min; i++ {
			if prog.traverseTree(tree.Sub[0]) {
				return true
			}
		}
	case syntax.OpLiteral:

		// If the previous step was also an OpLiteral, append to it
		if len(prog.steps) > 0 && prog.steps[len(prog.steps)-1].op == byPassOpLiteral && !prog.anchoredEnd {
			prevstep := prog.steps[len(prog.steps)-1]
			prevstep.literal += string(tree.Rune)
			prevstep.length += len(tree.Rune)
			prevstep.minWidth += len(string(tree.Rune))
			prevstep.maxWidth += len(string(tree.Rune))
			prog.length += len(tree.Rune)
			break
		}

		step = &byPassStep{
			op:       byPassOpLiteral,
			literal:  string(tree.Rune),
			length:   len(tree.Rune),
			minWidth: len(string(tree.Rune)),
			maxWidth: len(string(tree.Rune)),
		}

	case syntax.OpBeginText:
		if prog.length > 0 {
			prog.unmatchable = true
			return false
		}
		prog.anchoredBegin = true

	case syntax.OpEndText:
		prog.anchoredEnd = true

		if !prog.anchoredBegin {
			right := 0
			// go back to the previous steps & set their anchorIndex to the right
			for i := len(prog.steps) - 1; i >= 0; i-- {
				right -= prog.steps[i].length
				prog.steps[i].anchored = true
				prog.steps[i].anchorIndex = right
			}
		}

	case syntax.OpAnyCharNotNL:
		step = &byPassStep{
			op:       byPassOpNegativeCharClass,
			char:     rune('\n'),
			length:   1,
			minWidth: 1,
			maxWidth: -1,
		}

	case syntax.OpAnyChar:
		step = &byPassStep{
			op:       byPassOpAnyChar,
			length:   1,
			minWidth: 1,
			// TODO: could we use maxWidth:4 ? What about combining characters?
			maxWidth: -1,
		}

	case syntax.OpNoMatch:
		prog.unmatchable = true
		return false

	case syntax.OpCharClass:
		// Optimize single-character exclusion classes
		if len(tree.Rune) == 4 && tree.Rune[0] == 0 && tree.Rune[3] == utf8.MaxRune && tree.Rune[1]+2 == tree.Rune[2] {
			step = &byPassStep{
				op:       byPassOpNegativeCharClass,
				char:     tree.Rune[1] + 1,
				length:   1,
				minWidth: 1,
				maxWidth: -1,
			}
		} else {
			step = &byPassStep{
				op:       byPassOpCharClass,
				classes:  tree.Rune,
				length:   1,
				minWidth: 1,
				maxWidth: -1, // TODO we could be more precise (if [a-z] we know maxWidth=1)
			}
		}

	/*
		case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpAlternate, syntax.OpEmptyMatch,
			 syntax.OpWordBoundary, syntax.OpNoWordBoundary, syntax.OpCapture,
			 syntax.OpStar, syntax.OpPlus, syntax.OpQuest:
			 return true
	*/

	default:
		// Unsupported Op
		return true
	}

	if step != nil {

		if prog.anchoredEnd {
			prog.unmatchable = true
			return false
		}

		if prog.anchoredBegin {
			step.anchored = true
			step.anchorIndex = prog.length
		}

		prog.length += step.length
		prog.steps = append(prog.steps, step)

	}

	return false
}

func (prog *byPassProgAlternate) MatchString(s string) (matched bool) {
	for _, subprog := range prog.progs {
		if subprog.MatchString(s) {
			return true
		}
	}
	return false
}

func (prog *byPassProgFirstPass) MatchString(s string) (matched bool) {

	// Execute prefix and suffix first
	if prog.prefixProg != nil {
		if !prog.prefixProg.MatchString(s) {
			return false
		}
		s = s[nextRunesWidth(s, prog.prefixProg.length):]
	}
	if prog.suffixProg != nil {
		if !prog.suffixProg.MatchString(s) {
			return false
		}
		s = s[:len(s)-lastRunesWidth(s, prog.suffixProg.length)]
	}

	// Finally, execute the rest of the regexp with other matchers
	return prog.regexp.MatchString(s)

}

// computeWidth computes the byte length of a byPassProgLinear from its steps
func (prog *byPassProgLinear) computeWidth() {

	prog.length = 0
	for _, step := range prog.steps {
		prog.length += step.length
		prog.minWidth += step.minWidth
		if step.maxWidth != -1 && prog.maxWidth != -1 {
			prog.maxWidth += step.maxWidth
		} else {
			prog.maxWidth = -1
		}
	}

}

func (prog *byPassProgLinear) MatchString(s string) (matched bool) {

	if prog.unmatchable {
		return false
	}

	if len(s) < prog.minWidth {
		return false
	}

	// For exact matches like ^aa$, we know the number of bytes in advance
	if prog.anchoredBegin && prog.anchoredEnd && prog.maxWidth != -1 && len(s) > prog.maxWidth {
		return false
	}

	// slice window on the string, in bytes
	var begin int
	var end int

	// number of bytes in the current step.
	var stepWidth int

	for _, step := range prog.steps {

		end = len(s)
		stepWidth = -2

		// We don't have 0-length steps
		if begin >= end {
			return false
		}

		// If we are anchored, we can test a very narrow slice
		if step.anchored {
			// Anchored from the beginning
			if step.anchorIndex >= 0 {
				anchorWidth := nextRunesWidth(s, step.anchorIndex)
				if anchorWidth < begin {
					return false
				}
				begin = anchorWidth

				// Anchored from the end
			} else {
				anchorWidth := lastRunesWidth(s, -step.anchorIndex)
				if anchorWidth == -1 {
					return false
				}
				begin = end - anchorWidth
			}
			stepWidth = nextRunesWidth(s[begin:], step.length)
			end = begin + stepWidth
		}

		if stepWidth == -2 {
			stepWidth = nextRunesWidth(s[begin:], step.length)
		}

		switch step.op {
		case byPassOpLiteral:

			// In this case step.minWidth is always the expected width in bytes
			if end-begin < step.minWidth {
				return false
			} else if end-begin == step.minWidth {
				if s[begin:end] != step.literal {
					return false
				}
			} else {
				if !strings.Contains(s[begin:end], step.literal) {
					return false
				}
			}

		case byPassOpCharClass:
			isInClass := false
			for _, char := range s[begin:end] {
				for i := 0; i < len(step.classes); i += 2 {
					if step.classes[i] <= char && char <= step.classes[i+1] {
						isInClass = true
						break
					}
				}

				if isInClass {
					break
				}
			}

			if !isInClass {
				return false
			}

		case byPassOpNegativeCharClass:

			// Find the first rune that is different
			found := false
			for _, char := range s[begin:end] {
				if char != step.char {
					found = true
					break
				}
			}
			if !found {
				return false
			}

		case byPassOpAnyChar:
			// nothing to do

		}

		// go forward in the string
		begin += stepWidth

	}

	// If we are anchored to the end and didn't end at the exact end of the string, it's not a match
	if prog.anchoredEnd && begin != len(s) && len(prog.steps) > 0 {
		return false
	}

	return true

}
