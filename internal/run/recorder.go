package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Recorder struct {
	root string // .aios/runs/<id>
}

func Open(runsDir, runID string) (*Recorder, error) {
	p := filepath.Join(runsDir, runID)
	if err := os.MkdirAll(p, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir run dir: %w", err)
	}
	return &Recorder{root: p}, nil
}

func (r *Recorder) Root() string { return r.root }

func (r *Recorder) WriteFile(name string, data []byte) error {
	p := filepath.Join(r.root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (r *Recorder) WriteRoundFile(taskID string, round int, name string, data []byte) error {
	p := filepath.Join(r.root, taskID, fmt.Sprintf("round-%d", round), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (r *Recorder) WriteTaskFile(taskID string, name string, data []byte) error {
	p := filepath.Join(r.root, taskID, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (r *Recorder) AppendFile(name string, data []byte) error {
	p := filepath.Join(r.root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func (r *Recorder) WriteJSON(name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return r.WriteFile(name, raw)
}

func (r *Recorder) WriteRoundJSON(taskID string, round int, name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return r.WriteRoundFile(taskID, round, name, raw)
}
