// Package cmd — shared command context: the manifest + registry + lockfile
// trio that almost every C5 subcommand needs.
//
// `loadContext` is the canonical entry point. It resolves the active
// manifest (project → profile → system), loads the local config (for
// the registry URL), and returns a Context the subcommand can use.
//
// Why a context struct vs. globals: the same code path is used by both
// the CLI and (eventually) the TUI marketplace tab. Threading a value
// makes that reuse explicit.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sectersion/hackermode/internal/config"
	"github.com/sectersion/hackermode/internal/manifest"
	"github.com/sectersion/hackermode/internal/registry"
)

type cmdContext struct {
	cfg      config.Config
	manifest *manifest.Manifest
	scope    manifest.Scope
	registry registry.Client
}

// loadContext gathers everything needed by an install-related command.
// `needsManifest` controls whether we error out when no manifest is
// found (some commands like `init` create one rather than reading one).
func loadContext(needsManifest bool, profile string) (*cmdContext, error) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: config load:", err)
	}

	c := &cmdContext{cfg: cfg}

	m, scope, err := manifest.Load(manifest.LocateOpts{Profile: profile})
	switch {
	case err == nil:
		c.manifest = m
		c.scope = scope
	case err == manifest.ErrNoManifest:
		if needsManifest {
			return nil, fmt.Errorf("no hackermode.toml found — run `hackermode init` to create one")
		}
	default:
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	regClient, err := openRegistry(cfg.Marketplace.Registry)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	c.registry = regClient

	return c, nil
}

// openRegistry returns a Client for the configured URL. Supported
// schemes (Phase C):
//
//   file:///path/to/dir     filesystem-backed registry
//   /path/to/dir            (bare path, treated as file://)
//
// HTTP support arrives when the Node.js production server lands; for
// now this returns an error with a clear message.
func openRegistry(url string) (registry.Client, error) {
	if url == "" {
		return nil, fmt.Errorf("no registry configured (set [marketplace].registry in config.toml)")
	}
	switch {
	case strings.HasPrefix(url, "file://"):
		return registry.NewFS(strings.TrimPrefix(url, "file://")), nil
	case strings.HasPrefix(url, "/"):
		return registry.NewFS(url), nil
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		return nil, fmt.Errorf("HTTP registries land with the production registry; use file:// for now")
	default:
		return nil, fmt.Errorf("unsupported registry URL %q (expected file:// or absolute path)", url)
	}
}
