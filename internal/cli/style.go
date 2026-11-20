package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Shared palette (also used by the TUI). Light/dark variants follow the terminal background.
var (
	Pine  = lipgloss.AdaptiveColor{Light: "#0F5C5A", Dark: "#5FC2B8"}
	Amber = lipgloss.AdaptiveColor{Light: "#9A6A0B", Dark: "#F0BE5A"}
	Red   = lipgloss.AdaptiveColor{Light: "#B3402E", Dark: "#F07A65"}
	Green = lipgloss.AdaptiveColor{Light: "#2E7D4F", Dark: "#7DD69A"}
	Dim   = lipgloss.AdaptiveColor{Light: "#5D6B69", Dark: "#6E7C7A"}

	sBold  = lipgloss.NewStyle().Bold(true)
	sDim   = lipgloss.NewStyle().Foreground(Dim)
	sPine  = lipgloss.NewStyle().Foreground(Pine)
	sAmber = lipgloss.NewStyle().Foreground(Amber)
	sRed   = lipgloss.NewStyle().Foreground(Red)
	sGreen = lipgloss.NewStyle().Foreground(Green)
	sBadge = lipgloss.NewStyle().Bold(true).Padding(0, 1)
)

// NoColor turns styling off.
func NoColor() { lipgloss.SetColorProfile(termenv.Ascii) }

func isTTY(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func badge(text string, c lipgloss.TerminalColor) string {
	return sBadge.Background(c).Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#101615"}).Render(text)
}

func okMark() string   { return sGreen.Render("✔") }
func failMark() string { return sRed.Render("✘") }

// errorOut prints an error and its fix, if any.
func errorOut(msg, fix string) {
	fmt.Fprintln(os.Stderr, failMark()+" "+sRed.Bold(true).Render(msg))
	if fix != "" {
		lines := strings.Split(fix, "\n")
		fmt.Fprintln(os.Stderr, "  "+sDim.Render("Fix:")+" "+lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintln(os.Stderr, "       "+l)
		}
	}
}

func warnOut(msg string) {
	fmt.Fprintln(os.Stderr, sAmber.Render("! "+msg))
}
