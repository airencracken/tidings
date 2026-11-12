package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/airencracken/comfylib/autobackup"
	"github.com/airencracken/comfylib/clientip"
	"github.com/airencracken/comfylib/password"
	"github.com/airencracken/comfylib/privdrop"
	"github.com/airencracken/comfylib/proxyconfig"
	"github.com/airencracken/comfylib/svcconfig"
	"github.com/airencracken/tidings/deploy"
	"github.com/airencracken/tidings/internal/reader"
	"github.com/gofrs/flock"
)

var version = "0.5.3"

var instanceCommands = map[string]bool{"setup": true, "create-owner": true, "set-password": true, "backup": true, "restore": true}
var serviceSettings = []string{"TIDINGS_BASE_URL", "TIDINGS_ADDR", "TIDINGS_SECURE_COOKIES", "TIDINGS_TRUSTED_PROXIES", "TIDINGS_BACKUP_INTERVAL", "TIDINGS_BACKUP_KEEP", "TIDINGS_BACKUP_MAX_BYTES", "TIDINGS_BACKUP_COPY_MAX_BYTES"}

func main() {
	paths := svcconfig.Detect("tidings", "/var/lib/tidings")
	handled, status, err := privdrop.Reexec(privdrop.Request{Args: os.Args[1:], Commands: instanceCommands, Paths: paths, DefaultUser: "tidings", Settings: func(command, dir string) (map[string]string, error) { return paths.Settings(serviceSettings...) }})
	if handled {
		if err != nil {
			slog.Error("administrative command", "error", err)
		}
		os.Exit(status)
	}
	if err := run(); err != nil {
		slog.Error("tidings stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && (os.Args[1] == "setup" || os.Args[1] == "create-owner" || os.Args[1] == "set-password") {
		return accountCommand(os.Args[1], os.Args[2:], os.Stdin, os.Stdout, svcconfig.Detect("tidings", "/var/lib/tidings"))
	}
	if len(os.Args) > 2 && os.Args[1] == "help" && (os.Args[2] == "setup" || os.Args[2] == "create-owner" || os.Args[2] == "set-password") {
		return accountCommand(os.Args[2], []string{"--help"}, os.Stdin, os.Stdout, svcconfig.Detect("tidings", "/var/lib/tidings"))
	}
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("tidings", version)
		return nil
	}
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help") {
		fmt.Println("Tidings — a quiet RSS and Atom reader for you and your people.\n\nUsage:\n  tidings setup\n  tidings serve\n  tidings create-owner --username NAME [--password-prompt | --password-stdin | --password-file PATH]\n  tidings set-password --username NAME [--password-prompt | --password-stdin | --password-file PATH]\n  tidings backup --destination PRIVATE_DIRECTORY\n  tidings restore --archive BACKUP_DIRECTORY --destination NEW_DATA_DIRECTORY\n  tidings proxy-config caddy|nginx|apache --domain YOUR_DOMAIN\n\nEnvironment:\n  TIDINGS_ADDR       Listen address (127.0.0.1:8085)\n  TIDINGS_DATA_DIR   Private SQLite data directory (./data)\n  TIDINGS_BASE_URL   Public HTTPS origin for shared invitations\n  TIDINGS_SECURE_COOKIES  true behind an HTTPS proxy\n  TIDINGS_TRUSTED_PROXIES Explicit trusted proxy IPs or CIDRs\n  TIDINGS_BACKUP_INTERVAL 24h (0 disables); KEEP=7\n  TIDINGS_BACKUP_MAX_BYTES 10 GiB retained; COPY_MAX_BYTES 5 GiB per snapshot\n\nPasswords are read twice with terminal echo disabled, or from a private file.\nVerified automatic backups are stored in the private backups directory. Restore into a new directory, then switch your service configuration.")
		return nil
	}
	if len(os.Args) > 1 && os.Args[1] == "proxy-config" {
		return proxyconfig.Run(proxyconfig.Spec{App: "tidings", ExampleDomain: "tidings.example.org", DefaultUpstream: "127.0.0.1:8085", ProxyEnvironment: "TIDINGS_TRUSTED_PROXIES=127.0.0.1/32,::1/128", Examples: deploy.Examples}, os.Args[2:], os.Stdout)
	}
	if len(os.Args) > 2 && privdrop.HasHelpFlag(os.Args[2:]) {
		fmt.Println("Use tidings --help for commands and settings. backup needs --destination; restore needs --archive and --destination. Account commands accept --username and --password-file.")
		return nil
	}
	paths := svcconfig.Detect("tidings", "/var/lib/tidings")
	settings, err := processSettings(paths)
	if err != nil {
		return err
	}
	lookup := func(name string) string { return settings[name] }
	dir, err := paths.DataDir("TIDINGS_DATA_DIR")
	if err != nil {
		return err
	}
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command != "serve" && command != "create-owner" && command != "set-password" && command != "backup" && command != "restore" {
		return fmt.Errorf("unknown command %q; use tidings --help", command)
	}
	if instanceCommands[command] && !privdrop.HasHelpFlag(os.Args[2:]) {
		if err = privdrop.RefuseRoot(os.Geteuid(), command, "sudo -u tidings tidings "+command); err != nil {
			return err
		}
	}
	backupConfig, err := autobackup.Load("tidings", dir, lookup)
	if err != nil {
		return err
	}
	if command == "restore" {
		fs := flag.NewFlagSet(command, flag.ContinueOnError)
		archive := fs.String("archive", "", "verified backup directory")
		destination := fs.String("destination", "", "new private data directory")
		if err = fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 || *archive == "" || *destination == "" {
			return errors.New("restore needs --archive and --destination")
		}
		dest, err := filepath.Abs(*destination)
		if err != nil {
			return err
		}
		return reader.RestoreInstance(context.Background(), *archive, dest, backupConfig.CopyMaxBytes)
	}
	s, err := reader.OpenStore(dir)
	if err != nil {
		return err
	}
	defer s.Close()
	if command == "backup" {
		fs := flag.NewFlagSet(command, flag.ContinueOnError)
		destination := fs.String("destination", "", "private manual-backup directory")
		if err = fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 || *destination == "" {
			return errors.New("backup needs --destination")
		}
		dest, err := filepath.Abs(*destination)
		if err != nil {
			return err
		}
		if dest == backupConfig.Directory {
			return errors.New("use a separate directory for manual backups")
		}
		lock := flock.New(filepath.Join(dir, ".backup.lock"), flock.SetPermissions(0600))
		locked, err := lock.TryLock()
		if err != nil {
			return err
		}
		if !locked {
			return errors.New("another manual backup is running")
		}
		defer lock.Close()
		backupConfig.Directory = dest
		backupConfig.Keep = 30
		path, err := s.ManualBackup(context.Background(), backupConfig)
		if err == nil {
			fmt.Println("Verified backup: " + path)
		}
		return err
	}

	base := lookup("TIDINGS_BASE_URL")
	secure := lookup("TIDINGS_SECURE_COOKIES") == "true"
	if base != "" {
		base, err = reader.PublicOrigin(base)
		if err != nil {
			return fmt.Errorf("TIDINGS_BASE_URL: %w", err)
		}
		secure = secure || strings.HasPrefix(base, "https://")
	}
	addr := lookup("TIDINGS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8085"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && (!secure || base == "") {
		return errors.New("listening beyond loopback requires TIDINGS_BASE_URL and secure cookies; put Tidings behind an HTTPS reverse proxy")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	serverLock := flock.New(filepath.Join(dir, ".server.lock"), flock.SetPermissions(0600))
	locked, err := serverLock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("another Tidings server is using this instance")
	}
	defer serverLock.Close()
	trusted, err := clientip.ParseTrusted(lookup("TIDINGS_TRUSTED_PROXIES"), "TIDINGS_TRUSTED_PROXIES")
	if err != nil {
		return err
	}
	backups, err := autobackup.New(backupConfig, s.Snapshot, slog.Default())
	if err != nil {
		return err
	}
	app, err := reader.NewApp(ctx, s, reader.Config{SecureCookies: secure, BaseURL: base, TrustedProxies: clientip.Resolver{Trusted: trusted}, Backups: backups})
	if err != nil {
		return err
	}
	wizard, err := firstSetup(s, dir, base, addr)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: wizard.Wrap(app), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	pollDone := make(chan struct{})
	backupDone := make(chan struct{})
	go func() {
		defer close(backupDone)
		if wizard.Wait(ctx) == nil {
			backups.Run(ctx)
		}
	}()
	go func() { defer close(pollDone); app.Poll(ctx, 30*time.Minute) }()
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()
	slog.Info("Tidings is ready", "url", "http://"+listener.Addr().String())
	select {
	case err = <-stopped:
		cancel()
	case <-ctx.Done():
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	shutdownErr := server.Shutdown(shutdownCtx)
	cancel()
	<-pollDone
	<-backupDone
	app.Wait()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}

func readPassword(file string) (string, error) {
	return (password.Source{File: file}).Read(os.Stdin, os.Stderr)
}

// Service managers and root administrative re-execution already supply the
// resolved environment. A service account must not reopen root-only config.
func processSettings(paths svcconfig.Paths) (map[string]string, error) {
	if os.Geteuid() == 0 {
		return paths.Settings(serviceSettings...)
	}
	settings := map[string]string{}
	for _, key := range serviceSettings {
		if value := os.Getenv(key); value != "" {
			settings[key] = value
		}
	}
	return settings, nil
}
