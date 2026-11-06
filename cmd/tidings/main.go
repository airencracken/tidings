package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/airencracken/comfylib/password"
	"github.com/airencracken/tidings/internal/reader"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		slog.Error("tidings stopped", "error", err)
		os.Exit(1)
	}
}
func env(name, fallback string) string {
	if s := os.Getenv(name); s != "" {
		return s
	}
	return fallback
}
func run() error {
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help") {
		fmt.Println("Tidings — a quiet RSS and Atom reader for you and your people.\n\nUsage:\n  tidings serve\n  tidings create-owner --username NAME [--password-file PATH]\n  tidings set-password --username NAME [--password-file PATH]\n\nEnvironment:\n  TIDINGS_ADDR       Listen address (127.0.0.1:8083)\n  TIDINGS_DATA_DIR   Private SQLite data directory (./data)\n  TIDINGS_BASE_URL   Public HTTPS origin for shared invitations\n  TIDINGS_SECURE_COOKIES  true behind an HTTPS proxy\n\nPasswords are read twice with terminal echo disabled, or from a private file.\nBackups: stop Tidings, then copy its entire data directory.")
		return nil
	}
	dir, err := filepath.Abs(env("TIDINGS_DATA_DIR", "./data"))
	if err != nil {
		return err
	}
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command != "serve" && command != "create-owner" && command != "set-password" {
		return fmt.Errorf("unknown command %q; use tidings --help", command)
	}
	s, err := reader.OpenStore(dir)
	if err != nil {
		return err
	}
	defer s.DB.Close()
	if command != "serve" {
		fs := flag.NewFlagSet(command, flag.ContinueOnError)
		username := fs.String("username", "", "account username")
		passwordFile := fs.String("password-file", "", "read password from a private file instead of a terminal")
		if err = fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		password, err := readPassword(*passwordFile)
		if err != nil {
			return err
		}
		if command == "create-owner" {
			err = s.CreateOwner(context.Background(), *username, password)
		} else {
			err = s.SetPassword(context.Background(), *username, password)
		}
		if err != nil {
			return err
		}
		fmt.Println("Account updated.")
		return nil
	}
	base := os.Getenv("TIDINGS_BASE_URL")
	secure := os.Getenv("TIDINGS_SECURE_COOKIES") == "true"
	if base != "" {
		u, e := url.Parse(base)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("TIDINGS_BASE_URL must be an HTTPS origin, such as https://tidings.example.org")
		}
		secure = true
	}
	addr := env("TIDINGS_ADDR", "127.0.0.1:8083")
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && (!secure || base == "") {
		return errors.New("listening beyond loopback requires TIDINGS_BASE_URL and secure cookies; put Tidings behind an HTTPS reverse proxy")
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM users WHERE owner=1").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return errors.New("create your first owner with: tidings create-owner --username NAME")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	app, err := reader.NewApp(ctx, s, reader.Config{SecureCookies: secure, BaseURL: base})
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: app, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	pollDone := make(chan struct{})
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
	app.Wait()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}

func readPassword(file string) (string, error) {
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return "", err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1024 {
			return "", errors.New("password file must be a regular file, at most 1024 bytes, accessible only by its owner")
		}
		line, err := bufio.NewReader(f).ReadString('\n')
		if err != nil && len(line) == 0 {
			return "", err
		}
		return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("use a terminal for hidden password entry, or supply --password-file")
	}
	return password.Confirm(os.Stderr, "Password: ", "Repeat password: ", func() ([]byte, error) { return term.ReadPassword(fd) })
}
