package main

import (
	"context"
	"github.com/airencracken/tidings/internal/reader"
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
	t.Setenv("TIDINGS_ADDR", "0.0.0.0:8085")
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

func TestBackupRestoreAndProxyCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TIDINGS_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("TIDINGS_BACKUP_INTERVAL", "0")
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	s, err := reader.OpenStore(os.Getenv("TIDINGS_DATA_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CreateOwner(context.Background(), "host", "quiet mornings and books"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	destination := filepath.Join(dir, "copies")
	os.Args = []string{"tidings", "backup", "--destination", destination}
	if err = run(); err != nil {
		t.Fatal(err)
	}
	copies, err := os.ReadDir(destination)
	if err != nil || len(copies) != 1 {
		t.Fatal(copies, err)
	}
	restored := filepath.Join(dir, "restored")
	os.Args = []string{"tidings", "restore", "--archive", filepath.Join(destination, copies[0].Name()), "--destination", restored}
	if err = run(); err != nil {
		t.Fatal(err)
	}
	s, err = reader.OpenStore(restored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Login(context.Background(), "host", "quiet mornings and books"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	t.Setenv("TIDINGS_DATA_DIR", filepath.Join(dir, "never-created"))
	for _, proxy := range []string{"caddy", "nginx", "apache"} {
		os.Args = []string{"tidings", "proxy-config", proxy, "--domain", "reader.example.net"}
		if err = run(); err != nil {
			t.Fatal(proxy, err)
		}
	}
	if _, err = os.Stat(os.Getenv("TIDINGS_DATA_DIR")); !os.IsNotExist(err) {
		t.Fatal("proxy config opened the database", err)
	}
}

func TestHelpListsEveryCommandAndSetting(t *testing.T) {
	for _, name := range append([]string{"TIDINGS_DATA_DIR", "tidings serve", "tidings setup", "tidings create-owner", "tidings set-password", "tidings backup", "tidings restore", "tidings proxy-config", "tidings --version"}, serviceSettings...) {
		if !strings.Contains(helpText, name) {
			t.Errorf("help does not mention %s", name)
		}
	}
	for _, line := range strings.Split(helpText, "\n") {
		if len(line) > 100 {
			t.Errorf("help line is too long for a terminal: %q", line)
		}
	}
}
