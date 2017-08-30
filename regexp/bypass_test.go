// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package regexp

import (
	"encoding/csv"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"testing"
)

var compileByPassTests = []struct {
	pat      string
	isByPass bool
}{
	{`a`, true},
	{`[a]`, true},
	{`[^a]`, true},
	{`.`, true},
	{`.+`, false},
	{`a.`, true},
	{`^a.`, true},
	{`a{2}`, true},
	{`(a)`, false},
	{`x.[^z]yz$`, true},
	{`^(?:(?:a(?:a.)))$`, true},
	{`(?:a(?:a.))`, true},
	{`\A(?:(?:a(?:a.)))\z`, true},
	{`^aa.*`, true},
}

func TestByPassCompile(t *testing.T) {
	for _, test := range compileByPassTests {

		re := MustCompile(test.pat)

		if (re.bypass != nil) != test.isByPass {
			t.Errorf("pat: %s should have been bypassed=%t", test.pat, test.isByPass)
		}
	}

}

func TestByPassGithubRegexps(t *testing.T) {

	var total, linearUnanchored, linearAnchored, alt, firstpass, supported, unsupported, invalid, unmatchable int

	f, _ := os.Open("testdata/github-regexp.csv")
	defer f.Close()

	lines, _ := csv.NewReader(f).ReadAll()

	for _, line := range lines {
		n, _ := strconv.Atoi(line[1])
		total += n
		_, err := regexp.Compile(line[0])
		if err != nil {
			invalid += n
			continue
		}

		re, errb := Compile(line[0])
		if errb != nil {
			t.Errorf("Failed to compile in regexp-bypass but not in regexp: ", line[0])
			continue
		}

		if re.bypass == nil {
			unsupported += n
			continue
		}

		supported += n

		switch reflect.TypeOf(re.bypass).String() {
		case "*regexp.byPassProgAnchored":
			linearAnchored += n
		case "*regexp.byPassProgUnanchored":
			linearUnanchored += n
		case "*regexp.byPassProgAlternate":
			alt += n
		case "*regexp.byPassProgFirstPass":
			firstpass += n
		case "*regexp.byPassProgUnmatchable":
			unmatchable += n
		}

	}

	t.Logf("\nStats on %d unique regexps from GitHub for bypass matcher:", len(lines))
	t.Logf("Total occurences                                %d", total)
	t.Logf("Invalid                                         %d (%0.2f%%)", invalid, float64(invalid*100)/float64(total))
	t.Logf("Unsupported                                     %d (%0.2f%%)", unsupported, float64(unsupported*100)/float64(total))
	t.Logf("Supported total                                 %d (%0.2f%%)", supported, float64(supported*100)/float64(total))
	t.Logf(" Supported with byPassProgAnchored              %d (%0.2f%%)", linearAnchored, float64(linearAnchored*100)/float64(total))
	t.Logf(" Supported with byPassProgUnanchored            %d (%0.2f%%)", linearUnanchored, float64(linearUnanchored*100)/float64(total))
	t.Logf(" Supported with byPassProgAlternate             %d (%0.2f%%)", alt, float64(alt*100)/float64(total))
	t.Logf(" Supported with byPassProgFirstPass             %d (%0.2f%%)", firstpass, float64(firstpass*100)/float64(total))
	t.Logf(" Supported with byPassProgUnmatchable           %d (%0.2f%%)", unmatchable, float64(unmatchable*100)/float64(total))

}
