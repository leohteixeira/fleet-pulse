package webui_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/leohteixeira/fleet-pulse/internal/webui"
)

func TestFS_HasIndexHTML(t *testing.T) {
	t.Parallel()

	body, err := fs.ReadFile(webui.FS(), "index.html")
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	if !strings.Contains(string(body), "root") {
		t.Fatalf("embedded index.html missing root mount: %q", body)
	}
}

func TestFS_StreamUsesRentalFleet(t *testing.T) {
	t.Parallel()

	found := false
	err := fs.WalkDir(webui.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") {
			return err
		}
		body, readErr := fs.ReadFile(webui.FS(), path)
		if readErr != nil {
			return readErr
		}
		text := string(body)
		if strings.Contains(text, "/api/stream?fleet=") && strings.Contains(text, "rental") {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded ui: %v", err)
	}
	if !found {
		t.Fatal("embedded JS missing /api/stream?fleet=rental")
	}
}
