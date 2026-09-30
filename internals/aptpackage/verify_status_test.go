package aptpackage

import "testing"

func TestVerifiedOpenPGPSignerRequiresOneCurrentUnrevokedSignature(t *testing.T) {
	const primary = "0123456789ABCDEF0123456789ABCDEF01234567"
	valid := []byte("[GNUPG:] GOODSIG 0123456789ABCDEF signer\n[GNUPG:] VALIDSIG 89ABCDEF0123456789ABCDEF0123456789ABCDEF 2026-01-01 1 0 4 0 22 10 00 " + primary + "\n")
	got, err := verifiedOpenPGPSigner(valid)
	if err != nil || got != primary {
		t.Fatalf("valid signer=%q err=%v", got, err)
	}
	for name, status := range map[string][]byte{
		"expired key":          []byte("[GNUPG:] EXPKEYSIG 0123 signer\n" + string(valid)),
		"expired signature":    []byte("[GNUPG:] EXPSIG 0123 signer\n" + string(valid)),
		"revoked key":          []byte("[GNUPG:] REVKEYSIG 0123 signer\n" + string(valid)),
		"bad signature":        []byte("[GNUPG:] BADSIG 0123 signer\n" + string(valid)),
		"ambiguous signatures": []byte(string(valid) + string(valid)),
		"no signature":         []byte("[GNUPG:] GOODSIG 0123 signer\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifiedOpenPGPSigner(status); err == nil {
				t.Fatal("unacceptable OpenPGP status accepted")
			}
		})
	}
}
