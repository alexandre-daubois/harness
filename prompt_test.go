package harness

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPromptBytes(t *testing.T) {
	t.Parallel()

	const validation = ` Validate ./report.json against ./schema.json before finishing.`
	job := Job{SkillName: "audit", OutputFile: "report.json"}
	tests := []struct {
		name string
		h    Harness
		want string
	}{
		{
			name: "claude",
			h:    ClaudeHarness{},
			want: `Use the "audit" skill on the repository cloned at ./src. Write your structured output to ./report.json as the skill specifies.` + validation,
		},
		{
			name: "codex",
			h:    CodexHarness{},
			want: `Follow the instructions in ./skills/audit/SKILL.md against the repository cloned at ./src. Write your structured output to ./report.json as the skill specifies.` + validation,
		},
		{
			name: "copilot",
			h:    CopilotHarness{},
			want: `Follow the instructions in ./.github/skills/audit/SKILL.md against the repository cloned at ./src. Write your structured output to ./report.json as the skill specifies.` + validation,
		},
		{
			name: "opencode",
			h:    OpencodeHarness{},
			want: `Follow the instructions in ./.opencode/skill/audit/SKILL.md against the repository cloned at ./src. Write your structured output to ./report.json as the skill specifies.` + validation,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.h.Prompt(job); got != test.want {
				t.Errorf("Prompt() = %q, want %q", got, test.want)
			}
		})
	}

	// A caller-supplied ValidationHint replaces the generic default so a
	// caller with its own validation endpoint can steer the agent there.
	custom := job
	custom.ValidationHint = "POST it to http://host/validate; don't install a schema validator."
	got := ClaudeHarness{}.Prompt(custom)
	if !strings.HasSuffix(got, " "+custom.ValidationHint) {
		t.Errorf("Prompt() with ValidationHint = %q, want suffix %q", got, custom.ValidationHint)
	}
	if strings.Contains(got, "before finishing") {
		t.Errorf("Prompt() with ValidationHint still contains the default: %q", got)
	}
}

func TestPromptSourceDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		h    Harness
		job  Job
		want string
	}{
		{
			name: "workspace root",
			h:    ClaudeHarness{},
			job:  Job{SkillName: "audit", SrcDir: "."},
			want: `Use the "audit" skill on the repository cloned at the workspace root.`,
		},
		{
			name: "nested checkout",
			h:    CodexHarness{},
			job:  Job{SkillName: "audit", SrcDir: "checkouts/repo"},
			want: `Follow the instructions in ./skills/audit/SKILL.md against the repository cloned at ./checkouts/repo.`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.h.Prompt(test.job); got != test.want {
				t.Errorf("Prompt() = %q, want %q", got, test.want)
			}
		})
	}
}

type promptTransportHarness struct {
	viaArgs bool
	binary  string
	guide   string
}

func (h promptTransportHarness) Binary() string                   { return h.binary }
func (promptTransportHarness) Args(Job) []string                  { return nil }
func (promptTransportHarness) Prompt(Job) string                  { return "" }
func (promptTransportHarness) ParseStream(io.Reader, func(Event)) {}
func (promptTransportHarness) SkillDir(string, string) string     { return "" }
func (h promptTransportHarness) GuideFilename() string            { return h.guide }
func (h promptTransportHarness) SystemPromptViaArgs() bool        { return h.viaArgs }
func (promptTransportHarness) EgressHosts() []string              { return nil }
func (promptTransportHarness) Env(string) []string                { return nil }
func (promptTransportHarness) StateEnv(string) []string           { return nil }
func (promptTransportHarness) AccountErrorText(string) string     { return "" }
func (promptTransportHarness) DefaultModels() []ModelDefault      { return nil }

func TestWriteSystemPromptUsesBackendCapability(t *testing.T) {
	t.Parallel()

	t.Run("binary named claude can use a guide", func(t *testing.T) {
		t.Parallel()
		workspace := t.TempDir()
		h := promptTransportHarness{binary: "claude", guide: "CUSTOM.md"}
		if err := WriteSystemPrompt(h, Job{Workspace: workspace, SystemPrompt: "Use the guide."}); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filepath.Join(workspace, "CUSTOM.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "Use the guide.\n" {
			t.Errorf("guide = %q", content)
		}
	})

	t.Run("unregistered variant can pass prompt in args", func(t *testing.T) {
		t.Parallel()
		workspace := t.TempDir()
		h := promptTransportHarness{
			viaArgs: true,
			binary:  "claude-variant",
			guide:   "SHOULD-NOT-EXIST.md",
		}
		if err := WriteSystemPrompt(h, Job{Workspace: workspace, SystemPrompt: "Use argv."}); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(filepath.Join(workspace, "SHOULD-NOT-EXIST.md"))
		if !os.IsNotExist(err) {
			t.Fatalf("guide stat error = %v, want not found", err)
		}
	})
}
