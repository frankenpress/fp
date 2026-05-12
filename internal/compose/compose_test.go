package compose

import (
	"context"
	"strings"
	"testing"

	"github.com/frankenpress/fp/internal/docker"
)

func TestDefaultProject(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/Users/x/Developer/EightOEight/sts", "sts"},
		{"/Users/x/Some Folder", "somefolder"},
		{"/a/STS", "sts"},
	}
	for _, tc := range cases {
		got := DefaultProject(tc.in)
		if got != tc.want {
			t.Errorf("DefaultProject(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCheck_StatusMapping(t *testing.T) {
	cases := []struct {
		name       string
		containers []docker.Container
		want       ServiceStatus
	}{
		{"empty project", nil, StatusProjectMissing},
		{"service missing", []docker.Container{{Service: "db", State: "running"}}, StatusServiceMissing},
		{"service stopped", []docker.Container{{Service: "site", State: "exited"}}, StatusServiceStopped},
		{"service running", []docker.Container{{Service: "site", State: "running"}}, StatusServiceRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := docker.NewFake()
			r.PSContainers = tc.containers
			got, _, err := Check(context.Background(), r, "myproj", "site")
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if got != tc.want {
				t.Errorf("Check status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestFormatNotRunning(t *testing.T) {
	cases := []struct {
		status ServiceStatus
		want   string
	}{
		{StatusProjectMissing, "no docker-compose project"},
		{StatusServiceMissing, "no \"site\" service"},
		{StatusServiceStopped, "make up"},
	}
	for _, tc := range cases {
		got := FormatNotRunning(tc.status, "myproj", "site")
		if !strings.Contains(got, tc.want) {
			t.Errorf("FormatNotRunning(%d) = %q, want substring %q", tc.status, got, tc.want)
		}
	}
}
