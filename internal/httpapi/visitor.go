package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"strings"
)

const (
	// EnvAuditHashSecret salts the anonymous visitor hash.
	EnvAuditHashSecret = "AUDIT_HASH_SECRET"
	// EnvTrustForwarded enables the first X-Forwarded-For hop.
	EnvTrustForwarded = "TRUST_FORWARDED_FOR"

	defaultAuditSecret = "fleet-pulse-audit-hash"
)

func auditSecretFromEnv() string {
	if s := strings.TrimSpace(os.Getenv(EnvAuditHashSecret)); s != "" {
		return s
	}
	return defaultAuditSecret
}

func trustForwardedFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTrustForwarded))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func hashVisitor(secret, remoteAddr, forwardedFor string, trustForwarded bool) string {
	ip := stripHostPort(remoteAddr)
	if trustForwarded {
		if hop := firstForwardedHop(forwardedFor); hop != "" {
			ip = hop
		}
	}
	sum := sha256.Sum256([]byte(secret + "\n" + ip))
	return hex.EncodeToString(sum[:])
}

func stripHostPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}

func firstForwardedHop(header string) string {
	if header == "" {
		return ""
	}
	first, _, _ := strings.Cut(header, ",")
	return strings.TrimSpace(first)
}
