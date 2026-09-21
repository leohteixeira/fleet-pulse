package httpapi_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenAPI_DocumentedPaths(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	text := string(body)
	for _, path := range []string{
		"/api/clock:",
		"/api/leasing/vehicles:",
		"/api/contracts:",
		"/api/contracts/{id}:",
		"/api/contracts/{id}/notify:",
		"/api/contracts/{id}/block:",
		"/api/contracts/{id}/block/cancel:",
		"/api/contracts/{id}/payments:",
		"/api/vehicles:",
		"/api/stream:",
		"/api/vehicles/{vin}/unlock:",
		"/api/vehicles/{vin}/lock:",
		"/api/commands/{id}:",
		"/healthz:",
	} {
		if !strings.Contains(text, path) {
			t.Fatalf("openapi.yaml missing path %s", path)
		}
	}
}
