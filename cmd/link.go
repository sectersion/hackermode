// Package cmd — `hackermode link <path>`: register a local module as a
// path-dep in the active manifest. Useful while developing — the host
// will spawn the module from its source directory instead of needing
// a published version.
//
// Effect: writes `"<id>" = { path = "<absolute path>" }` into [modules].
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/modules"
)

func newLinkCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "link <path>",
		Short: "Register a local module directory as a path-dep",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			modManifest, err := modules.ParseDir(abs)
			if err != nil {
				return fmt.Errorf("read module manifest: %w", err)
			}
			if errs := modManifest.Validate(); len(errs) > 0 {
				return fmt.Errorf("module manifest invalid: %v", errs[0])
			}
			id := modManifest.Module.ID

			ctx, err := loadContext(true, profile)
			if err != nil {
				return err
			}
			// Drop any existing entry, then write the path form.
			_ = removeDependency(ctx.manifest, id) // ignore "not in manifest"
			data, err := os.ReadFile(ctx.manifest.Path)
			if err != nil {
				return err
			}
			entry := fmt.Sprintf(`"%s" = { path = "%s" }`, id, abs)
			updated := insertModuleLine(string(data), entry)
			if err := os.WriteFile(ctx.manifest.Path, []byte(updated), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "linked %s → %s\n", id, abs)
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "profile to edit")
	return cmd
}

// insertModuleLine drops `entry` under the [modules] header (creating
// the section if necessary). Doesn't validate that the entry's "id" is
// unique — callers should remove duplicates first via removeDependency.
func insertModuleLine(src, entry string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "[modules]" {
			insertAt := i + 1
			for insertAt < len(lines) {
				t := strings.TrimSpace(lines[insertAt])
				if t == "" || strings.HasPrefix(t, "#") {
					insertAt++
					continue
				}
				break
			}
			lines = append(lines[:insertAt],
				append([]string{entry}, lines[insertAt:]...)...)
			return strings.Join(lines, "\n")
		}
	}
	return strings.TrimRight(src, "\n") + "\n\n[modules]\n" + entry + "\n"
}
