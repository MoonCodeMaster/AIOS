package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type Task struct {
	ID            string              `yaml:"id"`
	Kind          string              `yaml:"kind"`
	DependsOn     []string            `yaml:"depends_on"`
	Status        string              `yaml:"status"`
	Acceptance    []string            `yaml:"acceptance"`
	MCPAllow      []string            `yaml:"mcp_allow"`
	MCPAllowTools map[string][]string `yaml:"mcp_allow_tools"`
	// M2 — decomposition lineage. Depth=0 for original tasks; sub-tasks of a
	// decomposed parent inherit Depth=parent.Depth+1. ParentID points at the
	// task that decomposed into this one. DecomposedInto is populated on the
	// PARENT task, listing the IDs of the sub-tasks it was split into.
	Depth          int      `yaml:"depth"`
	ParentID       string   `yaml:"parent_id"`
	DecomposedInto []string `yaml:"decomposed_into"`
	Body           string   `yaml:"-"`
	Path           string   `yaml:"-"`
}

// ParseTask parses a single task markdown file.
func ParseTask(src string) (*Task, error) {
	fm, body, err := splitFrontmatter(src)
	if err != nil {
		return nil, err
	}
	var t Task
	if err := yaml.Unmarshal([]byte(fm), &t); err != nil {
		return nil, fmt.Errorf("parse task frontmatter: %w", err)
	}
	if t.ID == "" {
		return nil, errors.New("task missing 'id'")
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	if len(t.Acceptance) == 0 {
		return nil, fmt.Errorf("task %s has no acceptance criteria", t.ID)
	}
	t.Body = body
	return &t, nil
}

// LoadTasks reads every .md file in dir, returns them sorted by filename.
func LoadTasks(dir string) ([]*Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read tasks dir: %w", err)
	}
	var out []*Task
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		t, err := ParseTask(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		t.Path = p
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
