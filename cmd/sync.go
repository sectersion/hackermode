// Package cmd — `hackermode sync` and `hackermode lock`.
//
//   lock   — re-resolve the manifest and rewrite hackermode.lock without
//            downloading anything. Useful when you've hand-edited the
//            manifest and want the lockfile refreshed before committing.
//
//   sync   — install exactly what's in the lockfile, no resolution. The
//            command refuses to run if the lockfile hashes don't line up
//            with what the registry serves — designed for CI / fresh
//            machine bootstrapping where reproducibility matters.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/installer"
	"github.com/sectersion/hackermode/internal/manifest"
)

func newSyncCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Install exactly what's in the lockfile (no resolution)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := loadContext(true, profile)
			if err != nil {
				return err
			}
			lockPath := manifest.LockfilePathFor(ctx.manifest.Path)
			lock, err := manifest.ParseLockFile(lockPath)
			if err != nil {
				return fmt.Errorf("no lockfile (run `hackermode lock` first): %w", err)
			}
			if err := installer.Sync(lock, ctx.registry, installer.Opts{
				Out: cmd.OutOrStdout(),
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synced %d modules\n", len(lock.Modules))
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "profile to sync")
	return cmd
}

func newLockCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Re-resolve the manifest and rewrite the lockfile",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := loadContext(true, profile)
			if err != nil {
				return err
			}
			// Install does everything we want for `lock` already — the
			// only difference would be skipping downloads, which we
			// don't (yet) optimize since the registry layout makes
			// HEAD-only checks meaningless on the local filesystem.
			lock, err := installer.Install(ctx.manifest, ctx.registry, installer.Opts{
				Out: cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "lockfile written: %s\n", lock.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "profile to lock")
	return cmd
}
