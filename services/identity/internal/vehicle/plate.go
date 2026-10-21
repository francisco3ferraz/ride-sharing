package vehicle

import (
	"fmt"
	"regexp"
	"strings"
)

// The layouts Portuguese plates have had, without hyphens: AA-00-00 until 1992,
// 00-00-AA until 2005, 00-AA-00 until 2020, and AA-00-AA since. Any letter is allowed:
// a stricter rule than the real one would turn away real plates.
var plateLayouts = regexp.MustCompile(`^(?:[A-Z]{2}\d{4}|\d{4}[A-Z]{2}|\d{2}[A-Z]{2}\d{2}|[A-Z]{2}\d{2}[A-Z]{2})$`)

// CleanPlate returns the plate as it's stored, such as AA-00-AA. Every spelling of a
// plate must come out the same, or its uniqueness can be dodged.
func CleanPlate(plate string) (string, error) {
	plate = strings.Map(func(r rune) rune {
		switch {
		case r == ' ' || r == '-':
			return -1
		// Not strings.ToUpper, which turns a few non-ASCII letters into ASCII ones, such
		// as ı into I.
		case 'a' <= r && r <= 'z':
			return r - 'a' + 'A'
		default:
			return r
		}
	}, plate)
	if !plateLayouts.MatchString(plate) {
		return "", fmt.Errorf("%w: plate must look like AA-00-AA, 00-AA-00, 00-00-AA or AA-00-00", ErrInvalid)
	}
	return plate[0:2] + "-" + plate[2:4] + "-" + plate[4:6], nil
}
