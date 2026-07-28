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
	defaultSrcDir = "src"
)

// explicitSkillPrompt is the activation prompt for backends that discover a
// SKILL.md but do not have a native skill invocation. An explicit resume
// prompt is returned unchanged so callers can send a targeted repair nudge.
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
	prompt := verb + " the instructions in " + skillPath + "/SKILL.md against the repository cloned at " + sourcePromptPath(j) + "."
	if j.OutputFile != "" {
		prompt += " Write your structured output to ./" + j.OutputFile + " as the skill specifies."
		prompt += schemaValidationHint(j.OutputFile)
	}
	return prompt
}

// buildSkillPrompt is Claude's activation prompt. SKILL.md holds the actual
// instructions, so the prompt only selects it and locates the repository.
func buildSkillPrompt(j Job) string {
	prompt := fmt.Sprintf("Use the %q skill on the repository cloned at %s.", j.SkillName, sourcePromptPath(j))
	if j.OutputFile != "" {
		prompt += fmt.Sprintf(" Write your structured output to ./%s as the skill specifies.", j.OutputFile)
		prompt += schemaValidationHint(j.OutputFile)
	}
	return prompt
}

// buildResumePrompt tells a resumed agent to continue and restates the output
// file, which is easy to lose among the earlier conversation turns.
func buildResumePrompt(j Job) string {
	if j.SkillName == "" {
		return "Continue from where you left off."
	}
	prompt := fmt.Sprintf(
		"Continue the %q skill on the repository at %s from where you left off.",
		j.SkillName,
		sourcePromptPath(j),
	)
	if j.OutputFile != "" {
		prompt += fmt.Sprintf(" Write your structured output to ./%s as the skill specifies.", j.OutputFile)
		prompt += schemaValidationHint(j.OutputFile)
	}
	return prompt
}

// sourcePromptPath keeps the historical ./src layout when SrcDir is empty.
// Callers whose workspace is already the repository root can set SrcDir to ".".
func sourcePromptPath(j Job) string {
	dir := strings.TrimSpace(j.SrcDir)
	if dir == "" {
		dir = defaultSrcDir
	}
	dir = filepath.ToSlash(filepath.Clean(dir))
	if dir == "." {
		return "the workspace root"
	}
	return "./" + strings.TrimPrefix(dir, "./")
}

// schemaValidationHint tells the agent to use the caller's validation endpoint
// instead of installing a JSON Schema library inside a restricted runner. The
// endpoint uses the schema staged with the skill, so its result matches the
// caller's post-run validation. Non-JSON outputs do not need the instruction.
func schemaValidationHint(outputFile string) string {
	if !strings.HasSuffix(outputFile, ".json") {
		return ""
	}
	return fmt.Sprintf(
		" To check ./%s against ./schema.json, POST it to {scrutineer.api_base}/scans/{scrutineer.scan_id}/validate-report (header \"Authorization: Bearer {scrutineer.token}\", values in ./context.json); {\"valid\":true} means it conforms. Don't install a schema validator.",
		outputFile,
	)
}

// WriteSystemPrompt writes j.SystemPrompt to the guide file used by h. A
// backend that passes the system prompt in Args has no file to write.
func WriteSystemPrompt(h Harness, j Job) error {
	if strings.TrimSpace(j.SystemPrompt) == "" {
		return nil
	}
	if h == nil {
		return fmt.Errorf("harness: backend is required for a system prompt")
	}
	if h.SystemPromptViaArgs() {
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
