package regexpbypass

import (
	"fmt"
	regexpb "github.com/sylvinus/regexpbypass/regexpbypass"
	"regexp"
)

var acceptable = regexp.MustCompile(`^([abcd☺\|\$\.\^\[\]]+) ([abcdef☺\n]+)$`)

// TODO better starting corpus based on RE2 tests
func Fuzz(data []byte) int {
	sdata := string(data)
	m := acceptable.FindStringSubmatch(sdata)
	if len(m) == 0 {
		return -1
	}
	fmt.Println(m)

	re, err := regexp.Compile(m[1])

	if err != nil {
		return 0
	}

	rebypass, errbypass := regexpb.Compile(m[1])

	if err != errbypass {
		panic("regexpbypass doesn't compile")
	}

	reMatches := re.MatchString(m[2])
	reBypassMatches := rebypass.MatchString(m[2])

	if reMatches != reBypassMatches {
		panic("engine results differ!")
	}
	return 1

}
