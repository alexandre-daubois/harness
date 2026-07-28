// Package skills parses, filters, and stages agent skill directories.
package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	skillFilename = "SKILL.md"
	maxNameLen    = 64
	maxDescLen    = 1024
	maxCompatLen  = 500
)

var (
	frontmatterPattern = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?(.*)\z`)
	namePattern        = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Skill is one parsed markdown instruction file and its source directory.
type Skill struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	Metadata      map[string]any
	Body          string
	SourcePath    string
	SourceHash    string
	Warnings      []string
}

type frontmatter struct {
	Name          string         `yaml:"name,omitempty"`
	Description   string         `yaml:"description,omitempty"`
	License       string         `yaml:"license,omitempty"`
	Compatibility string         `yaml:"compatibility,omitempty"`
	AllowedTools  string         `yaml:"allowed-tools,omitempty"`
	Metadata      map[string]any `yaml:"metadata,omitempty"`
}

// Parse reads a markdown instruction file. YAML frontmatter is optional.
func Parse(path string) (*Skill, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skills: read %s: %w", path, err)
	}
	abs, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("skills: resolve %s: %w", path, err)
	}
	fm, body, hasFrontmatter := splitFrontmatter(raw)
	parsed := frontmatter{Metadata: make(map[string]any)}
	if hasFrontmatter {
		if err := yaml.Unmarshal(fm, &parsed); err != nil {
			return nil, fmt.Errorf("skills: yaml %s: %w", path, err)
		}
		if parsed.Metadata == nil {
			parsed.Metadata = make(map[string]any)
		}
	}
	skill := &Skill{
		Name:          strings.TrimSpace(parsed.Name),
		Description:   strings.TrimSpace(parsed.Description),
		License:       strings.TrimSpace(parsed.License),
		Compatibility: strings.TrimSpace(parsed.Compatibility),
		AllowedTools:  strings.TrimSpace(parsed.AllowedTools),
		Metadata:      parsed.Metadata,
		Body:          strings.TrimSpace(body),
		SourcePath:    abs,
		SourceHash:    hash(raw),
	}
	if !hasFrontmatter {
		skill.Body = strings.TrimSpace(string(raw))
	}
	skill.validate(path, hasFrontmatter)
	return skill, nil
}

func splitFrontmatter(raw []byte) ([]byte, string, bool) {
	match := frontmatterPattern.FindSubmatch(raw)
	if match == nil {
		return nil, string(raw), false
	}
	return match[1], string(match[2]), true
}

func hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Skill) validate(path string, hadFrontmatter bool) {
	if s.Name == "" {
		s.Name = inferredName(path)
		if hadFrontmatter {
			s.Warnings = append(s.Warnings, "name missing, using path name")
		}
	}
	if hadFrontmatter && s.Description == "" {
		s.Warnings = append(s.Warnings, "description is missing")
	}
	if len(s.Name) > maxNameLen {
		s.Warnings = append(s.Warnings, fmt.Sprintf("name %q exceeds %d characters", s.Name, maxNameLen))
	}
	if !namePattern.MatchString(s.Name) {
		s.Warnings = append(s.Warnings,
			fmt.Sprintf("name %q is not spec-conformant (lowercase, digits, hyphens only)", s.Name))
	}
	if len(s.Description) > maxDescLen {
		s.Warnings = append(s.Warnings, fmt.Sprintf("description exceeds %d characters", maxDescLen))
	}
	if len(s.Compatibility) > maxCompatLen {
		s.Warnings = append(s.Warnings, fmt.Sprintf("compatibility exceeds %d characters", maxCompatLen))
	}
}

func inferredName(path string) string {
	base := filepath.Base(path)
	if strings.EqualFold(base, skillFilename) {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// ValidateNamespace rejects metadata keys in prefix that are not listed in
// allowed. Keys outside the namespace are ignored.
func ValidateNamespace(meta map[string]any, prefix string, allowed map[string]bool) error {
	for key := range meta {
		if strings.HasPrefix(key, prefix) && !allowed[key] {
			return fmt.Errorf("skills: unknown metadata key %q", key)
		}
	}
	return nil
}

func render(skill *Skill) ([]byte, error) {
	if skill == nil {
		return nil, fmt.Errorf("skills: skill is required")
	}
	if skill.Description == "" && skill.License == "" && skill.Compatibility == "" &&
		skill.AllowedTools == "" && len(skill.Metadata) == 0 {
		return withTrailingNewline(skill.Body), nil
	}
	fm, err := yaml.Marshal(frontmatter{
		Name:          skill.Name,
		Description:   skill.Description,
		License:       skill.License,
		Compatibility: skill.Compatibility,
		AllowedTools:  skill.AllowedTools,
		Metadata:      skill.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("skills: marshal frontmatter: %w", err)
	}
	var output strings.Builder
	output.WriteString("---\n")
	output.Write(fm)
	output.WriteString("---\n\n")
	output.WriteString(skill.Body)
	return withTrailingNewline(output.String()), nil
}

func withTrailingNewline(s string) []byte {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(s)
}
