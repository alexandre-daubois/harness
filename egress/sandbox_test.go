package egress

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWriteSandboxSettings(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	domains := []string{"api.anthropic.com", "packages.example.test"}
	if err := WriteSandboxSettings(workspace, domains); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings sandboxSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Permissions.Allow == nil || settings.Permissions.Deny == nil {
		t.Errorf("allow and deny must be JSON arrays: %s", data)
	}
	if !reflect.DeepEqual(settings.Permissions.Sandbox.AllowedDomains, domains) {
		t.Errorf("allowed domains = %v", settings.Permissions.Sandbox.AllowedDomains)
	}
}
