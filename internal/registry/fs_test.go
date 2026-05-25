package registry

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seed builds a tiny filesystem registry layout for tests:
//
//   <root>/index.json
//   <root>/modules/acme.email/module.json
//   <root>/modules/acme.email/0.3.4/{checksums.txt, linux-amd64.tar.gz}
//
// Returns the root path. Tarball payload is irrelevant — we just verify
// that FetchBinary streams whatever's there.
func seed(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("index.json", `{
  "schema": "0.1",
  "modules": [
    {"id":"acme.email","name":"Email","latest":"0.3.4"}
  ]
}`)
	mustWrite("modules/acme.email/module.json", `{
  "id":"acme.email","name":"Email","description":"d","versions":[
    {
      "version":"0.3.4",
      "manifest_url":"/module/acme.email/0.3.4/manifest.toml",
      "checksums_url":"/module/acme.email/0.3.4/checksums.txt",
      "binaries":[
        {"platform":"linux/amd64","url":"/.../linux-amd64.tar.gz","sha256":"DEADBEEF"}
      ],
      "dependencies":{"acme.oauth":"^1.0"},
      "capabilities":["network"]
    },
    {
      "version":"0.3.3",
      "yanked": true,
      "manifest_url":"/module/acme.email/0.3.3/manifest.toml",
      "checksums_url":"/module/acme.email/0.3.3/checksums.txt",
      "binaries":[]
    }
  ]
}`)
	mustWrite("modules/acme.email/0.3.4/checksums.txt",
		"DEADBEEF linux-amd64.tar.gz\n# comment line\nABC123 darwin-arm64.tar.gz\n")
	mustWrite("modules/acme.email/0.3.4/linux-amd64.tar.gz", "fake binary payload")
	return root
}

func TestFS_Index(t *testing.T) {
	c := NewFS(seed(t))
	idx, err := c.Index()
	if err != nil {
		t.Fatal(err)
	}
	if idx.Schema != "0.1" {
		t.Fatalf("schema: %q", idx.Schema)
	}
	if len(idx.Modules) != 1 || idx.Modules[0].ID != "acme.email" {
		t.Fatalf("modules: %+v", idx.Modules)
	}
}

func TestFS_Module(t *testing.T) {
	c := NewFS(seed(t))
	m, err := c.Module("acme.email")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(m.Versions))
	}
	if m.Versions[0].Dependencies["acme.oauth"] != "^1.0" {
		t.Fatalf("deps: %+v", m.Versions[0].Dependencies)
	}
}

func TestFS_ModuleNotFound(t *testing.T) {
	c := NewFS(seed(t))
	if _, err := c.Module("does.not.exist"); err == nil {
		t.Fatal("expected ErrNotFound")
	}
}

func TestFS_Checksums(t *testing.T) {
	c := NewFS(seed(t))
	cs, err := c.Checksums("acme.email", "0.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if cs.Entries["linux-amd64.tar.gz"] != "DEADBEEF" {
		t.Fatalf("checksums: %+v", cs.Entries)
	}
	if cs.Entries["darwin-arm64.tar.gz"] != "ABC123" {
		t.Fatalf("checksums: %+v", cs.Entries)
	}
}

func TestFS_FetchBinary(t *testing.T) {
	c := NewFS(seed(t))
	rc, err := c.FetchBinary("acme.email", "0.3.4", "linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fake binary") {
		t.Fatalf("payload: %q", string(data))
	}
}

func TestFS_FetchBinaryMissing(t *testing.T) {
	c := NewFS(seed(t))
	if _, err := c.FetchBinary("acme.email", "0.3.4", "windows/amd64"); err == nil {
		t.Fatal("expected ErrNotFound")
	}
}

func TestIndexFromClient_FiltersYanked(t *testing.T) {
	c := NewFS(seed(t))
	adapter := IndexFromClient{C: c}
	versions, err := adapter.Versions("acme.email")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Version != "0.3.4" {
		t.Fatalf("expected only non-yanked 0.3.4, got %+v", versions)
	}
}
