package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	guideDirMode  = 0o755
	guideFileMode = 0o644
)

func explicitSkillPrompt(j Job, skillPath string) string {
	resume := j.ResumeSessionID != ""
	if resume && j.ResumePrompt != "" {
		return j.ResumePrompt
	}
	if !resume && j.Prompt != "" {
		return j.Prompt
	}
	if j.SkillName == "" {
		if resume {
			return "Continue from where you left off."
		}
		return ""
	}
	verb := "Follow"
	if resume {
		verb = "Continue following"
	}
	prompt := verb + " the instructions in " + skillPath + "/SKILL.md against the repository at ./src."
	if j.OutputFile != "" {
		prompt += " Write your structured output to ./" + j.OutputFile + " as the skill specifies."
		prompt += schemaValidationHint(j.OutputFile)
	}
	return prompt
}

func buildSkillPrompt(name, outputFile string) string {
	prompt := fmt.Sprintf("Use the %q skill on the repository at ./src.", name)
	if outputFile != "" {
		prompt += fmt.Sprintf(" Write your structured output to ./%s as the skill specifies.", outputFile)
		prompt += schemaValidationHint(outputFile)
	}
	return prompt
}

func buildResumePrompt(name, outputFile string) string {
	if name == "" {
		return "Continue from where you left off."
	}
	prompt := fmt.Sprintf("Continue the %q skill on the repository at ./src from where you left off.", name)
	if outputFile != "" {
		prompt += fmt.Sprintf(" Write your structured output to ./%s as the skill specifies.", outputFile)
		prompt += schemaValidationHint(outputFile)
	}
	return prompt
}

func schemaValidationHint(outputFile string) string {
	if !strings.HasSuffix(strings.ToLower(outputFile), ".json") {
		return ""
	}
	return fmt.Sprintf(" Validate ./%s against ./schema.json before finishing.", outputFile)
}

// WriteSystemPrompt writes j.SystemPrompt to the guide file used by h. Claude
// receives its system prompt through argv, so this function has no work for
// that backend.
func WriteSystemPrompt(h Harness, j Job) error {
	if strings.TrimSpace(j.SystemPrompt) == "" || Name(h) == "claude" {
		return nil
	}
	if j.Workspace == "" {
		return fmt.Errorf("harness: workspace is required for a system prompt")
	}
	guide := h.GuideFilename()
	if guide == "" || !filepath.IsLocal(guide) {
		return fmt.Errorf("harness: invalid guide filename %q", guide)
	}
	path := filepath.Join(j.Workspace, guide)
	if err := os.MkdirAll(filepath.Dir(path), guideDirMode); err != nil {
		return fmt.Errorf("harness: create guide directory: %w", err)
	}
	content := j.SystemPrompt
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), guideFileMode); err != nil {
		return fmt.Errorf("harness: write %s: %w", guide, err)
	}
	return nil
}
