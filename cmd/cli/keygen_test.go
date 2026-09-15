package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeygenCommandWritesKeyFiles(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "sub", "key")

	rootCmd.SetArgs([]string{"keygen", "--out", prefix})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute keygen: %v", err)
	}

	if _, err := os.Stat(prefix + ".priv"); err != nil {
		t.Fatalf("expected private key file: %v", err)
	}
	if _, err := os.Stat(prefix + ".pub"); err != nil {
		t.Fatalf("expected public key file: %v", err)
	}
}
