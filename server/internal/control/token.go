package control

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var slugUnsafePattern = regexp.MustCompile(`[^a-z0-9]+`)

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func newState() (string, error) {
	value, err := randomHex(24)
	if err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	return value, nil
}

func newSessionToken() (string, error) {
	value, err := randomHex(32)
	if err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return "msp_" + value, nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:12]
}

func defaultWorkspaceSlug(login, userID string) string {
	base := strings.ToLower(strings.TrimSpace(login))
	if base == "" {
		base = "workspace"
	}
	base = strings.Trim(slugUnsafePattern.ReplaceAllString(base, "-"), "-")
	if base == "" {
		base = "workspace"
	}
	suffix := strings.ReplaceAll(userID, "-", "")
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix == "" {
		return base
	}
	return base + "-" + suffix
}
