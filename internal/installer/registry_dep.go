// Package installer — installRegistryDep / syncRegistryDep:
//
// Fetches a tarball from the registry, verifies it against two
// independent sources of truth (the version metadata and checksums.txt),
// extracts it into the per-version directory, and points `current` at it.
//
// Two-hash check: we compute the SHA256 of the streamed bytes once and
// require BOTH metadata.SHA256 AND checksums[filename] to match. A
// compromised metadata file alone cannot substitute a malicious tarball
// — the attacker would need to compromise both.
package installer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	hlog "github.com/sectersion/hackermode/internal/log"
	"github.com/sectersion/hackermode/internal/manifest"
	"github.com/sectersion/hackermode/internal/registry"
)

// installRegistryDep fetches and installs one (id, version) at the given
// platform. Returns the lockfile entry to record.
func installRegistryDep(reg registry.Client, id, version, platform string, opts Opts) (manifest.LockModule, error) {
	fmt.Fprintf(opts.out(), "  reg  %s@%s [%s]\n", id, version, platform)

	mod, err := reg.Module(id)
	if err != nil {
		return manifest.LockModule{}, fmt.Errorf("registry meta: %w", err)
	}
	ver, ok := pickVersion(mod, version)
	if !ok {
		return manifest.LockModule{}, fmt.Errorf("version %q not in registry", version)
	}
	bin, ok := pickBinary(ver, platform)
	if !ok {
		return manifest.LockModule{}, fmt.Errorf("platform %q not supported by %s@%s", platform, id, version)
	}

	cs, err := reg.Checksums(id, version)
	if err != nil {
		return manifest.LockModule{}, fmt.Errorf("checksums: %w", err)
	}

	dst := VersionDir(id, version)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return manifest.LockModule{}, fmt.Errorf("mkdir: %w", err)
	}

	hash, err := downloadAndExtract(reg, id, version, platform, dst)
	if err != nil {
		return manifest.LockModule{}, err
	}

	// Cross-check both hash sources.
	wantMeta := strings.ToLower(bin.SHA256)
	wantCS := strings.ToLower(cs.Entries[binaryFilename(platform)])
	got := strings.ToLower(hash)
	if wantMeta == "" || wantCS == "" {
		return manifest.LockModule{}, fmt.Errorf("missing hash in registry metadata or checksums.txt")
	}
	if got != wantMeta || got != wantCS {
		return manifest.LockModule{}, fmt.Errorf(
			"checksum mismatch: got %s, metadata=%s, checksums.txt=%s",
			got, wantMeta, wantCS)
	}

	if err := activate(id, version); err != nil {
		return manifest.LockModule{}, fmt.Errorf("activate: %w", err)
	}

	binaries := make([]manifest.LockBinary, 0, len(ver.Binaries))
	for _, b := range ver.Binaries {
		binaries = append(binaries, manifest.LockBinary{
			Platform: b.Platform, SHA256: b.SHA256, URL: b.URL,
		})
	}
	hlog.With("module", id).Info("installed", "version", version, "platform", platform)
	return manifest.LockModule{
		ID:       id,
		Version:  version,
		Source:   "registry",
		SHA256:   hash,
		Binaries: binaries,
	}, nil
}

// syncRegistryDep reinstalls a locked module without resolving. The
// downloaded bytes' hash is compared against the lockfile's recorded
// binary hash for the active platform; mismatch is fatal.
func syncRegistryDep(reg registry.Client, mod manifest.LockModule, platform string, opts Opts) error {
	fmt.Fprintf(opts.out(), "  reg  %s@%s [%s] (sync)\n", mod.ID, mod.Version, platform)

	want := ""
	for _, b := range mod.Binaries {
		if b.Platform == platform {
			want = strings.ToLower(b.SHA256)
			break
		}
	}
	if want == "" {
		return fmt.Errorf("lockfile has no entry for platform %q", platform)
	}

	dst := VersionDir(mod.ID, mod.Version)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	got, err := downloadAndExtract(reg, mod.ID, mod.Version, platform, dst)
	if err != nil {
		return err
	}
	if strings.ToLower(got) != want {
		return fmt.Errorf("lockfile checksum mismatch: got %s want %s", got, want)
	}
	return activate(mod.ID, mod.Version)
}

// downloadAndExtract streams the tarball into a memory buffer (hashing
// as it goes), then extracts. Modules are small enough that buffering is
// fine; this also gives us a clean retry boundary later.
//
// Returns the lowercase hex SHA256 of the tarball.
func downloadAndExtract(reg registry.Client, id, version, platform, dst string) (string, error) {
	rc, err := reg.FetchBinary(id, version, platform)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer rc.Close()

	var buf bytes.Buffer
	hash, err := hashCopy(&buf, rc)
	if err != nil {
		return "", err
	}
	if err := extractTarGz(dst, &buf); err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}
	return hash, nil
}

// activate (re)creates the `current` symlink to point at version's dir.
func activate(id, version string) error {
	link := CurrentLink(id)
	_ = os.Remove(link) // ignore "not exist"
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(version, link)
}

func pickVersion(m *registry.Module, want string) (registry.ModuleVersion, bool) {
	for _, v := range m.Versions {
		if v.Version == want {
			return v, true
		}
	}
	return registry.ModuleVersion{}, false
}

func pickBinary(v registry.ModuleVersion, platform string) (registry.BinaryEntry, bool) {
	for _, b := range v.Binaries {
		if b.Platform == platform {
			return b, true
		}
	}
	return registry.BinaryEntry{}, false
}

// binaryFilename mirrors registry.tarballName: "linux/amd64" →
// "linux-amd64.tar.gz". Duplicated rather than exported to keep the
// internal/registry public surface small.
func binaryFilename(platform string) string {
	for i, r := range platform {
		if r == '/' {
			return platform[:i] + "-" + platform[i+1:] + ".tar.gz"
		}
	}
	return platform + ".tar.gz"
}
