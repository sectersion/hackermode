// Package registry — adapter that lets a Client be used as the resolver's
// VersionIndex. Keeps the resolver decoupled from the registry's wire
// types and lets callers swap registries without touching resolver code.
package registry

import (
	"fmt"

	"github.com/sectersion/hackermode/internal/resolver"
)

// IndexFromClient wraps a Client so it satisfies resolver.VersionIndex.
type IndexFromClient struct{ C Client }

// Versions returns every published, non-yanked version of moduleID.
func (a IndexFromClient) Versions(moduleID string) ([]resolver.VersionInfo, error) {
	m, err := a.C.Module(moduleID)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	out := make([]resolver.VersionInfo, 0, len(m.Versions))
	for _, v := range m.Versions {
		if v.Yanked {
			continue
		}
		out = append(out, resolver.VersionInfo{
			Version:      v.Version,
			Dependencies: v.Dependencies,
		})
	}
	return out, nil
}
