package reader

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airencracken/comfylib/autobackup"
)

func TestVerifiedBackupAndRestore(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "backed-up", false)
	if err := s.SetState(ctx, 2, entryID(t, s, f), "saved", true); err != nil {
		t.Fatal(err)
	}
	config, err := autobackup.Load("tidings", t.TempDir(), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	archive, err := autobackup.Create(ctx, config, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if err = RestoreInstance(ctx, archive, destination, 5<<30); err != nil {
		t.Fatal(err)
	}
	next, err := OpenStore(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if got := entries(t, next, 2, "saved"); len(got) != 1 {
		t.Fatal("saved state missing", got)
	}
	if err = RestoreInstance(ctx, archive, destination, 5<<30); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatal("live destination accepted", err)
	}
	small := filepath.Join(t.TempDir(), "too-small")
	if err = s.Snapshot(ctx, small, 1024); err == nil {
		t.Fatal("copy cap ignored")
	}
	if _, err = os.Stat(filepath.Join(small, "reader.db")); !os.IsNotExist(err) {
		t.Fatal("oversized database was copied before the cap check", err)
	}
	if err = RestoreInstance(ctx, archive, filepath.Join(t.TempDir(), "small-restore"), 1024); err == nil {
		t.Fatal("restore cap ignored")
	}
	manifest := filepath.Join(archive, "manifest.json")
	b, _ := os.ReadFile(manifest)
	os.WriteFile(manifest, bytes.Replace(b, []byte(`"sha256":"`), []byte(`"sha256":"bad`), 1), 0600)
	dest := filepath.Join(t.TempDir(), "corrupt")
	if err = RestoreInstance(ctx, archive, dest, 5<<30); err == nil {
		t.Fatal("corrupt archive accepted")
	}
	if _, err = os.Stat(filepath.Join(dest, "reader.db")); !os.IsNotExist(err) {
		t.Fatal("failed restore published a database", err)
	}
}
func TestSnapshotArchiveAdversarialMembers(t *testing.T) {
	for _, name := range []string{"../escape", "/tmp/escape", "nested/reader.db", "reader.db"} {
		for _, kind := range []byte{tar.TypeReg, tar.TypeSymlink, tar.TypeLink} {
			t.Run(name+string(kind), func(t *testing.T) {
				var b bytes.Buffer
				w := tar.NewWriter(&b)
				for _, valid := range []string{"reader.db", "snapshot.json"} {
					if err := w.WriteHeader(&tar.Header{Name: valid, Typeflag: tar.TypeReg, Size: 0}); err != nil {
						t.Fatal(err)
					}
				}
				w.WriteHeader(&tar.Header{Name: name, Typeflag: kind, Size: 0, Linkname: "/tmp/escape"})
				w.Close()
				if err := extractSnapshot(context.Background(), &b, t.TempDir(), 1024); err == nil {
					t.Fatal("invalid/incomplete archive accepted")
				}
			})
		}
	}
}

func TestGzipBackupAndManualRetention(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	config, err := autobackup.Load("tidings", t.TempDir(), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	config.Keep = 1
	first, err := s.ManualBackup(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ManualBackup(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(first); err != nil {
		t.Fatal("manual backup rotated", err)
	}
	if err = RestoreInstance(ctx, second, filepath.Join(t.TempDir(), "gzip"), 5<<30); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.Snapshot(canceled, filepath.Join(t.TempDir(), "canceled"), 5<<30); err == nil {
		t.Fatal("canceled snapshot succeeded")
	}
}

func TestSnapshotKeepsConcurrentTransactionsConsistent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.DB.Exec("CREATE TABLE ledger(a INTEGER NOT NULL,b INTEGER NOT NULL);INSERT INTO ledger VALUES(0,0)"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	stopped := make(chan error, 1)
	go func() {
		for {
			select {
			case <-done:
				stopped <- nil
				return
			default:
			}
			if _, err := s.DB.Exec("UPDATE ledger SET a=a+1,b=b-1"); err != nil {
				stopped <- err
				return
			}
		}
	}()
	defer func() {
		close(done)
		if err := <-stopped; err != nil {
			t.Error(err)
		}
	}()
	for range 3 {
		destination := filepath.Join(t.TempDir(), "snapshot")
		if err := s.Snapshot(ctx, destination, 5<<30); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", filepath.Join(destination, "reader.db"))
		if err != nil {
			t.Fatal(err)
		}
		var sum int
		err = db.QueryRow("SELECT a+b FROM ledger").Scan(&sum)
		db.Close()
		if err != nil || sum != 0 {
			t.Fatal("inconsistent concurrent snapshot", sum, err)
		}
	}
}
