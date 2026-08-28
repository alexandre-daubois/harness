//go:build integration

package container

import (
	"io"
	"os"
	"testing"

	"github.com/alpha-omega-security/harness"
)

type integrationHarness struct{}

func (integrationHarness) Binary() string                             { return "true" }
func (integrationHarness) Args(harness.Job) []string                  { return nil }
func (integrationHarness) Prompt(harness.Job) string                  { return "" }
func (integrationHarness) ParseStream(io.Reader, func(harness.Event)) {}
func (integrationHarness) SkillDir(string, string) string             { return "" }
func (integrationHarness) GuideFilename() string                      { return "AGENTS.md" }
func (integrationHarness) SystemPromptViaArgs() bool                  { return true }
func (integrationHarness) EgressHosts() []string                      { return nil }
func (integrationHarness) Env(string) []string                        { return nil }
func (integrationHarness) StateEnv(string) []string                   { return nil }
func (integrationHarness) AccountErrorText(string) string             { return "" }
func (integrationHarness) DefaultModels() []harness.ModelDefault      { return nil }

func TestIntegrationHardenedRunner(t *testing.T) {
	image := os.Getenv("HARNESS_TEST_RUNNER_IMAGE")
	if image == "" {
		t.Skip("set HARNESS_TEST_RUNNER_IMAGE to a cached image containing true and grep")
	}
	rt, ok := DetectRuntime("docker")
	if !ok {
		t.Skip("docker is unavailable")
	}
	if !imageExistsLocally(t.Context(), rt, image) {
		t.Skipf("image %q is not cached", image)
	}
	runner := Runner{
		Runtime:  rt,
		Image:    image,
		Hardened: true,
		ProxyURL: "http://harness:token@host.docker.internal:3128",
	}
	if err := runner.Run(t.Context(), integrationHarness{}, harness.Job{Workspace: t.TempDir()}, func(harness.Event) {}); err != nil {
		t.Fatal(err)
	}
}
