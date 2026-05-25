// Package cmd — `hackermode new module <name>`: scaffold a brand-new
// module project.
//
// Lays down:
//
//   <name>/
//   ├── go.mod
//   ├── hackermode.toml
//   ├── main.go               — stream-mode skeleton using pkg/module
//   ├── README.md
//   └── .gitignore
//
// The module's ID defaults to "local.<name>"; the user can edit
// hackermode.toml after the fact. Phase D's `hackermode publish` will
// reject unnamespaced or reserved IDs.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const moduleMainTemplate = `// Command %[1]s is a hackermode module scaffolded by hackermode new module.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/sectersion/hackermode/pkg/module"
)

func main() {
	err := module.RunStream(context.Background(),
		module.Info{
			ID:      "%[2]s",
			Version: "0.1.0",
		},
		func(ctx context.Context, h module.Host, in io.Reader, out io.Writer) error {
			fmt.Fprintln(out, "%[1]s ready — type something and press enter.")
			sc := bufio.NewScanner(in)
			for sc.Scan() {
				fmt.Fprintf(out, "you said: %%s\n", sc.Text())
			}
			return sc.Err()
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "%[1]s:", err)
		os.Exit(1)
	}
}
`

const moduleManifestTemplate = `[module]
id          = "%[2]s"
name        = "%[1]s"
version     = "0.1.0"
description = "TODO: describe %[1]s"
license     = "MIT"

[entry]
binary = "%[1]s"
mode   = "stream"

[capabilities]
required = []

[[commands]]
id    = "%[2]s.greet"
title = "%[1]s: Greet"
hint  = "Say hello"
tags  = ["demo"]
`

const moduleGoModTemplate = `module local/%[1]s

go 1.22

require github.com/sectersion/hackermode v0.0.0-dev
`

const moduleReadmeTemplate = `# %[1]s

A hackermode module.

## Build

    go build -o %[1]s .

## Run from the host

    hackermode link /absolute/path/to/this/dir
    hackermode dev run /absolute/path/to/this/dir
`

const moduleGitignore = `# Compiled binary
%[1]s

# Go build artifacts
*.test
*.out
`

func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Scaffold a new module or related project",
	}
	cmd.AddCommand(newNewModuleCmd())
	return cmd
}

func newNewModuleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "module <name>",
		Short: "Scaffold a new hackermode module project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateModuleName(name); err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			dir := filepath.Join(cwd, name)
			if _, err := os.Stat(dir); err == nil {
				return fmt.Errorf("%s already exists", dir)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			id := "local." + name
			files := map[string]string{
				"main.go":         fmt.Sprintf(moduleMainTemplate, name, id),
				"hackermode.toml": fmt.Sprintf(moduleManifestTemplate, name, id),
				"go.mod":          fmt.Sprintf(moduleGoModTemplate, name),
				"README.md":       fmt.Sprintf(moduleReadmeTemplate, name),
				".gitignore":      fmt.Sprintf(moduleGitignore, name),
			}
			for rel, body := range files {
				if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created module skeleton at %s\n", dir)
			fmt.Fprintf(cmd.OutOrStdout(), "next: cd %s && go build -o %s . && hackermode dev run %s\n",
				name, name, dir)
			return nil
		},
	}
}

func validateModuleName(name string) error {
	if name == "" {
		return errors.New("module name required")
	}
	if strings.ContainsAny(name, "/\\") {
		return errors.New("module name must not contain path separators")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
		default:
			return fmt.Errorf("invalid module name %q (use lowercase letters, digits, -, _)", name)
		}
	}
	return nil
}
