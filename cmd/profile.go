// Package cmd — `hackermode profile <subcommand>`.
//
// Profiles are named manifests that live in ~/.config/hackermode/profiles/.
// They let one user keep multiple hackermode setups — e.g. "work" with
// AI + email + calendar, "personal" with a smaller set — and switch
// between them via the --profile flag on install/sync/run.
//
// Subcommands:
//
//   list             show known profiles
//   new <name>       create a fresh profile (uses the init.go template)
//   delete <name>    remove a profile (manifest + lockfile)
//   use <name>       set the default profile in config.toml
package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/paths"
)

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage named manifests (profiles)",
	}
	cmd.AddCommand(newProfileListCmd())
	cmd.AddCommand(newProfileNewCmd())
	cmd.AddCommand(newProfileDeleteCmd())
	cmd.AddCommand(newProfileUseCmd())
	return cmd
}

func newProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			profs, err := listProfiles()
			if err != nil {
				return err
			}
			if len(profs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no profiles")
				return nil
			}
			sort.Strings(profs)
			for _, p := range profs {
				fmt.Fprintln(cmd.OutOrStdout(), p)
			}
			return nil
		},
	}
}

func newProfileNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <name>",
		Short: "Create a fresh profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateProfileName(name); err != nil {
				return err
			}
			if err := os.MkdirAll(paths.ProfilesDir(), 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			path := profilePath(name)
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("profile %q already exists at %s", name, path)
			}
			if err := os.WriteFile(path, []byte(initTemplate), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", path)
			return nil
		},
	}
}

func newProfileDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a profile (manifest + lockfile)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path := profilePath(name)
			if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("profile %q not found", name)
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			// Best-effort lockfile cleanup.
			lock := strings.TrimSuffix(path, ".toml") + ".lock"
			_ = os.Remove(lock)
			fmt.Fprintf(cmd.OutOrStdout(), "deleted profile %q\n", name)
			return nil
		},
	}
}

func newProfileUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the default profile in config.toml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// Just verify the profile exists; don't actually update
			// config.toml structurally — we do a line edit to preserve
			// formatting (same pattern as install.go).
			if _, err := os.Stat(profilePath(name)); errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("profile %q not found (use `hackermode profile new %s`)", name, name)
			}
			cfgPath := filepath.Join(paths.ConfigDir(), "config.toml")
			if err := setDefaultProfile(cfgPath, name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "default profile is now %q\n", name)
			return nil
		},
	}
}

// listProfiles returns the names of every <name>.toml in the profiles
// directory (without the extension).
func listProfiles() ([]string, error) {
	dir := paths.ProfilesDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".toml") {
			out = append(out, strings.TrimSuffix(e.Name(), ".toml"))
		}
	}
	return out, nil
}

func profilePath(name string) string {
	return filepath.Join(paths.ProfilesDir(), name+".toml")
}

func validateProfileName(name string) error {
	if name == "" {
		return errors.New("profile name required")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
		default:
			return fmt.Errorf("invalid profile name %q (use letters, digits, -, _)", name)
		}
	}
	return nil
}

// setDefaultProfile rewrites the [hackermode].default_profile line in
// config.toml. If the file doesn't exist, creates a minimal one. If the
// line exists, replaces it; otherwise appends a [hackermode] section.
func setDefaultProfile(cfgPath, name string) error {
	body, err := os.ReadFile(cfgPath)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		body = nil
	default:
		return err
	}
	updated := upsertDefaultProfile(string(body), name)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(cfgPath, []byte(updated), 0o644)
}

func upsertDefaultProfile(src, name string) string {
	newLine := fmt.Sprintf(`default_profile = "%s"`, name)
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "default_profile") {
			lines[i] = newLine
			return strings.Join(lines, "\n")
		}
	}
	// Look for [hackermode] section, insert after its header.
	for i, l := range lines {
		if strings.TrimSpace(l) == "[hackermode]" {
			lines = append(lines[:i+1], append([]string{newLine}, lines[i+1:]...)...)
			return strings.Join(lines, "\n")
		}
	}
	// No [hackermode] section — append one.
	prefix := strings.TrimRight(src, "\n")
	if prefix != "" {
		prefix += "\n\n"
	}
	return prefix + "[hackermode]\n" + newLine + "\n"
}
