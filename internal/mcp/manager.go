package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

type Manager struct {
	servers map[string]config.MCPServer
	mu      sync.Mutex
	procs   map[string]*exec.Cmd // started server name -> proc
}

func NewManager(servers map[string]config.MCPServer) *Manager {
	return &Manager{servers: servers, procs: map[string]*exec.Cmd{}}
}

// ScopeFor renders a per-task MCP config file under outDir/<task-id>/, lazily
// starting any servers the task allows that aren't already running.
//
// Returns (nil, nil) when the task opts out of MCP (empty mcp_allow).
func (m *Manager) ScopeFor(tk *spec.Task, outDir string) (*engine.McpScope, error) {
	if len(tk.MCPAllow) == 0 {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range tk.MCPAllow {
		srv, ok := m.servers[name]
		if !ok {
			return nil, fmt.Errorf("task %s mcp_allow references unknown server %q", tk.ID, name)
		}
		if _, running := m.procs[name]; running {
			continue
		}
		if err := m.spawn(name, srv); err != nil {
			return nil, fmt.Errorf("spawn %s: %w", name, err)
		}
	}
	return RenderScope(m.servers, tk, outDir)
}

func (m *Manager) spawn(name string, srv config.MCPServer) error {
	cmd := exec.Command(srv.Binary, srv.Args...)
	if len(srv.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range srv.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	m.procs[name] = cmd
	return nil
}

// RunningCount returns how many MCP server procs are currently alive.
func (m *Manager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.procs)
}

// Shutdown sends SIGTERM to all running servers, waits up to ctx deadline,
// then SIGKILLs stragglers. Safe to call multiple times.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	procs := m.procs
	m.procs = map[string]*exec.Cmd{}
	m.mu.Unlock()

	for name, cmd := range procs {
		if cmd.Process == nil {
			continue
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = name
	}
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	for _, cmd := range procs {
		if cmd.Process == nil {
			continue
		}
		done := make(chan struct{})
		go func(c *exec.Cmd) {
			_, _ = c.Process.Wait()
			close(done)
		}(cmd)
		remaining := time.Until(deadline)
		if remaining < 0 {
			remaining = 0
		}
		select {
		case <-done:
		case <-time.After(remaining):
			_ = cmd.Process.Signal(syscall.SIGKILL)
			<-done
		}
	}
	return nil
}
