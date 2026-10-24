package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
)

func TestTokenCommandPrintsExistingToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHROME_CTL_HOME", home)

	token, _, err := config.EnsureToken(filepath.Join(home, "auth_token"))
	if err != nil {
		t.Fatalf("ensure token: %v", err)
	}

	out, err := executePhase2(t, "token")
	if err != nil {
		t.Fatalf("token command: %v", err)
	}
	if got := strings.TrimSpace(out); got != token {
		t.Fatalf("stdout = %q, want %q", got, token)
	}
}

func TestTokenCommandFailsWithoutToken(t *testing.T) {
	t.Setenv("CHROME_CTL_HOME", t.TempDir())

	if _, err := executePhase2(t, "token"); err == nil {
		t.Fatal("token command succeeded without a token file")
	}
}
