// Package cmd — `hackermode registry` family. Phase C ships:
//
//   hackermode registry serve <dir> [--addr :7777]
//       Run an HTTP server that exposes the filesystem registry at <dir>
//       through the wire endpoints in docs/REGISTRY.md. Useful for
//       module authors testing their releases before publishing, for
//       CI, and as the reference implementation against which the
//       production Node.js registry will be validated.
package cmd

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sectersion/hackermode/internal/registry"
)

func newRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Registry tooling (serve a local registry directory, ...)",
	}
	cmd.AddCommand(newRegistryServeCmd())
	return cmd
}

func newRegistryServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve <dir>",
		Short: "Serve a filesystem-backed registry over HTTP",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			return registry.ServeAddr(addr, root)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":7777", "address to listen on")
	return cmd
}
