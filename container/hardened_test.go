package container

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/alpha-omega-security/harness"
	"github.com/alpha-omega-security/harness/egress"
)

type hardenedStubHarness struct{ stubHarness }

func (hardenedStubHarness) EgressHosts() []string { return []string{"api.example.test"} }

func TestRunnerRunHardenedSidecarLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test runtime is a POSIX shell script")
	}
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "runtime.log")
	runtimePath := filepath.Join(binDir, "podman")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$HARNESS_RUNTIME_LOG"
if [ "$1" = "network" ] && [ "$2" = "inspect" ]; then
  exit 1
fi
if [ "$1" = "run" ]; then
  case "$*" in
    *"--entrypoint grep"*) printf '%s\n' '192.0.2.1 hgw'; exit 0 ;;
    *"http://1.1.1.1"*) printf '%s\n' 'BLOCKED'; exit 0 ;;
    *"http://10.89.1.2:3128/"*) printf '%s\n' 'REACHED'; exit 0 ;;
  esac
fi
if [ "$1" = "inspect" ]; then
  printf '%s\n' '10.89.1.2'
  exit 0
fi
if [ "$1" = "logs" ]; then
  printf '%s\n' 'time=t level=INFO msg="ready"'
  printf '%s\n' 'time=t level=WARN msg="egress denied" host=blocked.test'
