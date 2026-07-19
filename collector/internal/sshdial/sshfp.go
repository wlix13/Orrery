package sshdial

import (
	"crypto/sha1" // #nosec G505 -- SSHFP type 1 fingerprints are SHA-1 by spec (RFC 4255)
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/crypto/ssh"
)

const dnsTimeout = 5 * time.Second

// SSHFP algorithm numbers (RFC 4255 / 6594 / 7479).
const (
	sshfpAlgRSA     = 1
	sshfpAlgDSA     = 2
	sshfpAlgECDSA   = 3
	sshfpAlgEd25519 = 4
	sshfpAlgEd448   = 6
)

// SSHFP fingerprint types.
const (
	sshfpTypeSHA1   = 1
	sshfpTypeSHA256 = 2
)

// sshfpCallback verifies a node's host key against its DNS SSHFP records.
// It ignores hostname case (DNS is case-insensitive) and, unlike known_hosts,
// needs no per-node maintenance - new nodes just publish SSHFP records.
//
// When requireDNSSEC is set, records are only trusted if the resolver returned
// an authenticated (AD) answer; without DNSSEC, SSHFP is spoofable and no safer
// than trust-on-first-use.
func sshfpCallback(requireDNSSEC bool) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		host := hostname
		if h, _, err := net.SplitHostPort(hostname); err == nil {
			host = h
		}

		host = strings.TrimSuffix(strings.ToLower(host), ".")

		records, authenticated, err := lookupSSHFP(host)
		if err != nil {
			return fmt.Errorf("sshfp lookup for %s: %w", host, err)
		}

		if len(records) == 0 {
			return fmt.Errorf("no SSHFP records published for %s", host)
		}

		if requireDNSSEC && !authenticated {
			return fmt.Errorf("sshfp for %s is not DNSSEC-authenticated (set sshfp_require_dnssec: false to allow)", host)
		}

		if sshfpAlgorithm(key.Type()) == 0 {
			return fmt.Errorf("unsupported host key type %q for SSHFP", key.Type())
		}

		if keyMatchesSSHFP(records, key) {
			return nil
		}

		return fmt.Errorf("host key for %s (%s) matches no SSHFP record", host, ssh.FingerprintSHA256(key))
	}
}

// hostKeyAlgorithms returns the ssh host-key algorithms the published records
// cover, strongest first. A node offering several key types would otherwise let
// negotiation settle on one with no SSHFP record.
func hostKeyAlgorithms(records []*dns.SSHFP) []string {
	published := map[uint8]bool{}
	for _, r := range records {
		published[r.Algorithm] = true
	}

	var algos []string

	if published[sshfpAlgEd25519] {
		algos = append(algos, ssh.KeyAlgoED25519)
	}

	if published[sshfpAlgECDSA] {
		algos = append(algos, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521)
	}

	if published[sshfpAlgRSA] {
		algos = append(algos, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA)
	}

	return algos
}

// keyMatchesSSHFP reports whether the host key matches any SSHFP record
// (same algorithm and fingerprint).
func keyMatchesSSHFP(records []*dns.SSHFP, key ssh.PublicKey) bool {
	alg := sshfpAlgorithm(key.Type())
	if alg == 0 {
		return false
	}

	blob := key.Marshal()

	for _, r := range records {
		if int(r.Algorithm) != alg {
			continue
		}

		want := sshfpFingerprint(int(r.Type), blob)
		if want != "" && strings.EqualFold(want, r.FingerPrint) {
			return true
		}
	}

	return false
}

// lookupSSHFP queries SSHFP records and reports whether the answer was
// DNSSEC-authenticated (the resolver's AD bit).
func lookupSSHFP(host string) ([]*dns.SSHFP, bool, error) {
	server, err := resolverAddr()
	if err != nil {
		return nil, false, err
	}

	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(host), dns.TypeSSHFP)
	msg.SetEdns0(4096, true) // DO bit: ask the resolver to validate DNSSEC
	msg.RecursionDesired = true

	client := &dns.Client{Timeout: dnsTimeout}

	resp, _, err := client.Exchange(msg, server)
	if err != nil {
		return nil, false, err
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, false, fmt.Errorf("dns rcode %s", dns.RcodeToString[resp.Rcode])
	}

	var records []*dns.SSHFP

	for _, rr := range resp.Answer {
		if s, ok := rr.(*dns.SSHFP); ok {
			records = append(records, s)
		}
	}

	return records, resp.AuthenticatedData, nil
}

func resolverAddr() (string, error) {
	conf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil || len(conf.Servers) == 0 {
		return "127.0.0.1:53", nil // fall back to a local resolver
	}

	return net.JoinHostPort(conf.Servers[0], conf.Port), nil
}

func sshfpAlgorithm(keyType string) int {
	switch {
	case keyType == ssh.KeyAlgoRSA:
		return sshfpAlgRSA
	case strings.HasPrefix(keyType, "ecdsa-sha2-"):
		return sshfpAlgECDSA
	case keyType == ssh.KeyAlgoED25519:
		return sshfpAlgEd25519
	case keyType == "ssh-ed448":
		return sshfpAlgEd448
	default:
		return 0
	}
}

// sshfpFingerprint hashes the public-key blob as SSHFP requires (RFC 4255):
// the digest is over the raw key, hex-encoded.
func sshfpFingerprint(fpType int, blob []byte) string {
	switch fpType {
	case sshfpTypeSHA1:
		sum := sha1.Sum(blob) // #nosec G401 -- SSHFP type 1 is SHA-1 by spec
		return hex.EncodeToString(sum[:])
	case sshfpTypeSHA256:
		sum := sha256.Sum256(blob)
		return hex.EncodeToString(sum[:])
	default:
		return ""
	}
}
