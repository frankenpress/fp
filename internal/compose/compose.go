// Package compose contains the project + service lookup logic the
// snapshot orchestrator uses before shelling into a container.
//
// "Project name" in docker-compose terms is the prefix it puts on
// every resource (containers, volumes, networks). v2's default is
// basename(working-dir), so the same default works for fp when no
// explicit override is configured.
package compose

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/frankenpress/fp/internal/docker"
)

// DefaultProject returns the project name docker-compose itself would
// pick by default: basename(repoRoot), lowercased. Callers should
// prefer an explicit [snapshot].project value when one is configured.
func DefaultProject(repoRoot string) string {
	base := filepath.Base(repoRoot)
	return sanitizeProjectName(base)
}

// sanitizeProjectName mirrors compose's own normalisation: lowercase
// and only [a-z0-9_-] retained.
func sanitizeProjectName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			out = append(out, c)
		}
	}
	return string(out)
}

// ServiceStatus describes whether the named compose service has a
// container, and if so whether it is currently running.
type ServiceStatus int

const (
	StatusUnknown ServiceStatus = iota
	StatusProjectMissing
	StatusServiceMissing
	StatusServiceStopped
	StatusServiceRunning
)

// Check inspects the compose project for the given service. It maps
// directly onto the plan's Error-UX (a) hint hierarchy: project
// missing → "no docker-compose project named X is running",
// service missing → "is this the right directory?", service stopped
// → "run make up first".
func Check(ctx context.Context, r docker.Runner, project, service string) (ServiceStatus, *docker.Container, error) {
	containers, err := r.PS(ctx, project)
	if err != nil {
		return StatusUnknown, nil, err
	}
	if len(containers) == 0 {
		return StatusProjectMissing, nil, nil
	}
	for i := range containers {
		if containers[i].Service == service {
			c := containers[i]
			if isRunning(c.State) {
				return StatusServiceRunning, &c, nil
			}
			return StatusServiceStopped, &c, nil
		}
	}
	return StatusServiceMissing, nil, nil
}

func isRunning(state string) bool {
	switch state {
	case "running", "Running":
		return true
	}
	return false
}

// FormatNotRunning renders a friendly message for any non-running
// status. Callers (Error-UX (a) in snapshot.go) use this to
// produce the designer-facing error text.
func FormatNotRunning(status ServiceStatus, project, service string) string {
	switch status {
	case StatusProjectMissing:
		return fmt.Sprintf(
			"no docker-compose project named %q is running. is this the right directory?",
			project,
		)
	case StatusServiceMissing:
		return fmt.Sprintf(
			"docker-compose project %q has no %q service; check [snapshot].service in frankenpress.toml",
			project, service,
		)
	case StatusServiceStopped:
		return fmt.Sprintf(
			"docker-compose project %q has no running %q container.\nhint: run \"make up\" from the repo root, then retry.",
			project, service,
		)
	default:
		return fmt.Sprintf("docker-compose project %q is not in a runnable state", project)
	}
}
