// Package deploy embeds reference reverse proxy configurations for the CLI.
package deploy

import "embed"

//go:embed caddy/* nginx/* apache/*
var Examples embed.FS
