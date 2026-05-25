package main

import (
	"os"
	"path/filepath"

	"github.com/sectersion/hackermode/cmd"
)

// hackermode entrypoint. The same binary may be invoked as `hm` via a
// symlink; argv[0]'s basename is passed through to cobra so help/version
// output reflect the alias the user typed.
func main() {
	argv0 := filepath.Base(os.Args[0])
	cmd.Execute(argv0)
}
