package main

import (
	"os"
	"strings"
	"testing"
)

// LICENSE-DISTRIBUTION: TPA retains MIT and the owner's declared attribution;
// linked modules keep their notices, independent of the TPA.run service license.
func TestTPALicenseAndDistribution(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	license := read("LICENSE")
	for _, clause := range []string{
		"MIT License", "Copyright (c) 2026 the tpa.run Team",
		"Permission is hereby granted, free of charge",
		"The above copyright notice and this permission notice",
		"THE SOFTWARE IS PROVIDED \"AS IS\"",
	} {
		if !strings.Contains(license, clause) {
			t.Errorf("missing MIT clause %q", clause)
		}
	}
	readme := read("README.md")
	if !strings.Contains(readme, "[MIT License](LICENSE), by the tpa.run Team.") ||
		strings.Contains(strings.ToLower(readme), "coffee maker") {
		t.Fatal("README attribution/license inconsistent")
	}
	notices := read("THIRD_PARTY_NOTICES")
	for _, notice := range []string{
		"Go Authors", "github.com/ulikunitz/xz v0.5.17", "Ulrich Kunitz",
		"golang.org/x/sys v0.47.0", "gopkg.in/yaml.v3 v3.0.1",
		"Kirill Simonov", "Canonical Ltd", "Apache License", "Version 2.0",
		"END OF TERMS AND CONDITIONS",
	} {
		if !strings.Contains(notices, notice) {
			t.Errorf("missing dependency notice %q", notice)
		}
	}
	if !strings.Contains(read("scripts/stage-tpa-release.sh"), "cat LICENSE THIRD_PARTY_NOTICES") {
		t.Fatal("mandatory copyright must retain dependency notices under Debian doc filters")
	}
	for _, path := range []string{"copyright", "THIRD_PARTY_NOTICES"} {
		if !strings.Contains(read(".tpa-release.yml"), "usr/share/doc/tpa/"+path) {
			t.Errorf("reusable build must require packaged %s", path)
		}
	}
}
