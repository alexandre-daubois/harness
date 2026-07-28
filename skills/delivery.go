package skills

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alpha-omega-security/harness"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// Stage writes a skill and its sibling files to the selected backend's skill
// discovery directory.
func Stage(h harness.Harness, job harness.Job, skill *Skill) error {
	if h == nil {
		return fmt.Errorf("skills: harness is required")
	}
	if skill == nil {
		return fmt.Errorf("skills: skill is required")
	}
	name := job.SkillName
	if name == "" {
		name = skill.Name
	}
	if !filepath.IsLocal(name) || filepath.Base(name) != name {
		return fmt.Errorf("skills: skill name %q contains path separators", name)
	}
	if job.Workspace == "" {
		return fmt.Errorf("skills: workspace is required")
	}
	destination := h.SkillDir(job.Workspace, name)
	if destination == "" {
		return fmt.Errorf("skills: %s does not support staged skills", harness.Name(h))
	}
	if err := validateDestination(job.Workspace, destination); err != nil {
		return err
	}
	if err := os.RemoveAll(destination); err != nil {
		return fmt.Errorf("skills: clear destination: %w", err)
	}
	if err := os.MkdirAll(destination, dirPerm); err != nil {
		return fmt.Errorf("skills: create destination: %w", err)
	}
	if skill.SourcePath != "" {
		if err := copySiblings(skill.SourcePath, destination); err != nil {
			return fmt.Errorf("skills: copy siblings: %w", err)
		}
	}
	content, err := Render(skill)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(destination, skillFilename), content, filePerm); err != nil {
		return fmt.Errorf("skills: write SKILL.md: %w", err)
	}
	if skill.SchemaJSON != "" {
		if err := os.WriteFile(
			filepath.Join(destination, schemaFilename),
			[]byte(skill.SchemaJSON),
			filePerm,
		); err != nil {
			return fmt.Errorf("skills: write schema.json: %w", err)
		}
	}
	return nil
}

func validateDestination(workspace, destination string) error {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("skills: destination %q is outside the workspace", destination)
	}
	return nil
}

func copySiblings(source, destination string) error {
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", source)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == skillFilename || entry.Name() == ".git" {
			continue
		}
		if err := copyTree(
			filepath.Join(source, entry.Name()),
			filepath.Join(destination, entry.Name()),
		); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	switch {
	case info.IsDir():
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(
				filepath.Join(source, entry.Name()),
				filepath.Join(destination, entry.Name()),
			); err != nil {
				return err
			}
		}
		return nil
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(target, destination)
	case info.Mode().IsRegular():
		return copyFile(source, destination, info.Mode().Perm())
	default:
		return fmt.Errorf("unsupported file type %s", source)
	}
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

// Concat joins skill bodies into one system prompt.
func Concat(skills ...*Skill) string {
	parts := make([]string, 0, len(skills))
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		if body := strings.TrimSpace(skill.Body); body != "" {
			parts = append(parts, body)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}
