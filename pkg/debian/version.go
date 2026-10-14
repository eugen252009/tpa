package debian

import (
	"fmt"
	"strings"
)

// CompareVersions implements dpkg's epoch/upstream/revision ordering, not SemVer.
// It returns -1, 0 or 1. Epochs and digit runs are compared without integer overflow.
func CompareVersions(a, b string) (int, error) {
	if !ValidVersion(a) || !ValidVersion(b) {
		return 0, fmt.Errorf("invalid Debian version; see %s#version", controlPolicyURL)
	}
	ae, au, ar := splitVersion(a)
	be, bu, br := splitVersion(b)
	if n := compareDigits(ae, be); n != 0 {
		return n, nil
	}
	if n := comparePart(au, bu); n != 0 {
		return n, nil
	}
	return comparePart(ar, br), nil
}
func splitVersion(s string) (epoch, upstream, revision string) {
	epoch = "0"
	revision = "0"
	if i := strings.IndexByte(s, ':'); i >= 0 {
		epoch, s = s[:i], s[i+1:]
	}
	if i := strings.LastIndexByte(s, '-'); i >= 0 {
		s, revision = s[:i], s[i+1:]
	}
	return epoch, s, revision
}
func compareDigits(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
func digit(c byte) bool { return c >= '0' && c <= '9' }
func order(s string, i int) int {
	if i == len(s) {
		return 0
	}
	c := s[i]
	if c == '~' {
		return -1
	}
	if digit(c) {
		return 0
	}
	if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
		return int(c)
	}
	return int(c) + 256
}
func comparePart(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		for i < len(a) && !digit(a[i]) || j < len(b) && !digit(b[j]) {
			x, y := order(a, i), order(b, j)
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
			if i < len(a) {
				i++
			}
			if j < len(b) {
				j++
			}
		}
		ai, bj := i, j
		for i < len(a) && digit(a[i]) {
			i++
		}
		for j < len(b) && digit(b[j]) {
			j++
		}
		if n := compareDigits(a[ai:i], b[bj:j]); n != 0 {
			return n
		}
	}
	return 0
}
