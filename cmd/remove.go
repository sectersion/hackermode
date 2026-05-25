// Package cmd — `hackermode remove <id>`: drop a module from the active
// manifest and re-resolve. Existing on-disk module files are NOT
// deleted; future garbage collection can prune them once we have
// reference counting.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/installer"
	"github.com/sectersion/hackermode/internal/manifest"
)

func newRemoveCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a module from the manifest and re-resolve",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := loadContext(true, profile)
			if err != nil {
				return err
			}
			if err := removeDependency(ctx.manifest, args[0]); err != nil {
				return err
			}
			// Re-resolve. The installer rewrites the lockfile from the new
			// manifest; orphans naturally drop out.
			if _, err := installer.Install(ctx.manifest, ctx.registry, installer.Opts{
				Out: cmd.OutOrStdout(),
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "profile to edit")
	return cmd
}

// removeDependency is the inverse of addDependency in install.go. It
// rewrites the manifest in place, dropping any line whose key matches
// `"id"`. Preserves surrounding comments / formatting.
func removeDependency(m *manifest.Manifest, id string) error {
	if m.Path == "" {
		return fmt.Errorf("manifest has no on-disk path")
	}
	data, err := os.ReadFile(m.Path)
	if err != nil {
		return err
	}
	if !hasModuleEntry(string(data), id) {
		return fmt.Errorf("%s is not in the manifest", id)
	}

	needle := `"` + id + `"`
	out := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, needle) && strings.Contains(trim, "=") {
			continue
		}
		out = append(out, line)
	}
	if err := os.WriteFile(m.Path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return err
	}
	freshly, err := manifest.ParseFile(m.Path)
	if err != nil {
		return err
	}
	*m = *freshly
	return nil
}
