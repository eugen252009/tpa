package aptpackage

import (
	"bytes"
	"strings"
	"testing"
)

type hostedTestInput struct{ bytes.Buffer }

func (*hostedTestInput) Close() error { return nil }

func TestHostedCASHandshakeAndNegotiationFailure(t *testing.T) {
	for _, tc := range []struct{ name, ready, want string }{
		{"version-mismatch", `{"version":3,"protocol":"tparun-chunk-ssh","state":"ready","algorithm":"fixed-sha256-1m-v1"}`, "no fallback"},
		{"authorized-stale-parent", `{"version":2,"protocol":"tparun-chunk-ssh","state":"ready","algorithm":"fixed-sha256-1m-v1"}`, "CAS negotiation: CAS rejected: CAS parent changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			output.WriteString(tc.ready + "\n")
			if err := hostedCASWrite(&output, map[string]string{"error": "CAS parent changed"}); err != nil {
				t.Fatal(err)
			}
			input := &hostedTestInput{}
			_, err := publishHostedCAS(t.TempDir(), GenerationManifest{}, input, &output)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
			if tc.name == "version-mismatch" && input.Len() != 0 {
				t.Fatal("sent inventory after incompatible handshake")
			}
			if tc.name == "authorized-stale-parent" {
				var inv hostedCASInventory
				if err := hostedCASRead(input, &inv); err != nil {
					t.Fatal(err)
				}
				if input.Len() != 0 {
					t.Fatal("transferred payload after rejected negotiation")
				}
			}
		})
	}
}

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
