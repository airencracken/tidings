package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "password")
	if err := os.WriteFile(file, []byte("a quiet morning with books\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readPassword(file)
	if err != nil || got != "a quiet morning with books" {
		t.Fatalf("read password %q %v", got, err)
	}
	if err = os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = readPassword(file); err == nil {
		t.Fatal("public password file accepted")
	}
	if _, err = readPassword(dir); err == nil {
		t.Fatal("directory accepted as password file")
	}
	if err = os.WriteFile(file, []byte(strings.Repeat("x", 1025)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readPassword(file); err == nil {
		t.Fatal("oversized file accepted")
	}
}
func TestCommandsAndConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TIDINGS_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("TIDINGS_BASE_URL", "")
	t.Setenv("TIDINGS_SECURE_COOKIES", "")
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	password := filepath.Join(dir, "password")
	if err := os.WriteFile(password, []byte("quiet mornings and books"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"create-owner", "set-password"} {
		os.Args = []string{"tidings", command, "--username", "host", "--password-file", password}
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	os.Args = []string{"tidings", "serve"}
	t.Setenv("TIDINGS_ADDR", "0.0.0.0:8083")
	if err := run(); err == nil || !strings.Contains(err.Error(), "beyond loopback") {
		t.Fatalf("unsafe bind accepted %v", err)
	}
	t.Setenv("TIDINGS_BASE_URL", "https://example.org/path")
	if err := run(); err == nil {
		t.Fatal("base URL with path accepted")
	}
	os.Args = []string{"tidings", "unknown"}
	if err := run(); err == nil {
		t.Fatal("unknown command accepted")
	}
}
