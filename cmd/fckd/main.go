// fckd — lock distractions out of an Android phone. With no arguments, opens the TUI.
package main

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"fckdigression/internal/cli"
	"fckdigression/internal/tui"
)

func main() {
	var g cli.Global
	var rest []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-s", "--serial":
			if i+1 < len(args) {
				i++
				g.Serial = args[i]
			}
		case "--no-color":
			g.NoColor = true
		case "-h", "--help", "help":
			cli.Usage()
			return
		case "--version", "version":
			fmt.Println("fckd " + cli.Version)
			return
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || rest[0] == "tui" {
		if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
			cli.Usage()
			os.Exit(2)
		}
		os.Exit(tui.Run(g.Serial))
	}
	os.Exit(cli.Run(g, rest[0], rest[1:]))
}
