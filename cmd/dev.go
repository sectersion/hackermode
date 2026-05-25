// Package cmd — `hackermode dev` family: developer-facing subcommands that
// run uninstalled modules straight from a source directory. The marketplace
// install path lands in Phase C; until then, every developer test runs
// through `dev run`.
//
// Currently provided:
//
//   hackermode dev run <dir>    Launch the TUI and auto-run the manifest
//                               at <dir> in the first tab. Manifest is
//                               required; there is no "raw binary"
//                               escape hatch by design.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/config"
	hlog "github.com/sectersion/hackermode/internal/log"
	"github.com/sectersion/hackermode/internal/modules"
	"github.com/sectersion/hackermode/internal/paths"
	"github.com/sectersion/hackermode/internal/tui"
)

func newDevCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Developer workflows (run a module from source)",
	}
	cmd.AddCommand(newDevRunCmd())
	return cmd
}

func newDevRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <dir>",
		Short: "Launch the TUI and auto-run the module manifest at <dir>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			// Validate the manifest up-front so we fail with a useful
			// message before launching the TUI.
			manifest, err := modules.ParseDir(dir)
			if err != nil {
				return fmt.Errorf("manifest: %w", err)
			}
			if vErrs := manifest.Validate(); len(vErrs) > 0 {
				msg := "manifest invalid:"
				for _, e := range vErrs {
					msg += "\n  - " + e.Error()
				}
				return errors.New(msg)
			}
			return runTUIWithAutoLaunch(dir)
		},
	}
}

// runTUIWithAutoLaunch is the dev-run TUI launcher. It is identical to
// the bare TUI startup except it sends a `:dev run <dir>` slash-command
// after the program starts running, so the user lands directly in a tab
// bound to the module.
func runTUIWithAutoLaunch(dir string) error {
	if err := paths.EnsureAll(); err != nil {
		return fmt.Errorf("init paths: %w", err)
	}
	if err := hlog.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: log init:", err)
	}
	defer hlog.Close()
	hlog.Info("hackermode dev run", "dir", dir)

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
	model.SetRuntime(&tui.Runtime{Send: p.Send})

	// Auto-launch: after the program starts, send a synthetic
	// AutoRunMsg the model already knows how to handle. The model
	// translates it into a :dev run invocation against its own active
	// tab.
	go func() {
		p.Send(tui.AutoRunMsg{Dir: dir})
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
