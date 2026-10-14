package debian

import (
	"os/exec"
	"strings"
	"testing"
)

func TestFieldValidation(t *testing.T) {
	for _, test := range []struct {
		field, value string
		valid        bool
	}{
		{"package", "myPackage", false}, {"package", "my-package", true}, {"package", "a", false},
		{"version", "1.0~beta", true}, {"version", "nope", false}, {"dependency", "libc6 (>= 2.36)", true},
		{"dependency", "libc6 (=> 2.36)", false}, {"architecture", "x86_64", false},
	} {
		err := ValidateField(test.field, test.value)
		if (err == nil) != test.valid {
			t.Errorf("%+v: %v", test, err)
		}
	}
	if err := ValidateControl(Fields{}); len(ErrorDiagnostics(err)) < 5 {
		t.Fatal("missing aggregate diagnostics")
	}
}
func TestVersionOrderingAgainstDpkg(t *testing.T) {
	if _, err := exec.LookPath("dpkg"); err != nil {
		t.Skip("dpkg missing")
	}
	versions := []string{"0", "00", "0~", "1.0", "1.00", "1.0~beta", "1.0~~", "1.0~rc1", "1.0-0", "1.0-1", "1.0-1+b1", "1.0+git", "1.0a", "1.0A", "1.0.a", "1:0", "2:0", "001:1", "999999999:1", "2147483647:1", "1.0-0~", "1.0-01", "1.0-1~beta", "1.0-beta-2"}
	for _, a := range versions {
		for _, b := range versions {
			got, err := CompareVersions(a, b)
			if err != nil {
				t.Fatal(err)
			}
			op := "eq"
			if got < 0 {
				op = "lt"
			} else if got > 0 {
				op = "gt"
			}
			if out, err := exec.Command("dpkg", "--compare-versions", a, op, b).CombinedOutput(); err != nil {
				t.Fatalf("%s %s %s disagrees: %s %v", a, op, b, out, err)
			}
		}
	}
	// Arbitrarily large numeric runs must not overflow.
	if n, err := CompareVersions("1."+strings.Repeat("9", 100), "1.2"); err != nil || n <= 0 {
		t.Fatal(n, err)
	}
	if _, err := CompareVersions("2147483648:1", "1"); err == nil {
		t.Fatal("accepted overflowing dpkg epoch")
	}
	if _, err := CompareVersions("bad", "1"); err == nil {
		t.Fatal("accepted invalid version")
	}
}
