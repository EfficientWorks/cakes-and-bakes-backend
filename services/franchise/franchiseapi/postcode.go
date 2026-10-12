package franchiseapi

import (
	"regexp"
	"strings"
)

/*
 * Postcodes, as a store's delivery list and a customer's address give them.
 *
 * A US ZIP is five digits, and a store lists the ZIPs it serves. A UK
 * postcode ("SW1A 1AA") has an outward code (SW1A — the district) and an
 * inward code (1AA), and a UK shop lists the areas it serves at whichever
 * level suits it:
 *
 *   SW1A 1AA   one postcode
 *   SW1A 1     a sector
 *   SW1A       a district
 *   SW         an area (letters only)
 *
 * A customer's postcode is covered by an entry at any of those levels.
 * Postcodes are compared in one canonical form — upper case, one space before
 * the inward code — however they were typed ("sw1a1aa", " SW1A  1AA ").
 */

var (
	// ukPostcode is a full UK postcode, anywhere in the text (an address
	// line can carry one), spacing optional.
	ukPostcode = regexp.MustCompile(`\b([A-Z]{1,2}[0-9][A-Z0-9]?) ?([0-9][A-Z]{2})\b`)
	// ukPartial is a UK outward code, optionally with the sector digit.
	ukPartial = regexp.MustCompile(`^([A-Z]{1,2}[0-9][A-Z0-9]?)(?: ?([0-9]))?$`)
	ukArea    = regexp.MustCompile(`^[A-Z]{1,2}$`)
	usZip     = regexp.MustCompile(`\b([0-9]{5})(?:-[0-9]{4})?\b`)
)

// NormalizePostcode returns a postcode in its canonical form: a UK postcode
// (or district, sector or area) upper-cased and spaced "SW1A 1AA"; a US ZIP
// as its five digits ("08837-1234" → "08837"). Either is found inside a
// longer address line. Anything else comes back trimmed and upper-cased.
func NormalizePostcode(s string) string {
	up := strings.ToUpper(strings.Join(strings.Fields(s), " "))
	if up == "" {
		return ""
	}
	if m := ukPostcode.FindStringSubmatch(up); m != nil {
		return m[1] + " " + m[2]
	}
	if m := ukPartial.FindStringSubmatch(up); m != nil {
		if m[2] != "" {
			return m[1] + " " + m[2]
		}
		return m[1]
	}
	if ukArea.MatchString(up) {
		return up
	}
	// The last ZIP in the text: an address line ends with it ("…, Georgia,
	// 31408, USA"), and a house number can look like one.
	if all := usZip.FindAllStringSubmatch(up, -1); len(all) > 0 {
		return all[len(all)-1][1]
	}
	return up
}

// PostcodeCovers reports whether a store's delivery-list entry covers a
// customer's postcode: the same postcode, or — for a full UK postcode — its
// sector, district or area.
func PostcodeCovers(entry, postcode string) bool {
	e, p := NormalizePostcode(entry), NormalizePostcode(postcode)
	if e == "" || p == "" {
		return false
	}
	if e == p {
		return true
	}
	m := ukPostcode.FindStringSubmatch(p)
	if m == nil || m[0] != p {
		return false
	}
	outward, inward := m[1], m[2]
	if e == outward || e == outward+" "+inward[:1] {
		return true
	}
	// An area is the outward code's leading letters: "SW" covers SW1A but "S"
	// does not.
	return ukArea.MatchString(e) && leadingLetters(outward) == e
}

func leadingLetters(s string) string {
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		i++
	}
	return s[:i]
}

func postcodeInList(entries []string, postcode string) bool {
	for _, e := range entries {
		if PostcodeCovers(e, postcode) {
			return true
		}
	}
	return false
}

// feeForPostcode is the fee entry covering a postcode. The most specific one
// wins: a fee set for SW1A 1 beats one for SW1A, which beats one for SW.
func feeForPostcode(fees []ZipFee, postcode string) (ZipFee, bool) {
	best, found := ZipFee{}, false
	for _, f := range fees {
		if PostcodeCovers(f.Zip, postcode) && (!found || len(NormalizePostcode(f.Zip)) > len(NormalizePostcode(best.Zip))) {
			best, found = f, true
		}
	}
	return best, found
}
