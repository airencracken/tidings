// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/airencracken/comfylib/password"
	"github.com/airencracken/comfylib/privdrop"
	"github.com/airencracken/comfylib/setup"
	"github.com/airencracken/comfylib/svcconfig"
	"github.com/airencracken/tidings/internal/reader"
)

func firstSetup(s *reader.Store, data, base, addr string) (*setup.Handler, error) {
	return setup.New(setup.Config{App: "tidings", Directory: data, BaseURL: base, Address: addr, Ready: s.HasOwner, Create: s.CreateFirstOwner})
}
func accountCommand(command string, args []string, in io.Reader, out io.Writer, paths svcconfig.Paths) error {
	flags := flag.NewFlagSet("tidings "+command, flag.ContinueOnError)
	flags.SetOutput(out)
	data := flags.String("data-dir", "", "private data directory (defaults to the installed service)")
	var username *string
	var source *password.Source
	if command != "setup" {
		username = flags.String("username", "", "account username (required)")
		source = password.Flags(flags)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if username != nil && *username == "" {
		return errors.New("provide --username")
	}
	if err := privdrop.RefuseRoot(os.Geteuid(), command, "sudo -u tidings tidings "+command); err != nil {
		return err
	}
	secret := ""
	var err error
	if source != nil {
		secret, err = source.Read(in, out)
		if err != nil {
			return err
		}
		if err = reader.ValidateCredentials(*username, secret); err != nil {
			return err
		}
	}
	if *data == "" {
		*data, err = paths.DataDir("TIDINGS_DATA_DIR")
	} else {
		*data, err = filepath.Abs(*data)
	}
	if err != nil {
		return err
	}
	s, err := reader.OpenStore(*data)
	if err != nil {
		return err
	}
	defer s.Close()
	if command == "setup" {
		settings, err := processSettings(paths)
		if err != nil {
			return err
		}
		addr := settings["TIDINGS_ADDR"]
		if addr == "" {
			addr = "127.0.0.1:8085"
		}
		wizard, err := firstSetup(s, *data, settings["TIDINGS_BASE_URL"], addr)
		if err != nil {
			return err
		}
		return wizard.Enable(context.Background(), out)
	}
	if command == "create-owner" {
		err = s.CreateOwner(context.Background(), *username, secret)
	} else {
		err = s.SetPassword(context.Background(), *username, secret)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Account updated.")
	return err
}
