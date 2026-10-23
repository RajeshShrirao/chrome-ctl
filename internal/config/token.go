package config

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const tokenHexLen = 64

func EnsureToken(path string) (token string, created bool, err error) {
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		token = strings.TrimSpace(string(data))
		if !validToken(token) {
			return "", false, fmt.Errorf("invalid auth token in %s", path)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return "", false, fmt.Errorf("secure auth token: %w", err)
		}
		return token, false, nil
	case !errors.Is(readErr, fs.ErrNotExist):
		return "", false, fmt.Errorf("read auth token: %w", readErr)
	}

	buf := make([]byte, tokenHexLen/2)
	if _, err := rand.Read(buf); err != nil {
		return "", false, fmt.Errorf("generate auth token: %w", err)
	}
	token = hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", false, fmt.Errorf("write auth token: %w", err)
	}
	return token, true, nil
}

func ReadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if !validToken(token) {
		return "", fmt.Errorf("invalid auth token in %s", path)
	}
	return token, nil
}

func TokenMatches(path, presented string) bool {
	expected, err := ReadToken(path)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) == 1
}

func validToken(token string) bool {
	if len(token) != tokenHexLen {
		return false
	}
	for _, r := range token {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
