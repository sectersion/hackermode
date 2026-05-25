// Package cmd — `hackermode install [<id>[@version]]`.
//
// Two modes:
//
//   install                  — re-resolve the current manifest and
//                              install everything that's missing. The
//                              manifest itself is unchanged.
//
//   install <id>[@version]   — add the dep to the manifest, then re-
//                              resolve and install. The added entry
//                              uses a "^X.Y" caret constraint derived
//                              from the chosen version (or the latest
//                              available when no version is supplied).
//
// Both modes write the updated lockfile next to the manifest.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/installer"
	"github.com/sectersion/hackermode/internal/manifest"
)

func newInstallCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "install [<id>[@version]]",
		Short: "Install (or add + install) modules from the registry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := loadContext(true, profile)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if err := addDependency(ctx.manifest, args[0]); err != nil {
					return err
				}
			}
			lock, err := installer.Install(ctx.manifest, ctx.registry, installer.Opts{
				Out: cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %d modules; lockfile at %s\n",
				len(lock.Modules), lock.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "profile to install into")
	return cmd
}

// addDependency parses "id[@version]" and writes the entry into the
// manifest on disk. The choice of constraint is intentionally simple
// for v0: an explicit version pin becomes "^X.Y" (the same major+minor
// floor cargo uses by default); no version means "*" until we re-run
// resolution and refine it.
//
// We mutate the on-disk TOML directly so we preserve user formatting
// and comments. The mutation is line-based: find the "[modules]"
// header, append the entry if it's missing. This is good enough for
// Phase C; Phase E may replace it with a structural TOML editor.
func addDependency(m *manifest.Manifest, spec string) error {
	id, ver := parseSpec(spec)
	// No version supplied: ">=0.0.0" matches any published version so
	// the resolver picks whatever's latest. With an explicit version we
	// emit "^X.Y.Z" (caret), the same default Cargo / npm use. Users
	// can tighten constraints by hand-editing hackermode.toml.
	constraint := ">=0.0.0"
	if ver != "" {
		constraint = "^" + ver
	}

	if m.Path == "" {
		return fmt.Errorf("manifest has no on-disk path; cannot edit")
	}
	data, err := os.ReadFile(m.Path)
	if err != nil {
		return err
	}
	if hasModuleEntry(string(data), id) {
		return fmt.Errorf("%s is already in the manifest", id)
	}
	updated := appendModuleEntry(string(data), id, constraint)
	if err := os.WriteFile(m.Path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	// Re-parse so the caller's manifest reflects the change.
	freshly, err := manifest.ParseFile(m.Path)
	if err != nil {
		return fmt.Errorf("re-parse: %w", err)
	}
	*m = *freshly
	return nil
}

// parseSpec splits "id" or "id@version" into the two components.
func parseSpec(spec string) (id, version string) {
	if i := strings.IndexByte(spec, '@'); i > 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}

// hasModuleEntry returns true if a `"id" =` line exists under
// [modules]. Naive scan; good enough for the line-based edit.
func hasModuleEntry(src, id string) bool {
	needle := `"` + id + `"`
	for _, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, needle) && strings.Contains(trim, "=") {
			return true
		}
	}
	return false
}

// appendModuleEntry inserts a new "id" = "constraint" line under the
// [modules] header. If no [modules] header exists, one is appended.
func appendModuleEntry(src, id, constraint string) string {
	entry := fmt.Sprintf(`"%s" = "%s"`, id, constraint)
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "[modules]" {
			// Insert after the [modules] header (and any contiguous
			// comments) so existing comments stay attached.
			insertAt := i + 1
			for insertAt < len(lines) {
				t := strings.TrimSpace(lines[insertAt])
				if t == "" || strings.HasPrefix(t, "#") {
					insertAt++
					continue
				}
				break
			}
			lines = append(lines[:insertAt], append([]string{entry}, lines[insertAt:]...)...)
			return strings.Join(lines, "\n")
		}
	}
	// No [modules] section yet — append one.
	tail := "\n[modules]\n" + entry + "\n"
	return strings.TrimRight(src, "\n") + "\n" + tail
}
