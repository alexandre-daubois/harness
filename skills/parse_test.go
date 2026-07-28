package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, skillFilename)
	writeFile(t, path, `---
name: audit
description: Check a repository
license: MIT
allowed-tools: Read,Grep
metadata:
  example.level: strict
---

Read the repository.
`)

	skill, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "audit" || skill.Description != "Check a repository" {
		t.Errorf("skill = %+v", skill)
	}
	if skill.Metadata["example.level"] != "strict" {
		t.Errorf("metadata = %#v", skill.Metadata)
	}
	if skill.Body != "Read the repository." {
		t.Errorf("body = %q", skill.Body)
	}
	if skill.SourceHash == "" {
		t.Fatal("source hash is empty")
	}
}

func TestParsePlainMarkdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.md")
	writeFile(t, path, "# Review\n\nCheck the code.\n")

	skill, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "review" {
		t.Errorf("name = %q", skill.Name)
	}
	if len(skill.Metadata) != 0 {
		t.Errorf("metadata = %#v", skill.Metadata)
	}
	if skill.Body != "# Review\n\nCheck the code." {
		t.Errorf("body = %q", skill.Body)
	}
	if len(skill.Warnings) != 0 {
		t.Errorf("warnings = %v", skill.Warnings)
	}
}

func TestParseSourceHashIncludesSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, skillFilename)
	writeFile(t, path, "---\nname: audit\ndescription: Audit\n---\nCheck it.\n")

	withoutSchema, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, schemaFilename), `{"type":"object"}`)
	withSchema, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if withSchema.SourceHash == withoutSchema.SourceHash {
		t.Fatal("SourceHash did not change when schema.json was added")
	}
	if withSchema.SchemaJSON != `{"type":"object"}` {
		t.Errorf("SchemaJSON = %q", withSchema.SchemaJSON)
	}

	writeFile(t, filepath.Join(dir, schemaFilename), `{"type":"array"}`)
	changedSchema, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if changedSchema.SourceHash == withSchema.SourceHash {
		t.Fatal("SourceHash did not change after a schema-only edit")
	}
}

func TestRender(t *testing.T) {
	t.Parallel()

	rendered, err := Render(&Skill{
		Name:        "audit",
		Description: "Check a repository",
		Body:        "Read the code.",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nname: audit\ndescription: Check a repository\n---\n\nRead the code.\n"
	if string(rendered) != want {
		t.Errorf("Render() = %q, want %q", rendered, want)
	}
}

func TestParseWarnsOnSpecFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, skillFilename)
	writeFile(t, path, "---\nname: Bad_Name\n---\nbody\n")

	skill, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(skill.Warnings) < 2 {
		t.Errorf("warnings = %v", skill.Warnings)
	}
}

func TestParseRejectsInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), skillFilename)
	writeFile(t, path, "---\nname: [\n---\nbody\n")
	if _, err := Parse(path); err == nil {
		t.Fatal("Parse accepted invalid YAML")
	}
}

func TestValidateNamespace(t *testing.T) {
	t.Parallel()

	meta := map[string]any{"app.good": true, "other.key": true}
	allowed := map[string]bool{"app.good": true}
	if err := ValidateNamespace(meta, "app.", allowed); err != nil {
		t.Fatal(err)
	}
	meta["app.typo"] = true
	if err := ValidateNamespace(meta, "app.", allowed); err == nil {
		t.Fatal("ValidateNamespace accepted an unknown key")
	}
}

func TestWalk(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "one", skillFilename), "---\nname: one\ndescription: One\n---\none\n")
	writeFile(t, filepath.Join(root, "two", skillFilename), "---\nname: two\ndescription: Two\n---\ntwo\n")
	writeFile(t, filepath.Join(root, ".git", "hidden", skillFilename), "hidden\n")

	var names []string
	err := Walk(root, func(skill *Skill) error {
		names = append(names, skill.Name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "one,two" {
		t.Errorf("walked %v", names)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
