package deploy

import (
	"os"
	"strings"
	"testing"

	"github.com/airencracken/comfylib/proxyconfig"
	"github.com/airencracken/comfylib/svcconfig"
)

func TestProxyExamplesRenderAndRejectInjection(t *testing.T) {
	spec := proxyconfig.Spec{App: "tidings", ExampleDomain: "tidings.example.org", DefaultUpstream: "127.0.0.1:8085", Examples: Examples}
	for _, server := range []string{"caddy", "nginx", "apache"} {
		got, err := proxyconfig.Render(spec, proxyconfig.Options{Server: server, Domain: "reader.example.net", Upstream: "127.0.0.1:9000"})
		if err != nil || !strings.Contains(got, "reader.example.net") || !strings.Contains(got, "127.0.0.1:9000") || strings.Contains(got, "tidings.example.org") {
			t.Fatal(server, got, err)
		}
		if _, err = proxyconfig.Render(spec, proxyconfig.Options{Server: server, Domain: "example.org\nmalicious directive"}); err == nil {
			t.Fatal("config injection accepted")
		}
	}
}
func TestServiceConfigurationMatchesCLI(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		var paths svcconfig.Paths
		paths.Name = "Tidings"
		paths.Prefix = "TIDINGS_"
		paths.DefaultDataDir = "/var/lib/tidings"
		if manager == "systemd" {
			paths.SystemdUnit = "systemd/tidings.service"
		} else {
			paths.OpenRCInstalled = true
			paths.OpenRCConfig = "openrc/tidings.conf"
		}
		u, g, managed, err := paths.Account("tidings")
		if err != nil || !managed || u != "tidings" || g != "tidings" {
			t.Fatal(manager, u, g, managed, err)
		}
		if manager == "openrc" {
			value, err := paths.Setting("TIDINGS_DATA_DIR", "", "data directory")
			if err != nil || value != "/var/lib/tidings" {
				t.Fatal(value, err)
			}
		}
	}
	b, err := os.ReadFile("systemd/tidings.service")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"StateDirectoryMode=0700", "UMask=0077", "NoNewPrivileges=true", "ExecStart=/usr/local/bin/tidings serve"} {
		if !strings.Contains(string(b), want) {
			t.Fatal("service setting missing", want)
		}
	}
}
