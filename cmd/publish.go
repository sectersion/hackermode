// Package cmd — `hackermode publish`: package the module in the current
// directory and write it into a filesystem registry.
//
// Phase C does the minimum useful thing: build (if needed), package
// manifest + binary into a tarball, compute its sha256, update the
// registry's index.json + module.json + checksums.txt + tarball file.
//
// We deliberately avoid signing here — that's Phase F. We also avoid
// network upload — the registry is a directory you point at. Push that
// directory to a git repo or a Cloudflare R2 bucket out-of-band; the
// host doesn't care how it gets there as long as the wire shape stays
// intact.
package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/modules"
	"github.com/sectersion/hackermode/internal/registry"
)

func newPublishCmd() *cobra.Command {
	var regRoot, platform string
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Package the module in cwd and add it to a filesystem registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if regRoot == "" {
				return errors.New("--registry-dir is required (path to a registry root)")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			m, err := modules.ParseDir(cwd)
			if err != nil {
				return fmt.Errorf("read module manifest: %w", err)
			}
			if errs := m.Validate(); len(errs) > 0 {
				return fmt.Errorf("module manifest invalid: %v", errs[0])
			}
			if platform == "" {
				platform = runtime.GOOS + "/" + runtime.GOARCH
			}

			tarballBytes, err := packModule(cwd, m)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(tarballBytes)
			hashHex := hex.EncodeToString(hash[:])
			if err := writeToRegistry(regRoot, m, platform, tarballBytes, hashHex); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"published %s@%s [%s] into %s\n",
				m.Module.ID, m.Module.Version, platform, regRoot)
			return nil
		},
	}
	cmd.Flags().StringVar(&regRoot, "registry-dir", "", "path to a filesystem registry root")
	cmd.Flags().StringVar(&platform, "platform", "", "platform string (default: current GOOS/GOARCH)")
	return cmd
}

// packModule tar+gzips hackermode.toml plus the entry binary into bytes.
func packModule(dir string, m *modules.Manifest) ([]byte, error) {
	binPath := filepath.Join(dir, m.Entry.Binary)
	if _, err := os.Stat(binPath); err != nil {
		return nil, fmt.Errorf("entry binary %q missing — build it first", m.Entry.Binary)
	}
	var buf strings.Builder
	gz := gzip.NewWriter(byteWriter{b: &buf})
	tw := tar.NewWriter(gz)

	addFile := func(name, src string, mode int64) error {
		fi, err := os.Stat(src)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:     name,
			Size:     fi.Size(),
			Mode:     mode,
			Typeflag: tar.TypeReg,
			ModTime:  time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	}
	if err := addFile("hackermode.toml", filepath.Join(dir, modules.ManifestFileName), 0o644); err != nil {
		return nil, err
	}
	if err := addFile(m.Entry.Binary, binPath, 0o755); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// byteWriter lets us hand strings.Builder to gzip (which wants io.Writer).
type byteWriter struct{ b *strings.Builder }

func (w byteWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// writeToRegistry updates the registry's index.json, module.json, and
// stores the tarball + checksums.txt at the right path.
func writeToRegistry(root string, m *modules.Manifest, platform string, tarball []byte, sha256Hex string) error {
	id := m.Module.ID
	ver := m.Module.Version
	verDir := filepath.Join(root, "modules", id, ver)
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		return err
	}

	tarballName := strings.Replace(platform, "/", "-", 1) + ".tar.gz"
	if err := os.WriteFile(filepath.Join(verDir, tarballName), tarball, 0o644); err != nil {
		return err
	}
	cs := fmt.Sprintf("%s %s\n", sha256Hex, tarballName)
	if err := os.WriteFile(filepath.Join(verDir, "checksums.txt"), []byte(cs), 0o644); err != nil {
		return err
	}

	moduleJSONPath := filepath.Join(root, "modules", id, "module.json")
	mod, err := readOrInitModuleJSON(moduleJSONPath, m)
	if err != nil {
		return err
	}
	mod = upsertModuleVersion(mod, m, platform, sha256Hex, tarballName)
	body, err := json.MarshalIndent(mod, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(moduleJSONPath, body, 0o644); err != nil {
		return err
	}
	return updateIndex(root, m)
}

func readOrInitModuleJSON(path string, m *modules.Manifest) (*registry.Module, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// New module — start fresh.
		return &registry.Module{
			ID: m.Module.ID, Name: m.Module.Name,
			Description: m.Module.Description, Homepage: m.Module.Homepage,
			License: m.Module.License, Author: m.Module.Author,
		}, nil
	}
	var out registry.Module
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// upsertModuleVersion adds or updates the version's binary entry and
// keeps versions[] sorted with the newest first.
func upsertModuleVersion(mod *registry.Module, m *modules.Manifest, platform, sha, tarballName string) *registry.Module {
	id := m.Module.ID
	ver := m.Module.Version
	bin := registry.BinaryEntry{
		Platform: platform,
		URL:      fmt.Sprintf("/module/%s/%s/%s", id, ver, tarballName),
		SHA256:   sha,
	}
	var idx = -1
	for i, v := range mod.Versions {
		if v.Version == ver {
			idx = i
			break
		}
	}
	if idx >= 0 {
		// Update existing binary entry for this platform.
		updatedBins := []registry.BinaryEntry{}
		seen := false
		for _, b := range mod.Versions[idx].Binaries {
			if b.Platform == platform {
				updatedBins = append(updatedBins, bin)
				seen = true
				continue
			}
			updatedBins = append(updatedBins, b)
		}
		if !seen {
			updatedBins = append(updatedBins, bin)
		}
		mod.Versions[idx].Binaries = updatedBins
	} else {
		mod.Versions = append(mod.Versions, registry.ModuleVersion{
			Version:      ver,
			Released:     time.Now().UTC().Format(time.RFC3339),
			ManifestURL:  fmt.Sprintf("/module/%s/%s/hackermode.toml", id, ver),
			ChecksumsURL: fmt.Sprintf("/module/%s/%s/checksums.txt", id, ver),
			Binaries:     []registry.BinaryEntry{bin},
			Capabilities: m.Capabilities.Required,
		})
	}
	sort.Slice(mod.Versions, func(i, j int) bool {
		return mod.Versions[i].Version > mod.Versions[j].Version
	})
	return mod
}

// updateIndex rewrites <root>/index.json so the module appears in the
// global catalogue. Idempotent.
func updateIndex(root string, m *modules.Manifest) error {
	path := filepath.Join(root, "index.json")
	var idx registry.Index
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &idx)
	}
	if idx.Schema == "" {
		idx.Schema = registry.SchemaVersion
	}
	entry := registry.IndexEntry{
		ID: m.Module.ID, Name: m.Module.Name,
		Description: m.Module.Description, Latest: m.Module.Version,
	}
	replaced := false
	for i := range idx.Modules {
		if idx.Modules[i].ID == m.Module.ID {
			idx.Modules[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Modules = append(idx.Modules, entry)
	}
	sort.Slice(idx.Modules, func(i, j int) bool {
		return idx.Modules[i].ID < idx.Modules[j].ID
	})
	body, _ := json.MarshalIndent(idx, "", "  ")
	return os.WriteFile(path, body, 0o644)
}
