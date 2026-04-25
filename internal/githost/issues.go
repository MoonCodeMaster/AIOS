package githost

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ghIssueJSON matches the subset of `gh issue list/view --json` output we use.
type ghIssueJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (h *CLIHost) ListLabeled(ctx context.Context, label string) ([]Issue, error) {
	cmd := h.cmd(ctx, "gh", "issue", "list",
		"--label", label,
		"--state", "open",
		"--json", "number,title,body,url,labels",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	var raw []ghIssueJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh issue list: parse json: %w", err)
	}
	out2 := make([]Issue, 0, len(raw))
	for _, r := range raw {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out2 = append(out2, Issue{
			Number: r.Number,
			Title:  r.Title,
			Body:   r.Body,
			URL:    r.URL,
			Labels: labels,
		})
	}
	return out2, nil
}

func (h *CLIHost) AddLabel(ctx context.Context, issueNum int, label string) error {
	cmd := h.cmd(ctx, "gh", "issue", "edit", strconv.Itoa(issueNum), "--add-label", label)
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue edit --add-label: %w", err)
	}
	return nil
}

func (h *CLIHost) RemoveLabel(ctx context.Context, issueNum int, label string) error {
	cmd := h.cmd(ctx, "gh", "issue", "edit", strconv.Itoa(issueNum), "--remove-label", label)
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue edit --remove-label: %w", err)
	}
	return nil
}

func (h *CLIHost) AddComment(ctx context.Context, issueNum int, body string) error {
	cmd := h.cmd(ctx, "gh", "issue", "comment", strconv.Itoa(issueNum), "--body", body)
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue comment: %w", err)
	}
	return nil
}

func (h *CLIHost) OpenIssue(ctx context.Context, title, body string, labels []string) (int, error) {
	args := []string{"issue", "create", "--title", title, "--body", body}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	cmd := h.cmd(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("gh issue create: %w", err)
	}
	url := strings.TrimSpace(string(out))
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return 0, fmt.Errorf("gh issue create: empty output")
	}
	num, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0, fmt.Errorf("gh issue create: cannot parse issue number from %q: %w", url, err)
	}
	return num, nil
}

func (h *CLIHost) CloseIssue(ctx context.Context, issueNum int) error {
	cmd := h.cmd(ctx, "gh", "issue", "close", strconv.Itoa(issueNum))
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue close: %w", err)
	}
	return nil
}
