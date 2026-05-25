// Package resolver turns a hackermode.toml manifest into a fully-resolved
// module graph using PubGrub.
//
// Inputs:
//
//   - A manifest.Manifest declaring top-level deps.
//   - A VersionIndex that knows, for each module ID, which versions
//     exist and what each version's own dependencies are. The
//     registry client implements this in C3.
//
// Output:
//
//   - A Result with the chosen version for every module in the closure,
//     including transitive deps. Path and git deps are surfaced as
//     fixed pins (the resolver doesn't traverse into them).
//
// PubGrub handles backtracking and conflict reporting. We translate
// hackermode's dep model into PubGrub's Source interface in registrySource.
package resolver

import (
	"errors"
	"fmt"
	"sort"

	"github.com/mircearoata/pubgrub-go/pubgrub"
	"github.com/mircearoata/pubgrub-go/pubgrub/semver"

	"github.com/sectersion/hackermode/internal/manifest"
)

// rootPkg is the synthetic package name used as the resolver's entry
// point. It depends on every top-level entry in the manifest.
const rootPkg = "__root__"

// VersionInfo is one published version of a module, as returned by the
// registry. Dependencies map module IDs to semver constraints. The
// registry client builds this; the resolver consumes it.
type VersionInfo struct {
	Version      string
	Dependencies map[string]string // dep id → semver constraint
}

// VersionIndex is the resolver's view of the registry. Implementations
// must return versions sorted highest-first; the resolver picks the
// first one that satisfies a constraint.
type VersionIndex interface {
	// Versions returns all published versions of a module. Returns an
	// empty slice (and nil error) when the module exists but has no
	// versions; returns an error when the module ID is unknown.
	Versions(moduleID string) ([]VersionInfo, error)
}

// Pin is a resolved module entry. The resolver emits one per top-level
// dep and every transitive dep reachable from them.
type Pin struct {
	// Source classifies the resolution: "registry", "git", or "path".
	// Registry pins fill Version; git pins fill GitRev; path pins fill
	// PathDir.
	Source  string
	Version string
	GitRev  string
	PathDir string
}

// Result is the full resolved graph.
type Result struct {
	Pins map[string]Pin // module ID → pin
}

// Resolve runs PubGrub against the manifest's dependencies and returns
// the resolved graph. Path and git deps short-circuit PubGrub — they're
// added to the result with their declared pin and excluded from the
// registry's traversal.
func Resolve(m *manifest.Manifest, idx VersionIndex) (*Result, error) {
	if m == nil {
		return nil, errors.New("resolver: manifest is required")
	}
	if idx == nil {
		return nil, errors.New("resolver: VersionIndex is required")
	}

	overrides := map[string]Pin{}
	registryRoots := map[string]string{}
	for id, dep := range m.Modules {
		switch {
		case dep.IsPath():
			overrides[id] = Pin{Source: "path", PathDir: dep.Path}
		case dep.IsGit():
			overrides[id] = Pin{Source: "git", GitRev: dep.GitRev}
		case dep.IsRegistry():
			registryRoots[id] = dep.Version
		default:
			return nil, fmt.Errorf("module %q: invalid dependency form", id)
		}
	}

	src := &resolverSource{
		idx:      idx,
		roots:    registryRoots,
		excluded: overrides,
	}

	chosen, err := pubgrub.Solve(src, rootPkg)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}

	result := &Result{Pins: map[string]Pin{}}
	for id, v := range chosen {
		if id == rootPkg {
			continue
		}
		result.Pins[id] = Pin{Source: "registry", Version: v.RawString()}
	}
	for id, pin := range overrides {
		result.Pins[id] = pin
	}
	return result, nil
}

// resolverSource adapts our VersionIndex into pubgrub's Source. The root
// package depends on every top-level registry constraint. Path / git
// overrides are advertised as a single synthetic version so PubGrub
// satisfies the constraint trivially without ever traversing into the
// override's own deps.
type resolverSource struct {
	idx      VersionIndex
	roots    map[string]string // dep id → constraint (registry roots only)
	excluded map[string]Pin    // path / git overrides
}

// GetPackageVersions returns the candidate versions PubGrub may pick from.
func (s *resolverSource) GetPackageVersions(pkg string) ([]pubgrub.PackageVersion, error) {
	if pkg == rootPkg {
		deps := map[string]semver.Constraint{}
		for id, c := range s.roots {
			cs, err := semver.NewConstraint(c)
			if err != nil {
				return nil, fmt.Errorf("root dep %q: bad constraint %q: %w", id, c, err)
			}
			deps[id] = cs
		}
		// Path / git deps still need a slot in the root's deps so the
		// solver visits them (and so we can detect transitive registry
		// requirements down the line that conflict with the override).
		for id := range s.excluded {
			deps[id] = anyConstraint()
		}
		ver, _ := semver.NewVersion("0.0.0")
		return []pubgrub.PackageVersion{
			{Version: ver, Dependencies: deps},
		}, nil
	}

	// For path/git overrides we expose a single synthetic version that
	// satisfies any constraint and has no own dependencies. This lets
	// the rest of the graph resolve normally.
	if _, ok := s.excluded[pkg]; ok {
		ver, _ := semver.NewVersion("0.0.0")
		return []pubgrub.PackageVersion{{Version: ver}}, nil
	}

	versions, err := s.idx.Versions(pkg)
	if err != nil {
		return nil, fmt.Errorf("unknown module %q: %w", pkg, err)
	}
	out := make([]pubgrub.PackageVersion, 0, len(versions))
	for _, v := range versions {
		semVer, err := semver.NewVersion(v.Version)
		if err != nil {
			return nil, fmt.Errorf("module %q: invalid version %q: %w", pkg, v.Version, err)
		}
		deps := map[string]semver.Constraint{}
		for dID, dC := range v.Dependencies {
			c, err := semver.NewConstraint(dC)
			if err != nil {
				return nil, fmt.Errorf("module %q@%s dep %q: bad constraint %q: %w", pkg, v.Version, dID, dC, err)
			}
			deps[dID] = c
		}
		out = append(out, pubgrub.PackageVersion{Version: semVer, Dependencies: deps})
	}
	return out, nil
}

// PickVersion returns the preferred candidate. We pick the highest.
func (s *resolverSource) PickVersion(pkg string, candidates []semver.Version) semver.Version {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Compare(candidates[j]) > 0
	})
	return candidates[0]
}

func anyConstraint() semver.Constraint {
	c, _ := semver.NewConstraint(">=0.0.0")
	return c
}
