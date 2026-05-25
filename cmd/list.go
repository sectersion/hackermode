// Package cmd — read-only inspection subcommands.
//
//   list      — show installed modules (scan ~/.local/share/hackermode/modules/)
//   search    — search the registry index
//   why       — explain why a module is in the dependency graph (top-level
//               dep, transitive, override). Phase C v0 just reports the
//               source; richer "chain" output lands when we add edges to
//               the resolver result.
//   outdated  — compare installed versions to the registry's latest
package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/installer"
	"github.com/sectersion/hackermode/internal/manifest"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed modules",
		RunE: func(cmd *cobra.Command, args []string) error {
			installed, err := installer.Scan()
			if err != nil {
				return err
			}
			if len(installed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no modules installed")
				return nil
			}
			sort.Slice(installed, func(i, j int) bool {
				return installed[i].Manifest.Module.ID < installed[j].Manifest.Module.ID
			})
			for _, inst := range installed {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n",
					inst.Manifest.Module.ID,
					inst.Manifest.Module.Version,
					inst.Manifest.Module.Name)
			}
			return nil
		},
	}
}

func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search the registry index for modules",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := loadContext(false, "")
			if err != nil {
				return err
			}
			idx, err := ctx.registry.Index()
			if err != nil {
				return err
			}
			query := ""
			if len(args) == 1 {
				query = strings.ToLower(args[0])
			}
			matches := []string{}
			for _, e := range idx.Modules {
				if query != "" {
					hay := strings.ToLower(e.ID + " " + e.Name + " " + e.Description)
					if !strings.Contains(hay, query) {
						continue
					}
				}
				matches = append(matches,
					fmt.Sprintf("%s\t%s\t%s", e.ID, e.Latest, e.Name))
			}
			if len(matches) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no matches")
				return nil
			}
			sort.Strings(matches)
			for _, m := range matches {
				fmt.Fprintln(cmd.OutOrStdout(), m)
			}
			return nil
		},
	}
}

func newWhyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "why <id>",
		Short: "Show why a module is in the dependency graph",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			ctx, err := loadContext(true, "")
			if err != nil {
				return err
			}
			lockPath := manifest.LockfilePathFor(ctx.manifest.Path)
			lock, err := manifest.ParseLockFile(lockPath)
			if err != nil {
				return fmt.Errorf("no lockfile: %w", err)
			}
			entry, ok := lock.Get(id)
			if !ok {
				return fmt.Errorf("%s is not in the lockfile", id)
			}
			if _, top := ctx.manifest.Modules[id]; top {
				fmt.Fprintf(cmd.OutOrStdout(),
					"%s@%s — top-level dep declared in %s (source: %s)\n",
					id, entry.Version, ctx.manifest.Path, entry.Source)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"%s@%s — transitive dep (source: %s)\n",
				id, entry.Version, entry.Source)
			return nil
		},
	}
}

func newOutdatedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "outdated",
		Short: "List modules whose installed version is behind the registry's latest",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := loadContext(true, "")
			if err != nil {
				return err
			}
			lockPath := manifest.LockfilePathFor(ctx.manifest.Path)
			lock, err := manifest.ParseLockFile(lockPath)
			if err != nil {
				return fmt.Errorf("no lockfile: %w", err)
			}
			idx, err := ctx.registry.Index()
			if err != nil {
				return err
			}
			latest := map[string]string{}
			for _, e := range idx.Modules {
				latest[e.ID] = e.Latest
			}
			out := []string{}
			for _, mod := range lock.Modules {
				if mod.Source != "registry" {
					continue
				}
				if l, ok := latest[mod.ID]; ok && l != mod.Version {
					out = append(out,
						fmt.Sprintf("%s\t%s → %s", mod.ID, mod.Version, l))
				}
			}
			if len(out) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "everything is up to date")
				return nil
			}
			sort.Strings(out)
			for _, line := range out {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}
}
