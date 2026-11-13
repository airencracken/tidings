# Set up Tidings

Set your public HTTPS address and listener first, then create the first owner
in the browser or in a terminal. Installed commands find the service's data
directory and switch to its account when run with `sudo`. For a portable
install, run them as the account that owns the data directory.

## In the browser

```sh
sudo tidings setup
```

The command prints the setup address and a private setup key. Start the
service, open `/setup`, and enter the key with a username and password. Use
HTTPS unless the browser runs on the same machine. Keep the key private. The
page closes once any owner exists, and never replaces an account.

## In a terminal

```sh
sudo tidings create-owner --username yourname
sudo tidings set-password --username yourname
```

Both commands prompt twice with echo off. For automation, use
`--password-stdin` or `--password-file /private/path/password` with a private
file. `--data-dir DIRECTORY` selects a portable instance. Passwords need 12 or
more characters, up to 72 bytes. `create-owner` never changes an existing
account. `set-password` signs that account out everywhere.

Run `tidings setup --help` for details. Invitations and other settings are in
the app once you sign in.
