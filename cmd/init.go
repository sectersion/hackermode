// Package cmd — `hackermode init`: scaffold a hackermode.toml in cwd.
//
// `init` is intentionally minimal. We don't pre-populate any module
// dependencies; the user picks what they want via subsequent `install`
// commands. Profiles (named manifests living in ~/.config) are created
// by `hackermode profile new` rather than here.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/manifest"
)

const initTemplate = `[hackermode]
version = "0.1"

[profile]
name        = "default"
description = "Personal modules"

[modules]
# add modules with: hackermode install <id>
# or hand-edit, e.g.:
#   "acme.email" = "^0.3"

[ui]
theme            = "default"
side_panel_width = 12
`

func newInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a hackermode.toml in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			path := filepath.Join(cwd, manifest.FileName)
			if _, err := os.Stat(path); err == nil && !force {
				return errors.New("hackermode.toml already exists (use --force to overwrite)")
			}
			if err := os.WriteFile(path, []byte(initTemplate), 0o644); err != nil {
				return fmt.Errorf("write: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing manifest")
	return cmd
}
