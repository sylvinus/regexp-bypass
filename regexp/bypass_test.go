// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package regexp

import (
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
	{`a.`, false},
	{`^a.`, true},
	{`a{2}`, true},
	{`(a)`, false},
	{`x.[^z]yz$`, true},
	{`^(?:(?:a(?:a.)))$`, true},
	{`(?:a(?:a.))`, false},
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