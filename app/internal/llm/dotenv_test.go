package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSingleEnvFile_LastValueWinsWithinFileAndRespectsProcessEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := `
# First definition
TEST_DOTENV_DUP=first-value
TEST_DOTENV_PRESET=from-file
# Override later in the same file
TEST_DOTENV_DUP=second-value
`
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp .env: %v", err)
	}

	t.Setenv("TEST_DOTENV_DUP", "")
	t.Setenv("TEST_DOTENV_PRESET", "from-process")

	loadSingleEnvFile(envPath)

	if got := os.Getenv("TEST_DOTENV_DUP"); got != "second-value" {
		t.Errorf("expected last value 'second-value' from .env, got %q", got)
	}
	if got := os.Getenv("TEST_DOTENV_PRESET"); got != "from-process" {
		t.Errorf("expected pre-existing process env 'from-process' to be preserved, got %q", got)
	}
}
