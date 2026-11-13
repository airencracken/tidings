package reader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/airencracken/comfylib/autobackup"
	"github.com/gofrs/flock"
	"modernc.org/sqlite"
)

type snapshotInfo struct {
	App     string `json:"app"`
	Version int    `json:"version"`
	Schema  int    `json:"schema"`
	SHA256  string `json:"sha256"`
}

func databaseURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// Snapshot holds a SQLite read transaction while copying its fixed page set with
// the online backup API. Its size is checked before any database bytes are copied.
func (s *Store) Snapshot(ctx context.Context, destination string, limit int64) (failure error) {
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	source, err := sql.Open("sqlite", databaseURI(filepath.Join(s.Directory, "reader.db"))+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer source.Close()
	conn, err := source.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var pages, size int64
	if err = conn.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return err
	}
	if err = conn.QueryRowContext(ctx, "PRAGMA page_size").Scan(&size); err != nil {
		return err
	}
	if size <= 0 || pages > (limit-1024)/size {
		return errors.New("database exceeds backup copy cap")
	}
	file := filepath.Join(destination, "reader.db")
	if err = conn.Raw(func(raw any) (err error) {
		backup, err := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		}).NewBackup(databaseURI(file))
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, backup.Finish()) }()
		for {
			if err = ctx.Err(); err != nil {
				return err
			}
			more, e := backup.Step(32)
			if e != nil {
				return e
			}
			if !more {
				return nil
			}
		}
	}); err != nil {
		return err
	}
	destinationDB, err := sql.Open("sqlite", databaseURI(file))
	if err != nil {
		return err
	}
	_, modeErr := destinationDB.ExecContext(ctx, "PRAGMA journal_mode=DELETE")
	if err = errors.Join(modeErr, destinationDB.Close()); err != nil {
		return err
	}
	if err = os.Chmod(file, 0600); err != nil {
		return err
	}
	if err = verifyDatabase(ctx, file); err != nil {
		return err
	}
	hash, err := fileHash(ctx, file, limit)
	if err != nil {
		return err
	}
	b, err := json.Marshal(snapshotInfo{"tidings", 1, 4, hash})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(destination, "snapshot.json"), b, 0600); err != nil {
		return err
	}
	return syncFile(file)
}
func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func fileHash(ctx context.Context, path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, limit+1))
	if err != nil {
		return "", err
	}
	if n > limit {
		return "", errors.New("file exceeds byte cap")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func verifyDatabase(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", databaseURI(path)+"?mode=ro&_pragma=trusted_schema(0)")
	if err != nil {
		return err
	}
	defer db.Close()
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version < 2 || version > 6 {
		return fmt.Errorf("unsupported backup schema %d", version)
	}
	var check string
	if err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return errors.New("backup database failed integrity check")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	bad := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if bad {
		return errors.New("backup database has broken references")
	}
	var owners int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE owner=1 AND suspended=0").Scan(&owners); err != nil {
		return err
	}
	if owners == 0 {
		return errors.New("backup has no active owner")
	}
	return nil
}

