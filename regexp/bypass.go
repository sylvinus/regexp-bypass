// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package regexp

import (
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

// byPassOp is a custom Op understood by the matchers
type byPassOp uint8

const (
	byPassOpLiteral           byPassOp = iota + 1 // `abc`
	byPassOpCharClass                             // `[a-z]`
	byPassOpNegativeCharClass                     // `[^a]` (supports only a single character)
	byPassOpAnyChar                               // [\w\W]
)

// byPassStep is a step in the matching algorithm
type byPassStep struct {
	op             byPassOp
	classes        []rune // storage for byPassOpCharClass
	literal        string // storage for byPassOpLiteral
	char           rune   // storage for byPassOpNegativeCharClass
	length         int    // number of Runes to match
	previousLength int    // number of Runes in previous steps
	minWidth       int    // minimum number of bytes
	maxWidth       int    // maximum number of bytes, -1 if unknown
	minNextWidth   int    // minimum number of bytes needed to match from this step to the end of the pattern
	anchored       bool   // true if we are anchored from the beginning or from the end
	anchorIndex    int    // number of runes, can be negative if starting from the end
}

// byPassProg is the main interface we expose to the rest of the package.
// TODO: add other methods
type byPassProg interface {
	MatchString(s string) (matched bool)
}

var notByPass byPassProg = nil

// byPassProgAnchored is the main matcher for fixed-length anchored patterns
type byPassProgAnchored struct {
	steps         []*byPassStep // Steps to execute
	anchoredBegin bool
	anchoredEnd   bool
	unmatchable   bool // if true, this pattern will never match (e.g. `a$a`)
	length        int  // number of Runes
	minWidth      int  // minimum number of bytes
	maxWidth      int  // maximum number of bytes, -1 if unknown
}

// byPassProgUnanchored is the main matcher for fixed-length unanchored patterns
type byPassProgUnanchored struct {
	steps    []*byPassStep // Steps to execute
	length   int           // number of Runes
	minWidth int           // minimum number of bytes
	maxWidth int           // maximum number of bytes, -1 if unknown
}

// byPassProgAlternate can match top-level alternations like `jpg|png`
type byPassProgAlternate struct {
	progs []byPassProg // one byPassProg for each part of the alternation
}

// byPassProgFirstPass can match fixed-length prefixes and suffixes in a complex regexp (e.g. `^aa(c*)bb$`)
type byPassProgFirstPass struct {
	prefixProg *byPassProgAnchored
	suffixProg *byPassProgAnchored
	regexp     *Regexp // A new Regexp that matches the rest of the pattern after prefix & suffix were matched.
}

// byPassProgUnmatchable never matches anything
type byPassProgUnmatchable struct {
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

	// Try to compile the regexp as a single fixed-length pattern
	// we don't know yet if it will be anchored or not
	prog := &byPassProgAnchored{}

	bailout := prog.traverseTree(tree)

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

	if prog.unmatchable {
		return &byPassProgUnmatchable{}
	}

	prog.computeWidth()

	if prog.anchoredBegin || prog.anchoredEnd {
		return prog
	}

	// Use an unanchored prog
	return &byPassProgUnanchored{
		steps:    prog.steps,
		length:   prog.length,
		minWidth: prog.minWidth,
		maxWidth: prog.maxWidth,
	}
}

// compileByPassPartialPrefix finds out if a fixed-length prefix can be extracted from the tree
func compileByPassPartialPrefix(firstpassprog *byPassProgFirstPass, tree *syntax.Regexp) {

	if tree.Sub[0].Op == syntax.OpBeginText {

		prefixProg := &byPassProgAnchored{}
		i := 0
		validsteps := 0

		// Find the longest fixed-length prefix supported by a byPassProgAnchored
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

		suffixProg := &byPassProgAnchored{}
		lastInvalid := 0

		// Find the longest fixed-length suffix supported by a byPassProgAnchored
		for i := 0; i < len(tree.Sub); i++ {
			// Reset the prog until we have a full suffix
			if suffixProg.traverseTree(tree.Sub[i]) {
				suffixProg = &byPassProgAnchored{}
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

// traverseTree visits each node of the parsed regexp to detect fixed-length patterns
func (prog *byPassProgAnchored) traverseTree(tree *syntax.Regexp) (bailout bool) {

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
			literal:  "\n",
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
				literal:  string(tree.Rune[1] + 1),
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
		case syntax.OpCapture:

		case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpAlternate, syntax.OpEmptyMatch,
			 syntax.OpWordBoundary, syntax.OpNoWordBoundary,
			 syntax.OpStar, syntax.OpPlus, syntax.OpQuest:
			 return true
	*/

	default:
		// Unsupported Op
		return true
	}

	if step != nil {

		// No more steps with length > 0 can be added after a $
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

func (prog *byPassProgUnmatchable) MatchString(s string) (matched bool) {
	return false
}

// computeWidth computes the byte length of a byPassProgAnchored from its steps
func (prog *byPassProgAnchored) computeWidth() {

	previousLength := 0
	prog.length = 0
	for _, step := range prog.steps {
		prog.length += step.length
		prog.minWidth += step.minWidth
		step.previousLength = previousLength
		if step.maxWidth != -1 && prog.maxWidth != -1 {
			prog.maxWidth += step.maxWidth
		} else {
			prog.maxWidth = -1
		}
		previousLength += step.length
	}
	minNextWidth := prog.minWidth
	for _, step := range prog.steps {
		step.minNextWidth = minNextWidth
		minNextWidth -= step.minWidth
	}

}

func (prog *byPassProgAnchored) MatchString(s string) (matched bool) {

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

		// We don't have 0-length steps
		if begin >= end {
			return false
		}

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

		// Test the contents of the slice
		if !matchStepAnchored(step, s[begin:end]) {
			return false
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

// findCharClass finds the first character in a string that belongs to a byPassOpCharClass
func findCharClass(s string, step *byPassStep) (foundIndex int, matchingChar rune) {
	for idx, char := range s {
		for i := 0; i < len(step.classes); i += 2 {
			if step.classes[i] <= char && char <= step.classes[i+1] {
				return idx, char
			}
		}
	}
	foundIndex = -1
	return
}

// matchCharInClasses checks if a character belongs to a byPassOpCharClass
func matchCharInClasses(char rune, step *byPassStep) (matches bool) {
	for i := 0; i < len(step.classes); i += 2 {
		if step.classes[i] <= char && char <= step.classes[i+1] {
			return true
		}
	}
	return false
}

// findOtherChar finds the first character in a string that's different than a specific character
func findOtherChar(s string, char rune) (foundIndex int, matchingChar rune) {
	for idx, nextChar := range s {
		if char != nextChar {
			return idx, nextChar
		}
	}
	foundIndex = -1
	return
}

// matchStepAnchored matches a string slice as a whole against a byPassStep
func matchStepAnchored(step *byPassStep, s string) (matched bool) {

	switch step.op {
	case byPassOpLiteral:

		if s != step.literal {
			return false
		}

	case byPassOpCharClass:

		idx, _ := findCharClass(s, step)
		if idx == -1 {
			return false
		}

	case byPassOpNegativeCharClass:

		if s == step.literal {
			return false
		}

	case byPassOpAnyChar:
		// nothing to do

	}

	return true
}

func (prog *byPassProgUnanchored) MatchString(s string) (matched bool) {

	if len(s) < prog.minWidth {
		return false
	}

	var nextRune rune
	var nextWidth int

	// Position in bytes in the string where we start testing the pattern
	var cursor int

byPassUnanchoredRestart:

	// Width in bytes of the first rune at cursor
	firstRuneWidth := 1

	// Position in bytes in the string where we are testing the current step
	begin := cursor

	for stepn, step := range prog.steps {

		// Do we already know we wont have enough bytes to match the rest of the pattern?
		if begin+step.minNextWidth > len(s) {
			return false
		}

		if step.op != byPassOpLiteral {
			nextRune, nextWidth = utf8.DecodeRuneInString(s[begin:])

			if stepn == 0 {
				firstRuneWidth = nextWidth
			}
		}

		switch step.op {
		case byPassOpLiteral:

			idx := strings.Index(s[begin:], step.literal)

			switch idx {
			case -1:
				// Not found at all
				return false
			case 0:
				// Found in the right place
				if stepn == 0 {
					firstRuneWidth = step.minWidth
				}
				begin += step.minWidth
			default:
				// Found later, we have to backtrack.
				if stepn == 0 {
					cursor = begin + idx
					begin += idx + step.minWidth
					firstRuneWidth = step.minWidth
				} else {

					if idx == 1 {
						// In this special case we know the next rune had a width of 1 byte
						cursor += 1
					} else {
						// TODO: the call to lastRunesWidth could be avoided in some cases
						cursor = begin + idx - lastRunesWidth(s[cursor:begin+idx], step.previousLength)
					}
					goto byPassUnanchoredRestart
				}
			}

		case byPassOpNegativeCharClass:

			if nextRune != step.char {
				begin += nextWidth
			} else if stepn == 0 {

				idx, char := findOtherChar(s[begin+nextWidth:], step.char)
				if idx == -1 {
					return false
				}
				firstRuneWidth = utf8.RuneLen(char)
				cursor = begin + nextWidth + idx
				begin += nextWidth + idx + firstRuneWidth

			} else {
				cursor += firstRuneWidth
				goto byPassUnanchoredRestart
			}

		case byPassOpCharClass:

			if matchCharInClasses(nextRune, step) {
				begin += nextWidth
			} else if stepn == 0 {
				idx, matchingChar := findCharClass(s[begin+nextWidth:], step)

				if idx == -1 {
					return false
				}
				firstRuneWidth = utf8.RuneLen(matchingChar)
				cursor = begin + nextWidth + idx
				begin += nextWidth + idx + firstRuneWidth

			} else {
				cursor += firstRuneWidth
				goto byPassUnanchoredRestart
			}

		case byPassOpAnyChar:

			begin += nextWidth
		}
	}

	return true

}
