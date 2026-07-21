package sshdial

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/miekg/dns"
	"golang.org/x/crypto/ssh"
)

func testHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	return key
}

func TestSSHFPAlgorithm(t *testing.T) {
	cases := map[string]int{
		ssh.KeyAlgoRSA:        sshfpAlgRSA,
		ssh.KeyAlgoED25519:    sshfpAlgEd25519,
		"ecdsa-sha2-nistp256": sshfpAlgECDSA,
		"ecdsa-sha2-nistp521": sshfpAlgECDSA,
		"ssh-ed448":           sshfpAlgEd448,
		"something-else":      0,
	}
	for keyType, want := range cases {
		if got := sshfpAlgorithm(keyType); got != want {
			t.Errorf("sshfpAlgorithm(%q) = %d, want %d", keyType, got, want)
		}
	}
}

func TestSSHFPFingerprintMatchesSpec(t *testing.T) {
	key := testHostKey(t)
	blob := key.Marshal()
	sum := sha256.Sum256(blob)
	want := hex.EncodeToString(sum[:])

	got := sshfpFingerprint(sshfpTypeSHA256, blob)
	if got != want {
		t.Errorf("sha256 fingerprint = %q, want %q", got, want)
	}

	if len(sshfpFingerprint(sshfpTypeSHA1, blob)) != 40 {
		t.Error("sha1 fingerprint should be 40 hex chars")
	}

	if sshfpFingerprint(99, blob) != "" {
		t.Error("unknown fp type should return empty")
	}
}

func TestKeyMatchesSSHFP(t *testing.T) {
	key := testHostKey(t)
	fp := sshfpFingerprint(sshfpTypeSHA256, key.Marshal())

	good := &dns.SSHFP{Algorithm: sshfpAlgEd25519, Type: sshfpTypeSHA256, FingerPrint: fp}
	// SSHFP records store hex uppercase or lowercase; matching is case-insensitive.
	upper := &dns.SSHFP{Algorithm: sshfpAlgEd25519, Type: sshfpTypeSHA256, FingerPrint: hexUpper(fp)}
	wrongFP := &dns.SSHFP{Algorithm: sshfpAlgEd25519, Type: sshfpTypeSHA256, FingerPrint: "00" + fp[2:]}
	wrongAlg := &dns.SSHFP{Algorithm: sshfpAlgRSA, Type: sshfpTypeSHA256, FingerPrint: fp}

	tests := []struct {
		name    string
		records []*dns.SSHFP
		want    bool
	}{
		{"exact", []*dns.SSHFP{good}, true},
		{"case-insensitive", []*dns.SSHFP{upper}, true},
		{"among others", []*dns.SSHFP{wrongAlg, wrongFP, good}, true},
		{"wrong fingerprint", []*dns.SSHFP{wrongFP}, false},
		{"wrong algorithm", []*dns.SSHFP{wrongAlg}, false},
		{"empty", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := keyMatchesSSHFP(tc.records, key); got != tc.want {
				t.Errorf("keyMatchesSSHFP = %v, want %v", got, tc.want)
			}
		})
	}
}

// A node offering several host key types must not be able to steer the
// handshake to one it published no record for.
func TestHostKeyAlgorithms(t *testing.T) {
	rec := func(alg uint8) *dns.SSHFP { return &dns.SSHFP{Algorithm: alg, Type: sshfpTypeSHA256} }

	tests := []struct {
		name    string
		records []*dns.SSHFP
		want    []string
	}{
		{"ed25519 only", []*dns.SSHFP{rec(sshfpAlgEd25519)}, []string{ssh.KeyAlgoED25519}},
		{"rsa only", []*dns.SSHFP{rec(sshfpAlgRSA)}, []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA}},
		{
			"ed25519 leads a mixed set",
			[]*dns.SSHFP{rec(sshfpAlgRSA), rec(sshfpAlgEd25519)},
			[]string{ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA},
		},
		{"duplicates collapse", []*dns.SSHFP{rec(sshfpAlgEd25519), rec(sshfpAlgEd25519)}, []string{ssh.KeyAlgoED25519}},
		{"unsupported algorithm yields nothing", []*dns.SSHFP{rec(sshfpAlgEd448)}, nil},
		{"no records leaves negotiation unconstrained", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hostKeyAlgorithms(tc.records)
			if !slices.Equal(got, tc.want) {
				t.Errorf("hostKeyAlgorithms = %v, want %v", got, tc.want)
			}
		})
	}

	// The regression this exists for: ECDSA is offered but only Ed25519 is published.
	algos := hostKeyAlgorithms([]*dns.SSHFP{rec(sshfpAlgEd25519)})
	if slices.Contains(algos, ssh.KeyAlgoECDSA256) {
		t.Error("ECDSA must not be offered when only Ed25519 is published")
	}
}

func hexUpper(s string) string {
	out := []byte(s)
	for i, b := range out {
		if b >= 'a' && b <= 'f' {
			out[i] = b - 32
		}
	}

	return string(out)
}
