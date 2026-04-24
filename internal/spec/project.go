package spec

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type Project struct {
	Name          string   `yaml:"name"`
	Goal          string   `yaml:"goal"`
	NonGoals      []string `yaml:"non_goals"`
	Constraints   []string `yaml:"constraints"`
	AcceptanceBar []string `yaml:"acceptance_bar"`
	Body          string   `yaml:"-"`
}

// ParseProject reads a markdown document with YAML frontmatter.
// Frontmatter is delimited by lines of exactly "---".
func ParseProject(src string) (*Project, error) {
	fm, body, err := splitFrontmatter(src)
	if err != nil {
		return nil, err
	}
	var p Project
	if err := yaml.Unmarshal([]byte(fm), &p); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	if p.Goal == "" {
		return nil, errors.New("project.md frontmatter missing 'goal'")
	}
	if len(p.AcceptanceBar) == 0 {
		return nil, errors.New("project.md frontmatter missing 'acceptance_bar'")
	}
	p.Body = body
	return &p, nil
}

func splitFrontmatter(src string) (frontmatter, body string, err error) {
	lines := strings.Split(src, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", errors.New("no frontmatter: expected leading '---'")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", "", errors.New("no frontmatter close: missing trailing '---'")
	}
	return strings.Join(lines[1:end], "\n"), strings.Join(lines[end+1:], "\n"), nil
}