fi
exit 0
`
	if err := os.WriteFile(runtimePath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HARNESS_RUNTIME_LOG", logPath)

	var events []harness.Event
	runner := Runner{
		Runtime:  Runtime{Bin: "podman", Rootless: true},
		Image:    "img:latest",
		Hardened: true,
		Sidecar: SidecarConfig{
			Token:     "tok",
			Allow:     []string{"packages.example.test"},
			APIPort:   "8080",
			GatewayIP: "192.0.2.9",
		},
	}
	err := runner.Run(t.Context(), hardenedStubHarness{}, harness.Job{Workspace: t.TempDir()}, func(event harness.Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	var network string
	for line := range strings.SplitSeq(log, "\n") {
		if strings.HasPrefix(line, "network create --internal --disable-dns -- ") {
			network = strings.TrimPrefix(line, "network create --internal --disable-dns -- ")
			break
		}
	}
	if !strings.HasPrefix(network, hardenedNetworkPrefix) {
		t.Fatalf("network create missing from runtime log:\n%s", log)
	}
	for _, want := range []string{
		"run -d --name " + strings.Replace(network, hardenedNetworkPrefix, proxySidecarPrefix, 1) + " --network " + network,
		ProxyAllowEnv + "=" + egress.HostGatewayAlias + ",api.example.test,packages.example.test",
		"-- img:latest harness-proxy",
		"network connect -- podman",
		"--network " + network,
		"HTTPS_PROXY=http://harness:tok@10.89.1.2:3128",
		"--read-only",
		"stub --headless",
		"network rm -- " + network,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("runtime log missing %q:\n%s", want, log)
		}
	}
	if len(events) != 1 || events[0].Kind != harness.KindEgress || !strings.Contains(events[0].Text, "blocked.test") {
		t.Errorf("egress events = %+v", events)
	}
}

func TestRunnerRunHardenedValidation(t *testing.T) {
	job := harness.Job{Workspace: t.TempDir()}
	if err := (Runner{Hardened: true, Network: "existing"}).Run(t.Context(), stubHarness{}, job, nil); err == nil || !strings.Contains(err.Error(), "cannot both") {
		t.Errorf("Hardened plus Network error = %v", err)
	}
	if err := (Runner{Hardened: true}).Run(t.Context(), stubHarness{}, job, nil); err == nil || !strings.Contains(err.Error(), "ProxyURL is required") {
		t.Errorf("missing ProxyURL error = %v", err)
	}
	if err := (Runner{Hardened: true, ProxyURL: "http://proxy"}).Run(t.Context(), stubHarness{}, job, nil); err == nil || !strings.Contains(err.Error(), "invalid ProxyURL") {
		t.Errorf("invalid ProxyURL error = %v", err)
	}
}

func TestResolveSidecarConfigAddsHarnessHosts(t *testing.T) {
	runner := Runner{
		Runtime: Runtime{Bin: "podman", Rootless: true},
		Sidecar: SidecarConfig{
			Token:     "tok",
			Allow:     []string{"API.EXAMPLE.TEST", "packages.example.test"},
			APIPort:   "8080",
			GatewayIP: "192.0.2.9",
		},
	}
	got, err := runner.resolveSidecarConfig(context.Background(), hardenedStubHarness{}, "runner:latest")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{egress.HostGatewayAlias, "api.example.test", "packages.example.test"}
	if !slices.Equal(got.Allow, want) {
		t.Errorf("Allow = %v, want %v", got.Allow, want)
	}
	if got.Image != "runner:latest" {
		t.Errorf("Image = %q", got.Image)
	}
}

func TestSidecarRunArgs(t *testing.T) {
	cfg := SidecarConfig{
		Image:     "proxy:latest",
		Token:     "tok",
		Allow:     []string{"api.example.test"},
		APIPort:   "8080",
		HostPorts: []string{"11434"},
		GatewayIP: "192.0.2.9",
	}
	args := sidecarRunArgs(cfg, "harness-proxy-a", "harness-hardened-a")
	for _, pair := range [][2]string{
		{"--name", "harness-proxy-a"},
		{"--network", "harness-hardened-a"},
		{"--cap-drop", "ALL"},
		{"--security-opt", "no-new-privileges"},
		{"--add-host", egress.HostGatewayAlias + ":192.0.2.9"},
		{"-e", ProxyListenEnv + "=" + egress.ListenFirstIface + ":3128"},
		{"-e", ProxyHostPortsEnv + "=11434"},
	} {
		if !hasAdjacent(args, pair[0], pair[1]) {
			t.Errorf("missing %q %q in %v", pair[0], pair[1], args)
		}
	}
	if !slices.Contains(args, "--read-only") {
		t.Errorf("missing --read-only in %v", args)
	}
	if tail := args[len(args)-3:]; !slices.Equal(tail, []string{"--", "proxy:latest", "harness-proxy"}) {
		t.Errorf("sidecar tail = %v", tail)
	}
}

func TestHardenedProbeArgs(t *testing.T) {
	rt := Runtime{Bin: "podman", Rootless: true}
	block := rt.hardenedEgressBlockArgs("harness-hardened-a", "img:latest")
	if !hasAdjacent(block, "--network", "harness-hardened-a") || !strings.Contains(strings.Join(block, " "), "1.1.1.1") {
		t.Errorf("block args = %v", block)
	}
	if strings.Contains(strings.Join(block, " "), "HTTPS_PROXY") {
		t.Errorf("block probe contains proxy env: %v", block)
	}
	reach := sidecarReachArgs("harness-hardened-a", "10.89.1.2:3128", "img:latest")
	if !hasAdjacent(reach, "--network", "harness-hardened-a") || !strings.Contains(strings.Join(reach, " "), "http://10.89.1.2:3128/") {
		t.Errorf("sidecar reach args = %v", reach)
	}
}

func TestHardenedNetworkCreateArgs(t *testing.T) {
	docker := hardenedNetworkCreateArgs(Runtime{Bin: "docker"}, "harness-hardened-a")
	if slices.Contains(docker, "--disable-dns") {
		t.Errorf("docker args contain unsupported --disable-dns: %v", docker)
	}
	podman := hardenedNetworkCreateArgs(Runtime{Bin: "podman", Rootless: true}, "harness-hardened-a")
	if !slices.Contains(podman, "--disable-dns") {
		t.Errorf("rootless podman args missing --disable-dns: %v", podman)
	}
	for _, args := range [][]string{docker, podman} {
		if tail := args[len(args)-2:]; !slices.Equal(tail, []string{"--", "harness-hardened-a"}) {
			t.Errorf("network name is not protected by --: %v", args)
		}
	}
}

func TestEmitProxyLogLines(t *testing.T) {
	var events []harness.Event
	emitProxyLogLines([]byte("time=t level=INFO msg=ready\ntime=t level=INFO msg=\"egress allowed\" host=api.test\ntime=t level=WARN msg=denied\ntime=t level=ERROR msg=failed\n"), func(event harness.Event) {
		events = append(events, event)
	})
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	for _, event := range events {
		if event.Kind != harness.KindEgress || !strings.HasPrefix(event.Text, "egress-proxy: ") {
			t.Errorf("event = %+v", event)
		}
	}
}

func TestHardenedHelpers(t *testing.T) {
	if got := routeHexIPv4("0102A8C0"); got != "192.168.2.1" {
		t.Errorf("routeHexIPv4 = %q", got)
	}
	if got := proxyURLWithHost("http://harness:tok@host.docker.internal:3128", "192.0.2.1"); got != "http://harness:tok@192.0.2.1:3128" {
		t.Errorf("proxyURLWithHost = %q", got)
	}
	if port, err := proxyPortFromURL("http://harness:tok@host:3128"); err != nil || port != "3128" {
		t.Errorf("proxyPortFromURL = %q, %v", port, err)
	}
	for _, raw := range []string{"http://host", "http://host:70000", "http://host:bad"} {
		if _, err := proxyPortFromURL(raw); err == nil {
			t.Errorf("proxyPortFromURL(%q) returned nil error", raw)
		}
	}
	got := prefixedNames([]byte("harness-hardened-a\nmy-harness-hardened-b\nharness-hardened-c\n"), hardenedNetworkPrefix)
	if !slices.Equal(got, []string{"harness-hardened-a", "harness-hardened-c"}) {
		t.Errorf("prefixedNames = %v", got)
	}
}

func TestSweepHardened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test runtime is a POSIX shell script")
	}
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "runtime.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$HARNESS_RUNTIME_LOG"
if [ "$1" = "ps" ]; then
  printf '%s\n' 'harness-proxy-a' 'my-harness-proxy-b' 'harness-proxy-c'
fi
if [ "$1" = "network" ] && [ "$2" = "ls" ]; then
  printf '%s\n' 'harness-hardened-a' 'my-harness-hardened-b' 'harness-hardened-c'
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "podman"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HARNESS_RUNTIME_LOG", logPath)

	result, err := SweepHardened(t.Context(), Runtime{Bin: "podman", Rootless: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProxySidecars != 2 || result.Networks != 2 {
		t.Errorf("result = %+v", result)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"rm -f -- harness-proxy-a",
		"rm -f -- harness-proxy-c",
		"network rm -- harness-hardened-a",
		"network rm -- harness-hardened-c",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("runtime log missing %q: %s", want, log)
		}
	}
	if strings.Contains(log, "rm -f -- my-") || strings.Contains(log, "network rm -- my-") {
		t.Errorf("sweep removed a substring-only match: %s", log)
	}
}
