package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomerAccountDelegatesWithoutShellOrDestinationReinterpretation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tparun"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	for _, test := range []struct {
		args      []string
		operation string
	}{
		{[]string{"register", "--address", "host:2222", "--known-hosts", "pinned"}, "register"},
		{[]string{"register", "--confirm", "--confirmation-file", "private-code"}, "confirm"},
		{[]string{"login", "--server", "https://account.invalid", "--identity", "file;not-a-shell-command"}, "login"},
	} {
		var out, errors bytes.Buffer
		if code := runWithTerminal(test.args, strings.NewReader(""), &out, &errors, false); code != 0 {
			t.Fatal(code, errors.String())
		}
		if !strings.HasPrefix(out.String(), "customer-ssh\n"+test.operation+"\n") {
			t.Fatal(out.String())
		}
		if strings.Contains(out.String(), "--confirm\n") {
			t.Fatal("wrapper option leaked to canonical client")
		}
	}
}
func TestCustomerAccountFailsClosedWithoutClient(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out, errors bytes.Buffer
	if code := runWithTerminal([]string{"register"}, strings.NewReader(""), &out, &errors, false); code == 0 {
		t.Fatal("missing client accepted")
	}
	if out.Len() != 0 {
		t.Fatal("missing client implied registration", out.String())
	}
}
