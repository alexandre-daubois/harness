package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alpha-omega-security/harness"
)

func TestStage(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, skillFilename), "---\nname: audit\ndescription: Audit\n---\nOld body\n")
	writeFile(t, filepath.Join(source, "scripts", "check.sh"), "#!/bin/sh\n")
	workspace := t.TempDir()
	skill := &Skill{
		Name:        "audit",
		Description: "Audit",
		Body:        "New body",
		SourcePath:  source,
		Metadata:    map[string]any{},
	}
	job := harness.Job{Workspace: workspace, SkillName: "audit"}
	if err := Stage(harness.ClaudeHarness{}, job, skill); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(workspace, ".claude", "skills", "audit")
	content, err := os.ReadFile(filepath.Join(destination, skillFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "New body") || strings.Contains(string(content), "Old body") {
		t.Errorf("SKILL.md = %q", content)
	}
	if _, err := os.Stat(filepath.Join(destination, "scripts", "check.sh")); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(destination, "stale.txt"), "stale")
	if err := Stage(harness.ClaudeHarness{}, job, skill); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale file survived, error = %v", err)
	}
}

func TestStageRejectsUnsafeName(t *testing.T) {
	t.Parallel()
	err := Stage(harness.ClaudeHarness{}, harness.Job{
		Workspace: t.TempDir(),
		SkillName: "../outside",
	}, &Skill{Name: "audit", Body: "body"})
	if err == nil {
		t.Fatal("Stage accepted an unsafe name")
	}
}

func TestConcat(t *testing.T) {
	t.Parallel()
	got := Concat(
		&Skill{Body: "First"},
		nil,
		&Skill{Body: "Second"},
	)
	if got != "First\n\n---\n\nSecond" {
		t.Errorf("Concat() = %q", got)
	}
}