type backupManifest struct {
	Version int    `json:"version"`
	App     string `json:"app"`
	Created string `json:"created"`
	Archive string `json:"archive"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

// RestoreInstance publishes a verified database into an empty private destination.
// Existing instances are never overwritten; the caller switches directories after
// checking the restored instance. The exclusive lock also refuses a running target.
func RestoreInstance(ctx context.Context, archive, destination string, limit int64) (failure error) {
	if limit < 1024 {
		return errors.New("invalid restore byte cap")
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("restore destination must be a private directory")
	}
	lock := flock.New(filepath.Join(destination, ".instance.lock"), flock.SetPermissions(0600))
	ok, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("restore destination is in use")
	}
	defer lock.Close()
	files, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.Name() != ".instance.lock" {
			return errors.New("restore destination must be empty; choose a new directory")
		}
	}
	root, err := os.OpenRoot(archive)
	if err != nil {
		return err
	}
	defer root.Close()
	manifestFile, err := root.Open("manifest.json")
	if err != nil {
		return err
	}
	var m backupManifest
	d := json.NewDecoder(io.LimitReader(manifestFile, 16384))
	d.DisallowUnknownFields()
	err = d.Decode(&m)
	extra := d.Decode(new(any))
	manifestFile.Close()
	if err != nil || extra != io.EOF || m.Version != 1 || m.App != "tidings" || (m.Archive != "snapshot.tar.gz" && m.Archive != "snapshot.tar.zst") || m.Bytes < 1 || m.Bytes > limit {
		return errors.New("invalid Tidings backup manifest")
	}
	input, err := root.Open(m.Archive)
	if err != nil {
		return err
	}
	defer input.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, input}, limit+1))
	if err != nil {
		return err
	}
	if n != m.Bytes || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return errors.New("backup archive checksum mismatch")
	}
	if _, err = input.Seek(0, 0); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(destination, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if m.Archive == "snapshot.tar.gz" {
		z, err := gzip.NewReader(input)
		if err != nil {
			return err
		}
		err = extractSnapshot(ctx, z, stage, limit)
		returnErr := z.Close()
		if err != nil {
			return err
		}
		if returnErr != nil {
			return returnErr
		}
	} else {
		childCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		cmd := exec.CommandContext(childCtx, "zstd", "-q", "-d", "-c")
		cmd.Stdin = input
		cmd.Stderr = io.Discard
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err = cmd.Start(); err != nil {
			return err
		}
		err = extractSnapshot(ctx, pipe, stage, limit)
		if err != nil {
			cancel()
		}
		waitErr := cmd.Wait()
		if err != nil {
			return err
		}
		if waitErr != nil {
			return waitErr
		}
	}
	b, err := os.ReadFile(filepath.Join(stage, "snapshot.json"))
	if err != nil {
		return err
	}
	var meta snapshotInfo
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&meta); err != nil || d.Decode(new(any)) != io.EOF || meta.App != "tidings" || meta.Version != 1 || (meta.Schema < 2 || meta.Schema > 4) {
		return errors.New("invalid snapshot metadata")
	}
	file := filepath.Join(stage, "reader.db")
	hash, err := fileHash(ctx, file, limit)
	if err != nil {
		return err
	}
	if hash != meta.SHA256 {
		return errors.New("snapshot database checksum mismatch")
	}
	if err = verifyDatabase(ctx, file); err != nil {
		return err
	}
	if err = syncFile(file); err != nil {
		return err
	}
	if err = os.Rename(file, filepath.Join(destination, "reader.db")); err != nil {
		return err
	}
	return syncFile(destination)
}
func extractSnapshot(ctx context.Context, input io.Reader, stage string, limit int64) error {
	// Only two regular files are part of this format. No path from the tar header
	// is used until it has matched this allowlist.
	bounded := &io.LimitedReader{R: contextReader{ctx, input}, N: limit + 16384}
	tr := tar.NewReader(bounded)
	seen := map[string]bool{}
	remaining := limit
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || (header.Name != "reader.db" && header.Name != "snapshot.json") || seen[header.Name] || header.Size < 0 || header.Size > remaining || (header.Name == "snapshot.json" && header.Size > 1024) {
			return errors.New("invalid snapshot archive member")
		}
		seen[header.Name] = true
		remaining -= header.Size
		file, err := os.OpenFile(filepath.Join(stage, header.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, tr)
		err = errors.Join(copyErr, file.Sync(), file.Close())
		if err != nil {
			return err
		}
	}
	if _, err := io.Copy(io.Discard, bounded); err != nil {
		return err
	}
	if bounded.N <= 0 || !seen["reader.db"] || !seen["snapshot.json"] {
		return errors.New("incomplete or oversized snapshot archive")
	}
	return nil
}

// ManualBackup publishes in a separate namespace. Automatic retention can never
// remove these archives, and this function does not rotate earlier manual copies.
func (s *Store) ManualBackup(ctx context.Context, c autobackup.Config) (output string, failure error) {
	if err := os.MkdirAll(c.Directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(c.Directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("manual backup destination must be a private directory")
	}
	stage, err := os.MkdirTemp(c.Directory, ".manual-backup-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	parent := c.Directory
	c.Directory = stage
	archive, err := autobackup.Create(ctx, c, s.Snapshot)
	if err != nil {
		return "", err
	}
	output = filepath.Join(parent, "manual-"+filepath.Base(archive))
	if err = os.Rename(archive, output); err != nil {
		return "", err
	}
	return output, syncFile(parent)
}
