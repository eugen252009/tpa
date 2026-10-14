package main

import (
	"bytes"
	"testing"
)

func TestUtilityCLI(t *testing.T) {
	for _, test := range []struct {
		args     []string
		ok       bool
		contains string
	}{
		{[]string{"check", "package", "myPackage"}, false, "uppercase"},
		{[]string{"check", "dependency", "libc6 (>= 2.36)"}, true, "Valid"},
		{[]string{"version", "compare", "1.0~beta", "1.0"}, true, "-1"},
		{[]string{"--help"}, true, "repo html"},
	} {
		var out, errOut bytes.Buffer
		code := run(test.args, &out, &errOut)
		if (code == 0) != test.ok || !bytes.Contains(append(out.Bytes(), errOut.Bytes()...), []byte(test.contains)) {
			t.Fatalf("%v code=%d out=%s err=%s", test.args, code, &out, &errOut)
		}
	}
}
