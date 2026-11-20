package tui

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"fckdigression/internal/core"
)

// Renders every screen with fake data: catches panics, and `SHOW=1 go test -v` prints the layout.
func TestScreensRender(t *testing.T) {
	phone := &core.Phone{Serial: "8a3f21c0", Model: "Google Pixel 8", Android: "15", SDK: 34}
	st := core.ParseStatus("app_version=2 owner=true dns_mode=hostname dns=dns.example.com lock_dns=true lock_users=true lock_reset=true lock_vpn=true lock_debug=false code_set=true code_len=30 fails=0 wait_s=0")
	show := func(name string, m *model) {
		out := m.View()
		if os.Getenv("SHOW") != "" {
			t.Logf("\n=== %s ===\n%s", name, out)
		}
	}
	m := newModel("")
	m.width = 80
	show("waiting", m)

	m.phone, m.st, m.scr = phone, st, scDash
	m.buildMenu()
	show("dashboard", m)

	m.form = lockForm(st)
	m.scr = scForm
	m.form.get("keep").on = false
	m.form.focus = 2
	for _, c := range "12345678901234567890123" {
		m.form.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
	}
	show("lock form", m)

	m.form = unlockForm(false, st)
	for _, c := range "123456789012" {
		m.form.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
	}
	m.form.err = m.form.validate().Error()
	show("unlock form", m)

	m.scr, m.runTitle = scRunning, "Lock this phone"
	m.steps = []stepState{{2, "Google Pixel 8 · Android 15 · 8a3f21c0"}, {3, "app is already device owner, updating the lock"}, {2, "fckdigression app v2 installed"}, {3, "already device owner"}, {1, "Private DNS → dns.example.com"}, {0, ""}}
	show("running", m)

	m.scr, m.resOK, m.resText = scResult, false, "This phone has 2 account(s): com.google, com.whatsapp."
	m.resFix = "Android only grants device owner on a phone with no accounts."
	show("result", m)
}

// TestDumpScreens writes the dashboard and a finished lock as ANSI text, for the README screenshots.
// Run with: DUMP_SCREENS=docs/screenshots/src go test ./internal/tui -run TestDumpScreens
func TestDumpScreens(t *testing.T) {
	dir := os.Getenv("DUMP_SCREENS")
	if dir == "" {
		t.Skip("set DUMP_SCREENS to a folder")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	phone := &core.Phone{Serial: "8a3f21c0", Model: "Google Pixel 8", Android: "15", SDK: 35}
	st := core.ParseStatus("app_version=2 owner=true dns_mode=hostname dns=dns.example.com lock_dns=true lock_users=true lock_reset=true lock_vpn=true lock_debug=false code_set=true code_len=30 fails=0 wait_s=0")
	m := newModel("")
	m.width = 76
	m.phone, m.st, m.scr = phone, st, scDash
	m.buildMenu()
	write := func(name string) {
		if err := os.WriteFile(dir+"/"+name, []byte(m.View()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tui-dashboard.ansi")

	m.scr, m.runTitle, m.resOK, m.resText = scResult, "Lock this phone", true, "Phone locked. You can add accounts now."
	m.steps = []stepState{
		{2, "Google Pixel 8 · Android 15 · 8a3f21c0"}, {2, "none"}, {2, "fckdigression app v2 installed"},
		{2, "app is device owner"}, {2, "Private DNS → dns.example.com · blocked: Private DNS, extra users, factory reset, VPN"},
		{2, "turned off"},
	}
	write("tui-locked.ansi")
}
