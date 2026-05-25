package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sectersion/hackermode/internal/manifest"
	"github.com/sectersion/hackermode/internal/registry"
)

// makeTarGz packages a single file into a gzip+tar tarball and returns
// the bytes.
func makeTarGz(t *testing.T, name, content string, mode int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:     name,
		Size:     int64(len(content)),
		Mode:     mode,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// seedRegistry builds a complete filesystem registry layout with one
// module (acme.echo @ 0.1.0) for "linux/amd64". Returns the root path.
func seedRegistry(t *testing.T, dataDir string) string {
	t.Helper()
	root := filepath.Join(dataDir, "registry")

	tarball := makeTarGz(t, "echo-bin", "#!/bin/sh\necho hi\n", 0o755)
	hash := sha256Hex(tarball)

	mustWrite := func(rel string, body []byte) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("index.json", []byte(`{
  "schema": "0.1",
  "modules": [{"id":"acme.echo","name":"Echo","latest":"0.1.0"}]
}`))

	moduleJSON, _ := json.Marshal(registry.Module{
		ID: "acme.echo", Name: "Echo", Description: "test",
		Versions: []registry.ModuleVersion{{
			Version:      "0.1.0",
			ManifestURL:  "/module/acme.echo/0.1.0/manifest.toml",
			ChecksumsURL: "/module/acme.echo/0.1.0/checksums.txt",
			Binaries: []registry.BinaryEntry{
				{Platform: "linux/amd64", URL: "/x", SHA256: hash},
			},
			Capabilities: []string{},
		}},
	})
	mustWrite("modules/acme.echo/module.json", moduleJSON)
	mustWrite("modules/acme.echo/0.1.0/checksums.txt",
		[]byte(fmt.Sprintf("%s linux-amd64.tar.gz\n", hash)))
	mustWrite("modules/acme.echo/0.1.0/linux-amd64.tar.gz", tarball)
	return root
}

// withFakeDataDir points $XDG_DATA_HOME at a tempdir so the installer
// doesn't write into the real ~/.local/share.
func withFakeDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	return dir
}

func TestInstall_HappyPath(t *testing.T) {
	dataHome := withFakeDataDir(t)
	regRoot := seedRegistry(t, t.TempDir())
	reg := registry.NewFS(regRoot)

	// Project manifest.
	projDir := t.TempDir()
	manifestPath := filepath.Join(projDir, manifest.FileName)
	if err := os.WriteFile(manifestPath, []byte(`
[hackermode]
version = "0.1"

[modules]
"acme.echo" = "^0.1"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.ParseFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	lock, err := Install(m, reg, Opts{Platform: "linux/amd64", Out: io_Discard{}})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	got, ok := lock.Get("acme.echo")
	if !ok {
		t.Fatal("lockfile missing acme.echo")
	}
	if got.Version != "0.1.0" {
		t.Fatalf("version: %q", got.Version)
	}

	// Verify extracted file and current symlink.
	verDir := filepath.Join(dataHome, "hackermode", "modules", "acme.echo", "0.1.0")
	if _, err := os.Stat(filepath.Join(verDir, "echo-bin")); err != nil {
		t.Fatalf("expected echo-bin in %s: %v", verDir, err)
	}
	link, err := os.Readlink(filepath.Join(dataHome, "hackermode", "modules", "acme.echo", "current"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "0.1.0" {
		t.Fatalf("symlink: %q", link)
	}

	// Lockfile written.
	if _, err := os.Stat(filepath.Join(projDir, manifest.LockFileName)); err != nil {
		t.Fatalf("lockfile not on disk: %v", err)
	}
}

func TestInstall_ChecksumMismatchAborts(t *testing.T) {
	withFakeDataDir(t)
	regRoot := seedRegistry(t, t.TempDir())
	// Tamper with the tarball after-the-fact so the hash no longer matches.
	tarballPath := filepath.Join(regRoot, "modules/acme.echo/0.1.0/linux-amd64.tar.gz")
	if err := os.WriteFile(tarballPath, []byte("tampered!"), 0o644); err != nil {
		t.Fatal(err)
	}

	projDir := t.TempDir()
	manifestPath := filepath.Join(projDir, manifest.FileName)
	_ = os.WriteFile(manifestPath, []byte(`
[modules]
"acme.echo" = "^0.1"
`), 0o644)
	m, _ := manifest.ParseFile(manifestPath)

	_, err := Install(m, registry.NewFS(regRoot), Opts{Platform: "linux/amd64", Out: io_Discard{}})
	if err == nil {
		t.Fatal("expected checksum-mismatch error")
	}
}

func TestSync_UsesLockfileHashes(t *testing.T) {
	dataHome := withFakeDataDir(t)
	regRoot := seedRegistry(t, t.TempDir())
	reg := registry.NewFS(regRoot)

	// First install to produce a lockfile.
	projDir := t.TempDir()
	manifestPath := filepath.Join(projDir, manifest.FileName)
	_ = os.WriteFile(manifestPath, []byte(`[modules]` + "\n" + `"acme.echo" = "^0.1"`), 0o644)
	m, _ := manifest.ParseFile(manifestPath)
	lock, err := Install(m, reg, Opts{Platform: "linux/amd64", Out: io_Discard{}})
	if err != nil {
		t.Fatal(err)
	}

	// Clean install dir so Sync has to do work.
	_ = os.RemoveAll(filepath.Join(dataHome, "hackermode", "modules", "acme.echo"))

	if err := Sync(lock, reg, Opts{Platform: "linux/amd64", Out: io_Discard{}}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "hackermode", "modules", "acme.echo", "0.1.0", "echo-bin")); err != nil {
		t.Fatalf("sync didn't re-extract: %v", err)
	}
}

// io_Discard avoids importing "io" just for io.Discard in -short test
// environments where the import graph is sensitive. It's a no-op writer.
type io_Discard struct{}

func (io_Discard) Write(p []byte) (int, error) { return len(p), nil }
