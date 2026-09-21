package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoFile(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func TestCompose_ProjectAndPublishedPorts(t *testing.T) {
	t.Parallel()

	body := repoFile(t, "compose.yaml")
	if !strings.Contains(body, "name: fleet-pulse") {
		t.Fatal("compose.yaml missing project name fleet-pulse")
	}
	if strings.Contains(body, "1883") {
		t.Fatal("compose.yaml must not publish or mention MQTT 1883")
	}
	if !strings.Contains(body, "3300:3300") {
		t.Fatal("compose.yaml must publish Caddy on 3300")
	}
	if !strings.Contains(body, "5435:5432") {
		t.Fatal("compose.yaml must publish Postgres on 5435")
	}
	if strings.Contains(body, "8300:8300") {
		t.Fatal("compose.yaml must not publish app HTTP 8300")
	}
}

func TestCaddyfile_SameOriginSPAAndAPI(t *testing.T) {
	t.Parallel()

	body := repoFile(t, "Caddyfile")
	for _, needle := range []string{
		":3300",
		"/api",
		"/healthz",
		"/",
		"/carteira",
		"/carteira/*",
		"reverse_proxy app:8300",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("Caddyfile missing %q", needle)
		}
	}
}
