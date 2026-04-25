package cli

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

type ServeConfig struct {
	Repo        ServeRepo        `toml:"repo"`
	Labels      ServeLabels      `toml:"labels"`
	Poll        ServePoll        `toml:"poll"`
	Concurrency ServeConcurrency `toml:"concurrency"`
}

type ServeRepo struct {
	Owner string `toml:"owner"`
	Name  string `toml:"name"`
}

type ServeLabels struct {
	Do         string `toml:"do"`
	InProgress string `toml:"in_progress"`
	PROpen     string `toml:"pr_open"`
	Stuck      string `toml:"stuck"`
	Done       string `toml:"done"`
}

type ServePoll struct {
	IntervalSec int `toml:"interval_sec"`
}

type ServeConcurrency struct {
	MaxConcurrentIssues int `toml:"max_concurrent_issues"`
}

func LoadServeConfig(path string) (*ServeConfig, error) {
	cfg := &ServeConfig{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			applyServeDefaults(cfg)
			return cfg, nil
		}
		return nil, err
	}
	if _, err := toml.Decode(string(raw), cfg); err != nil {
		return nil, err
	}
	applyServeDefaults(cfg)
	return cfg, nil
}

func applyServeDefaults(c *ServeConfig) {
	if c.Labels.Do == "" {
		c.Labels.Do = "aios:do"
	}
	if c.Labels.InProgress == "" {
		c.Labels.InProgress = "aios:in-progress"
	}
	if c.Labels.PROpen == "" {
		c.Labels.PROpen = "aios:pr-open"
	}
	if c.Labels.Stuck == "" {
		c.Labels.Stuck = "aios:stuck"
	}
	if c.Labels.Done == "" {
		c.Labels.Done = "aios:done"
	}
	if c.Poll.IntervalSec == 0 {
		c.Poll.IntervalSec = 60
	}
	if c.Concurrency.MaxConcurrentIssues == 0 {
		c.Concurrency.MaxConcurrentIssues = 1
	}
}
