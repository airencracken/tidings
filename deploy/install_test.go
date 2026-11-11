package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestServiceInstallPreservesConfigurationSymlinks(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, exists := range []bool{false, true} {
			t.Run(manager+map[bool]string{false: "/dangling", true: "/existing"}[exists], func(t *testing.T) {
				stage := t.TempDir()
				relative := "etc/tidings/tidings.env"
				if manager == "openrc" {
					relative = "etc/conf.d/tidings"
				}
				destination := filepath.Join(stage, relative)
				if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(stage, "managed configuration.env")
				const original = "TIDINGS_DATA_DIR=/srv/custom-reader\n"
				if exists {
					if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, destination); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					// The service installation is independent of the binary build.
					command := exec.Command("make", "-C", "..", "-o", "install", "install-"+manager, "DESTDIR="+stage)
					if output, err := command.CombinedOutput(); err != nil {
						t.Fatalf("install: %v\n%s", err, output)
					}
					if got, err := os.Readlink(destination); err != nil || got != target {
						t.Fatalf("configuration symlink replaced: %q, %v", got, err)
					}
					contents, err := os.ReadFile(target)
					if exists && (err != nil || string(contents) != original) || !exists && !os.IsNotExist(err) {
						t.Fatal("installation changed the managed configuration target")
					}
				}
			})
		}
	}
}
