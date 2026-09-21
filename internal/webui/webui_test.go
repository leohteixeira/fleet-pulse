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
