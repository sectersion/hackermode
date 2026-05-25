// Package cmd is the CLI surface. The root command launches the TUI.
// Subcommands (install, run, env, ...) will be added in later phases.
//
// Both `hackermode` and `hm` (when installed as a symlink to the same binary)
// resolve to this package; argv[0] is treated as cosmetic.
package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/config"
	hlog "github.com/sectersion/hackermode/internal/log"
	"github.com/sectersion/hackermode/internal/paths"
	"github.com/sectersion/hackermode/internal/tui"
)

// Execute is the single entry point. argv0 is the basename of os.Args[0]
// (so symlinked `hm` reports correctly in --help and version output).
func Execute(argv0 string) {
	// Allow `hm i ...` to mean `hm install ...` when the prefix is unambiguous.
	cobra.EnablePrefixMatching = true

	root := newRootCmd(argv0)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd(argv0 string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           argv0,
		Short:         "hackermode — a CLI for everything",
		Long:          "hackermode is a TUI host that loads modules from a marketplace.\nThe `hm` and `hackermode` binaries are equivalent.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Allow unique-prefix subcommand abbreviation: `hm i ...` → install.
		// (Cobra performs this when the prefix is unambiguous.)
		RunE: runRoot,
	}
	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.Version = "0.0.0-dev"

	cmd.AddCommand(newDevCmd())
	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newRegistryCmd())

	// C5: install / inspect / sync family
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newInstallCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newLockCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newSearchCmd())
	cmd.AddCommand(newWhyCmd())
	cmd.AddCommand(newOutdatedCmd())
	cmd.AddCommand(newProfileCmd())

	// C5.4: author DX
	cmd.AddCommand(newLinkCmd())
	cmd.AddCommand(newNewCmd())
	cmd.AddCommand(newPublishCmd())
	return cmd
}

func runRoot(cmd *cobra.Command, args []string) error {
	if err := paths.EnsureAll(); err != nil {
		return fmt.Errorf("init paths: %w", err)
	}

	if err := hlog.Init(); err != nil {
		// Logging failure is non-fatal; just print once before the TUI takes over.
		fmt.Fprintln(os.Stderr, "warning: log init:", err)
	}
	defer hlog.Close()
	hlog.Info("hackermode start", "argv0", cmd.Name())

	cfg, err := config.Load()
	if err != nil {
		hlog.Warn("config load failed", "err", err.Error())
	}

	model := tui.New(cfg)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	// Background goroutines (module stdout pump, etc.) need a way to
	// inject messages back into the event loop. Capture program.Send
	// into a Runtime and hand it to the model before Run starts.
	model.SetRuntime(&tui.Runtime{Send: p.Send})
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
