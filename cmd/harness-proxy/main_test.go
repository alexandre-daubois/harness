package main

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/alpha-omega-security/harness/container"
	"github.com/alpha-omega-security/harness/egress"
)

func TestParseProxyConfigFromSidecarEnv(t *testing.T) {
	env := map[string]string{}
	cfg := container.SidecarConfig{
		Token:     "tok",
		Allow:     []string{"api.example.test", "*.example.test"},
		APIPort:   "8080",
		HostPorts: []string{"11434", "1234"},
		GatewayIP: "192.0.2.9",
	}
	for _, assignment := range container.SidecarEnv(cfg, egress.ListenFirstIface+":3128") {
		key, value, _ := strings.Cut(assignment, "=")
		env[key] = value
	}
	got, err := parseProxyConfig(nil, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if got.token != cfg.Token || got.apiHost != cfg.GatewayIP || got.apiPort != cfg.APIPort {
		t.Errorf("config = %+v", got)
	}
	if !slices.Equal(got.allow, cfg.Allow) || !slices.Equal(got.hostPorts, cfg.HostPorts) {
		t.Errorf("config = %+v", got)
	}
	if got.listen != egress.ListenFirstIface+":3128" {
		t.Errorf("listen = %q", got.listen)
	}
}

func TestParseProxyConfigValidation(t *testing.T) {
	getenv := func(string) string { return "" }
	if _, err := parseProxyConfig(nil, getenv); err == nil {
		t.Fatal("empty config returned nil error")
	}
	if _, err := parseProxyConfig([]string{"-token", "tok", "-allow", "api.example.test", "-api-port", "8080"}, getenv); err == nil {
		t.Fatal("api-port without api-host returned nil error")
	}
}

func TestResolveListen(t *testing.T) {
	got, err := resolveListen(egress.ListenFirstIface+":3128", func() (string, error) { return "10.89.1.2", nil })
	if err != nil || got != "10.89.1.2:3128" {
		t.Errorf("resolveListen = %q, %v", got, err)
	}
	if _, err := resolveListen(egress.ListenFirstIface+":3128", func() (string, error) { return "", errors.New("no interface") }); err == nil {
		t.Fatal("resolution failure returned nil error")
	}
	got, err = resolveListen("127.0.0.1:3128", func() (string, error) { return "", errors.New("must not run") })
	if err != nil || got != "127.0.0.1:3128" {
		t.Errorf("explicit listen = %q, %v", got, err)
	}
}
