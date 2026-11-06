// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/airencracken/tidings/internal/reader"
)

func main() {
	port := flag.Int("port", 8083, "local demo port (0 chooses a free port)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: demo [-port PORT]")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *port, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

// run ignores TIDINGS_* settings and owns only its newly created temp directory.
func run(ctx context.Context, port int, out io.Writer) (err error) {
	if port < 0 || port > 65535 {
		return errors.New("demo port must be between 0 and 65535")
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("listen: %w; try make demo PORT=9000", err)
	}
	defer func() {
		if e := listener.Close(); e != nil && !errors.Is(e, net.ErrClosed) {
			err = errors.Join(err, e)
		}
	}()
	dir, err := os.MkdirTemp("", "tidings-demo-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	s, err := reader.OpenStore(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.DB.Close()) }()
	if err = seed(ctx, s); err != nil {
		return fmt.Errorf("seed demo: %w", err)
	}
	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	app, err := reader.NewApp(appCtx, s, reader.Config{Demo: true})
	if err != nil {
		return err
	}
	// Sample refreshes exercise the normal parser and persistence without egress.
	app.Fetcher = &reader.Fetcher{Client: &http.Client{Transport: sampleTransport{}, Timeout: 5 * time.Second}}
	pollDone := make(chan struct{})
	go func() { defer close(pollDone); app.Poll(appCtx, 30*time.Minute) }()
	defer func() { cancel(); <-pollDone; app.Wait() }()
	base := "http://" + listener.Addr().String()
	if _, err = fmt.Fprintf(out, "\nOpen %s\nOwner: demo\nMembers: freya, jules\nPassword for all three: %s\n\nTry personal sources, handpicked and tag-based groups, and saved stories.\nDemo and freya follow the household table; jules starts opted out.\nSample feeds refresh offline. No publisher requests are made in this demo.\nDemo data: %s\nPress Ctrl-C to stop and remove this demo. Each run starts fresh.\n\n", base, demoPassword, dir); err != nil {
		return err
	}
	return serve(ctx, listener, app)
}

func serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := server.Shutdown(shutdown)
		if err != nil {
			err = errors.Join(err, server.Close())
		}
		<-done
		return err
	}
}
