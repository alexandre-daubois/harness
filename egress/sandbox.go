package egress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	settingsDirMode  = 0o755
	settingsFileMode = 0o644
)

type sandboxSettings struct {
	Permissions sandboxPermissions `json:"permissions"`
}

type sandboxPermissions struct {
	Allow   []string       `json:"allow"`
	Deny    []string       `json:"deny"`
	Sandbox sandboxDomains `json:"sandbox"`
}

type sandboxDomains struct {
	AllowedDomains []string `json:"allowedDomains"`
}

// WriteSandboxSettings writes Claude's workspace sandbox domain allowlist.
func WriteSandboxSettings(workspace string, allowedDomains []string) error {
	if workspace == "" {
		return fmt.Errorf("egress: workspace is required")
	}
	path := filepath.Join(workspace, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), settingsDirMode); err != nil {
		return fmt.Errorf("egress: create settings directory: %w", err)
	}
	settings := sandboxSettings{
		Permissions: sandboxPermissions{
			Allow: []string{},
			Deny:  []string{},
			Sandbox: sandboxDomains{
				AllowedDomains: allowedDomains,
			},
		},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("egress: marshal sandbox settings: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, settingsFileMode); err != nil {
		return fmt.Errorf("egress: write sandbox settings: %w", err)
	}
	return nil
}
