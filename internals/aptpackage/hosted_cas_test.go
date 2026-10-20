package aptpackage

import (
	"bytes"
	"strings"
	"testing"
)

func TestHostedCASNegotiationReportsServerRejection(t *testing.T) {
	var frame bytes.Buffer
	if err := hostedCASWrite(&frame, map[string]string{"error": "CAS parent changed"}); err != nil {
		t.Fatal(err)
	}
	var need struct {
		Missing []hostedChunk `json:"missing"`
	}
	err := hostedCASRead(&frame, &need)
	if err == nil || !strings.Contains(err.Error(), "CAS parent changed") || strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("lost server rejection: %v", err)
	}
}

func TestHostedCASResponseRemainsStrict(t *testing.T) {
	for _, v := range []any{
		map[string]any{"error": "CAS parent changed", "missing": []any{}},
		map[string]any{"error": 17},
		map[string]any{"error": ""},
		map[string]any{"unknown": true},
	} {
		var frame bytes.Buffer
		if err := hostedCASWrite(&frame, v); err != nil {
			t.Fatal(err)
		}
		var need struct {
			Missing []hostedChunk `json:"missing"`
		}
		if err := hostedCASRead(&frame, &need); err == nil {
			t.Fatalf("accepted malformed response %#v", v)
		}
	}
	var frame bytes.Buffer
	_ = hostedCASWrite(&frame, map[string]any{"missing": []any{}})
	var need struct {
		Missing []hostedChunk `json:"missing"`
	}
	if err := hostedCASRead(&frame, &need); err != nil {
		t.Fatal(err)
	}
	// An incomplete response fails, and a fresh stream remains independently usable.
	var partial bytes.Buffer
	_ = hostedCASWrite(&partial, map[string]string{"error": "credential revoked"})
	data := partial.Bytes()
	if err := hostedCASRead(bytes.NewReader(data[:len(data)-1]), &need); err == nil {
		t.Fatal("accepted interrupted frame")
	}
	err := hostedCASRead(bytes.NewReader(data), &need)
	if err == nil || !strings.Contains(err.Error(), "credential revoked") {
		t.Fatal(err)
	}
}
