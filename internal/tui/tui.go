// Package tui is the full-screen fckd interface (Bubble Tea).
package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"fckdigression/internal/apk"
	"fckdigression/internal/cli"
	"fckdigression/internal/core"
)

// Run starts the TUI and returns an exit code.
func Run(serial string) int {
	m := newModel(serial)
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// ---------------------------------------------------------------- styles

var (
	sBold  = lipgloss.NewStyle().Bold(true)
	sDim   = lipgloss.NewStyle().Foreground(cli.Dim)
	sPine  = lipgloss.NewStyle().Foreground(cli.Pine)
	sAmber = lipgloss.NewStyle().Foreground(cli.Amber)
	sRed   = lipgloss.NewStyle().Foreground(cli.Red)
	sGreen = lipgloss.NewStyle().Foreground(cli.Green)
	sSel   = lipgloss.NewStyle().Foreground(cli.Pine).Bold(true)
	sBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cli.Dim).Padding(0, 2)
	sBadge = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#101615"})
)

func badge(t string, c lipgloss.TerminalColor) string { return sBadge.Background(c).Render(t) }

// ---------------------------------------------------------------- model

type screen int

const (
	scWaiting screen = iota
	scDash
	scForm
	scRunning
	scResult
)

type action int

const (
	actLock action = iota
	actUnlock
	actRelease
)

type menuItem struct {
	label, hint string
	act         action
}

type stepState struct {
	state  int // 0 pending, 1 running, 2 done, 3 skipped
	detail string
}

type model struct {
	serial  string
	scr     screen
	width   int
	spin    spinner.Model
	phone   *core.Phone
	st      core.Status
	connErr error
	probing bool

	menu []menuItem
	cur  int

	form *form

	// running
	runTitle string
	steps    []stepState
	warns    []string
	ch       chan tea.Msg

	// result
	resOK   bool
	resText string
	resFix  string
}

type probeMsg struct {
	phone *core.Phone
	st    core.Status
	err   error
}
type tickMsg struct{}
type eventMsg core.Event
type doneMsg struct {
	err  error
	text string
}

func newModel(serial string) *model {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = sPine
	return &model{serial: serial, spin: s, width: 80}
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.probe()) }

func (m *model) probe() tea.Cmd {
	m.probing = true
	serial := m.serial
	return func() tea.Msg {
		p, err := core.Connect(serial)
		if err != nil {
			return probeMsg{err: err}
		}
		st, err := p.Status()
		return probeMsg{phone: p, st: st, err: err}
	}
}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

func waitCh(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *model) buildMenu() {
	m.menu = nil
	switch {
	case !m.st.Owner:
		m.menu = append(m.menu, menuItem{"Lock this phone", "install the app and turn on the DNS guard", actLock})
	case m.st.Locked():
		m.menu = append(m.menu,
			menuItem{"Update lock", "change server or options, update the app", actLock},
			menuItem{"Unlock", "lift the restrictions with the parent code", actUnlock},
			menuItem{"Release", "remove fckdigression entirely", actRelease})
	default:
		m.menu = append(m.menu,
			menuItem{"Lock again", "turn the guards back on", actLock},
			menuItem{"Release", "remove fckdigression entirely", actRelease})
	}
	if m.cur >= len(m.menu) {
		m.cur = 0
	}
}

// ---------------------------------------------------------------- update

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case spinner.TickMsg:
		var c tea.Cmd
		m.spin, c = m.spin.Update(msg)
		return m, c

	case probeMsg:
		m.probing = false
		if m.scr != scWaiting && m.scr != scDash {
			return m, nil
		}
		if msg.phone == nil {
			m.phone, m.connErr, m.scr = nil, msg.err, scWaiting
			return m, tick(1500 * time.Millisecond)
		}
		m.phone, m.st, m.connErr, m.scr = msg.phone, msg.st, msg.err, scDash
		m.buildMenu()
		return m, tick(3 * time.Second)

	case tickMsg:
		if (m.scr == scWaiting || m.scr == scDash) && !m.probing {
			return m, m.probe()
		}
		return m, nil

	case eventMsg:
		e := core.Event(msg)
		switch e.Kind {
		case core.StepStart:
			m.steps[e.Step-1] = stepState{1, e.Detail}
		case core.StepDone:
			m.steps[e.Step-1] = stepState{2, e.Detail}
		case core.StepSkip:
			m.steps[e.Step-1] = stepState{3, e.Detail}
		case core.Warn:
			m.warns = append(m.warns, e.Detail)
		}
		return m, waitCh(m.ch)

	case doneMsg:
		m.scr = scResult
		for i := range m.steps {
			if m.steps[i].state == 1 {
				m.steps[i].state = 4 // failed
			}
		}
		m.resOK = msg.err == nil
		m.resText, m.resFix = msg.text, ""
		if msg.err != nil {
			var ce *core.Error
			if errors.As(msg.err, &ce) {
				m.resText, m.resFix = ce.Msg, ce.Fix
			} else {
				m.resText = msg.err.Error()
			}
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.scr {
		case scWaiting:
			if msg.String() == "q" || msg.String() == "esc" {
				return m, tea.Quit
			}
		case scDash:
			return m.keyDash(msg)
		case scForm:
			return m.keyForm(msg)
		case scResult:
			m.scr = scWaiting
			return m, m.probe()
		}
	}
	return m, nil
}

func (m *model) keyDash(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.cur = (m.cur + len(m.menu) - 1) % len(m.menu)
	case "down", "j":
		m.cur = (m.cur + 1) % len(m.menu)
	case "r":
		if !m.probing {
			return m, m.probe()
		}
	case "enter", " ":
		switch m.menu[m.cur].act {
		case actLock:
			m.form = lockForm(m.st)
		case actUnlock:
			m.form = unlockForm(false, m.st)
		case actRelease:
			m.form = unlockForm(true, m.st)
		}
		m.scr = scForm
		return m, m.form.focusCmd()
	}
	return m, nil
}

func (m *model) keyForm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.form
	switch k.String() {
	case "esc":
		m.scr = scDash
		return m, m.probe()
	case "up", "shift+tab":
		f.move(-1)
		return m, f.focusCmd()
	case "down", "tab":
		f.move(1)
		return m, f.focusCmd()
	case "enter":
		if f.onSubmit() {
			if err := f.validate(); err != nil {
				f.err = err.Error()
				return m, nil
			}
			return m, m.start(f)
		}
		f.move(1)
		return m, f.focusCmd()
	}
	return m, f.key(k)
}

// start runs the form's action in the background, streaming progress.
func (m *model) start(f *form) tea.Cmd {
	m.ch = make(chan tea.Msg, 32)
	m.warns = nil
	m.scr = scRunning
	phone := m.phone
	ch := m.ch

	switch f.act {
	case actLock:
		m.runTitle = "Lock this phone"
		m.steps = make([]stepState, len(core.LockSteps))
		o := f.lockOptions()
		go func() {
			defer close(ch)
			if !apk.Available() {
				ch <- doneMsg{err: &core.Error{Msg: "This fckd was built without the app inside.", Fix: "Run make in the fckdigression folder."}}
				return
			}
			path, err := apk.WriteTemp()
			if err != nil {
				ch <- doneMsg{err: err}
				return
			}
			defer os.Remove(path)
			o.APK = path
			err = phone.Lock(o, func(e core.Event) { ch <- eventMsg(e) })
			text := "Phone locked. You can add accounts now."
			if o.AllowDebug {
				text = "Phone locked. USB debugging is still on."
			}
			ch <- doneMsg{err: err, text: text}
		}()
	default:
		release := f.act == actRelease
		m.runTitle = "Unlock"
		if release {
			m.runTitle = "Release"
		}
		m.steps = []stepState{{1, "checking the parent code"}}
		code := f.code()
		go func() {
			defer close(ch)
			_, err := phone.Unlock(code, release)
			text := "Unlocked. Settings can be changed until you lock again."
			if release {
				text = "Lock removed and app uninstalled."
			}
			ch <- doneMsg{err: err, text: text}
		}()
	}
	return tea.Batch(waitCh(ch), m.spin.Tick)
}

// ---------------------------------------------------------------- view

func (m *model) View() string {
	var title, right, body, keys string
	switch m.scr {
	case scWaiting:
		title, right, body, keys = "fckdigression", "v"+cli.Version, m.viewWaiting(), "q quit"
	case scDash:
		title, right, body, keys = "fckdigression", "v"+cli.Version, m.viewDash(), "↑↓ move · enter select · r refresh · q quit"
	case scForm:
		title, body, keys = m.form.title, m.form.view(m.width), "↑↓/tab move · space toggle · enter next · esc back"
	case scRunning:
		title, body, keys = m.runTitle, m.viewRunning(), "working…"
	case scResult:
		title, body, keys = m.runTitle, m.viewResult(), "any key to continue"
	}
	return m.frame(title, right, body, keys)
}

func (m *model) frame(title, right, body, keys string) string {
	w := min(max(m.width-2, 40), 72)
	head := sBold.Render(title)
	if right != "" {
		gap := w - 6 - lipgloss.Width(title) - lipgloss.Width(right)
		head += strings.Repeat(" ", max(gap, 1)) + sDim.Render(right)
	}
	box := sBox.Width(w - 2).Render(head + "\n\n" + body)
	return "\n" + box + "\n  " + sDim.Render(colorKeys(keys)) + "\n"
}

func colorKeys(k string) string { return k }

// hang wraps text to width, indenting continuation lines under the first line's text.
func hang(prefix, text string, indent, width int) string {
	body := lipgloss.NewStyle().Width(max(width-indent, 20)).Render(text)
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
		if i > 0 {
			lines[i] = strings.Repeat(" ", indent) + lines[i]
		}
	}
	return prefix + strings.Join(lines, "\n")
}

func (m *model) inner() int { return min(max(m.width-2, 40), 72) - 6 }

func (m *model) viewWaiting() string {
	var b strings.Builder
	b.WriteString(m.spin.View() + " Waiting for a phone over USB…\n\n")
	var ce *core.Error
	if errors.As(m.connErr, &ce) && ce.Msg != "No phone found." {
		b.WriteString(sAmber.Render(ce.Msg) + "\n")
		if ce.Fix != "" {
			b.WriteString(sDim.Render(ce.Fix) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(sDim.Render("1.") + " Settings → About → tap the build number 7 times\n")
	b.WriteString(sDim.Render("2.") + " Developer options → turn on USB debugging\n")
	b.WriteString("   " + sDim.Render(`Some phones have more switches to turn on, such as`) + "\n")
	b.WriteString("   " + sDim.Render(`"Install via USB" or "USB debugging (Security settings)"`) + "\n")
	b.WriteString(sDim.Render("3.") + " Plug in the cable and tap Allow on the phone\n")
	return b.String()
}

func pad(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func (m *model) viewDash() string {
	var b strings.Builder
	st, p := m.st, m.phone
	var bd string
	switch {
	case !st.Installed:
		bd = badge("NOT INSTALLED", cli.Dim)
	case !st.Owner:
		bd = badge("NOT SET UP", cli.Amber)
	case st.Locked():
		bd = badge("LOCKED", cli.Pine)
	default:
		bd = badge("UNLOCKED", cli.Amber)
	}
	b.WriteString(sBold.Render(p.Model) + "   " + bd + "\n")
	b.WriteString(sDim.Render(fmt.Sprintf("Android %s · SDK %d · %s", p.Android, p.SDK, p.Serial)) + "\n\n")

	if m.connErr != nil {
		b.WriteString(sAmber.Render("! "+m.connErr.Error()) + "\n\n")
	}
	if st.Owner {
		dnsDot := sGreen.Render("● dns")
		if !st.LockDNS {
			dnsDot = sAmber.Render("◐ dns")
		}
		b.WriteString(pad(sDim.Render("Guards"), 14) + dnsDot + "   " + sDim.Render("○ apps   ○ schedule   later") + "\n")
		host := st.DNS
		if host == "" {
			host = "not set"
		}
		b.WriteString(pad(sDim.Render("DNS server"), 14) + host + "\n\n")

		cell := func(label string, on bool) string {
			dot, val := sGreen.Render("●"), "blocked"
			if !on {
				dot, val = sAmber.Render("●"), "allowed"
			}
			return dot + " " + pad(label, 15) + pad(val, 10)
		}
		b.WriteString(cell("DNS changes", st.LockDNS) + "  " + cell("Factory reset", st.LockReset) + "\n")
		b.WriteString(cell("VPN", st.LockVPN) + "  " + cell("USB debugging", st.LockDebug) + "\n")
		code := sAmber.Render("●") + " " + pad("Parent code", 15) + "not set"
		if st.CodeSet {
			c := "set"
			if st.CodeLen > 0 {
				c = fmt.Sprintf("set · %d digits", st.CodeLen)
			}
			code = sGreen.Render("●") + " " + pad("Parent code", 15) + c
		}
		if st.UsersKnown {
			b.WriteString(cell("Extra users", st.LockUsers) + "  " + code + "\n")
		} else {
			b.WriteString(code + "\n")
		}
		if st.WaitS > 0 {
			b.WriteString(sAmber.Render(fmt.Sprintf("  Too many wrong codes: next try in %d min", st.WaitS/60+1)) + "\n")
		}
		if st.AppVersion < 2 {
			b.WriteString("\n" + sAmber.Render("App v1 on the phone.") + sDim.Render(" Unlock, then Update lock to install v2.") + "\n")
		}
	} else {
		b.WriteString(sDim.Render("fckdigression isn't set up on this phone yet.") + "\n")
		b.WriteString(sDim.Render("The phone must have no accounts (Google, WhatsApp, the phone maker's…) for this.") + "\n")
	}

	b.WriteString("\n" + sDim.Render(strings.Repeat("─", 40)) + "\n")
	for i, it := range m.menu {
		line := "  " + pad(it.label, 16) + sDim.Render(it.hint)
		if i == m.cur {
			line = sSel.Render("› "+pad(it.label, 16)) + sDim.Render(it.hint)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *model) viewRunning() string {
	var b strings.Builder
	names := core.LockSteps
	if len(m.steps) == 1 {
		names = []string{"Parent code"}
	}
	for i, s := range m.steps {
		var mark, name, det string
		switch s.state {
		case 0:
			mark, name = sDim.Render("○"), sDim.Render(names[i])
		case 1:
			mark, name, det = m.spin.View(), sBold.Render(names[i]), sDim.Render(s.detail+"…")
		case 2:
			mark, name, det = sGreen.Render("✔"), names[i], sDim.Render(s.detail)
		case 3:
			mark, name, det = sDim.Render("–"), names[i], sDim.Render(s.detail)
		case 4:
			mark, name, det = sRed.Render("✘"), names[i], sDim.Render(s.detail)
		}
		b.WriteString(hang(mark+" "+pad(name, 15), det, lipgloss.Width(mark)+16, m.inner()) + "\n")
	}
	for _, w := range m.warns {
		b.WriteString("\n" + sAmber.Render("! "+w))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *model) viewResult() string {
	var b strings.Builder
	if len(m.steps) > 1 {
		b.WriteString(m.viewRunning() + "\n\n")
	}
	if m.resOK {
		b.WriteString(sGreen.Render("✔ ") + sBold.Render(m.resText))
		if m.runTitle == "Lock this phone" {
			b.WriteString("\n  " + sDim.Render("To unlock, open fckd on the phone and enter the parent code."))
		}
	} else {
		b.WriteString(hang(sRed.Render("✘ "), sRed.Bold(true).Render(m.resText), 2, m.inner()))
		if m.resFix != "" {
			b.WriteString("\n" + hang("  "+sDim.Render("Fix: "), m.resFix, 7, m.inner()))
		}
	}
	return b.String()
}

// ---------------------------------------------------------------- forms

type fieldKind int

const (
	fkText fieldKind = iota
	fkToggle
	fkCode
	fkButton
)

type field struct {
	kind   fieldKind
	key    string
	label  string
	hint   string
	input  textinput.Model
	on     bool
	digits []byte
	hidden func() bool
}

type form struct {
	title   string
	act     action
	fields  []*field
	focus   int
	err     string
	codeLen int
}

func (f *form) get(key string) *field {
	for _, fl := range f.fields {
		if fl.key == key {
			return fl
		}
	}
	return nil
}

func (f *form) visible(i int) bool {
	fl := f.fields[i]
	return fl.hidden == nil || !fl.hidden()
}

func (f *form) move(d int) {
	for n := 0; n < len(f.fields); n++ {
		f.focus = (f.focus + d + len(f.fields)) % len(f.fields)
		if f.visible(f.focus) {
			break
		}
	}
}

func (f *form) onSubmit() bool { return f.fields[f.focus].kind == fkButton }

func (f *form) focusCmd() tea.Cmd {
	var cmd tea.Cmd
	for i, fl := range f.fields {
		if fl.kind != fkText {
			continue
		}
		if i == f.focus {
			cmd = fl.input.Focus()
		} else {
			fl.input.Blur()
		}
	}
	return cmd
}

func (f *form) key(k tea.KeyMsg) tea.Cmd {
	f.err = ""
	fl := f.fields[f.focus]
	switch fl.kind {
	case fkText:
		var c tea.Cmd
		fl.input, c = fl.input.Update(k)
		return c
	case fkToggle:
		if k.String() == " " || k.String() == "x" {
			fl.on = !fl.on
		}
	case fkCode:
		switch s := k.String(); {
		case s == "backspace":
			if len(fl.digits) > 0 {
				fl.digits = fl.digits[:len(fl.digits)-1]
			}
		case s == "ctrl+u":
			fl.digits = nil
		case len(s) == 1 && s[0] >= '0' && s[0] <= '9':
			if len(fl.digits) < core.MaxCode {
				fl.digits = append(fl.digits, s[0])
			}
		}
	}
	return nil
}

func (f *form) view(width int) string {
	var b strings.Builder
	for i, fl := range f.fields {
		if !f.visible(i) {
			continue
		}
		focused := i == f.focus
		cursor := "  "
		if focused {
			cursor = sSel.Render("› ")
		}
		switch fl.kind {
		case fkText:
			b.WriteString(cursor + pad(fl.label, 12) + fl.input.View() + "\n")
		case fkToggle:
			box := sDim.Render("[ ]")
			if fl.on {
				box = sGreen.Render("[x]")
			}
			b.WriteString(cursor + box + " " + fl.label)
			if fl.hint != "" {
				b.WriteString(" " + sDim.Render(fl.hint))
			}
			b.WriteString("\n")
		case fkCode:
			dots := core.Group(strings.Repeat("•", len(fl.digits)))
			if focused {
				dots = sPine.Render(dots) + sDim.Render("_")
			} else if len(fl.digits) == 0 {
				dots = sDim.Render("·")
			}
			count := ""
			if len(fl.digits) > 0 {
				if f.codeLen > 0 {
					count = sDim.Render(fmt.Sprintf("  %d/%d", len(fl.digits), f.codeLen))
				} else {
					count = sDim.Render(fmt.Sprintf("  %d", len(fl.digits)))
				}
			}
			b.WriteString(cursor + pad(fl.label, 12) + dots + count + "\n")
			if fl.hint != "" && focused {
				b.WriteString("    " + sDim.Render(fl.hint) + "\n")
			}
		case fkButton:
			btn := lipgloss.NewStyle().Padding(0, 2).Render(fl.label)
			if focused {
				btn = lipgloss.NewStyle().Padding(0, 2).Bold(true).Background(cli.Pine).
					Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#101615"}).Render(fl.label)
			}
			b.WriteString("\n" + cursor + btn + "\n")
		}
	}
	if f.err != "" {
		b.WriteString("\n" + sRed.Render("✘ "+f.err))
	}
	return strings.TrimRight(b.String(), "\n")
}

func toggle(key, label, hint string, on bool) *field {
	return &field{kind: fkToggle, key: key, label: label, hint: hint, on: on}
}

func lockForm(st core.Status) *form {
	dns := textinput.New()
	dns.Placeholder = "dns.example.com"
	dns.SetValue(st.DNS)
	dns.CharLimit = 253
	dns.Width = 40
	dns.Prompt = ""

	f := &form{title: "Lock this phone", act: actLock}
	keepable := st.Owner && st.CodeSet
	keep := toggle("keep", "Keep the current parent code", "(no new code to send)", keepable)
	keep.hidden = func() bool { return !keepable }
	noKeep := func() bool { return keepable && keep.on }
	f.fields = []*field{
		{kind: fkText, key: "dns", label: "DNS server", input: dns},
		keep,
		{kind: fkCode, key: "code", label: "Code", hint: fmt.Sprintf("Digits only, %d or more. Typed by the person who keeps it.", core.MinCode), hidden: noKeep},
		{kind: fkCode, key: "confirm", label: "Confirm", hidden: noKeep},
		toggle("vpn", "Block VPN apps", "(a VPN bypasses Private DNS)", !(st.Owner && !st.LockVPN && st.LockDNS)),
		toggle("reset", "Block factory reset", "", true),
		toggle("debug", "Turn off USB debugging", "(else adb can undo the lock)", true),
		{kind: fkButton, key: "go", label: "Lock"},
	}
	if st.Owner {
		f.title = "Update lock"
		f.fields[len(f.fields)-1].label = "Update lock"
	}
	return f
}

func unlockForm(release bool, st core.Status) *form {
	f := &form{title: "Unlock", act: actUnlock, codeLen: st.CodeLen}
	f.fields = []*field{
		{kind: fkCode, key: "code", label: "Parent code", hint: "Have it read out to you in groups of 5."},
	}
	if release {
		f.title, f.act = "Release", actRelease
		f.fields = append(f.fields, toggle("sure", "I understand locking again needs a factory reset", "", false))
		f.fields = append(f.fields, &field{kind: fkButton, key: "go", label: "Remove fckdigression"})
	} else {
		f.fields = append(f.fields, &field{kind: fkButton, key: "go", label: "Unlock"})
	}
	return f
}

func (f *form) code() string { return string(f.get("code").digits) }

func (f *form) validate() error {
	switch f.act {
	case actLock:
		dns := strings.TrimSpace(f.get("dns").input.Value())
		if dns == "" {
			return errors.New("Enter a DNS-over-TLS server.")
		}
		if !core.ValidHost(dns) {
			return fmt.Errorf("“%s” isn't a valid server name.", dns)
		}
		if f.get("code").hidden() {
			return nil
		}
		if err := core.CheckCode(f.code()); err != nil {
			return err
		}
		if f.code() != string(f.get("confirm").digits) {
			return errors.New("The two codes don't match.")
		}
	default:
		c := f.code()
		if c == "" {
			return errors.New("Enter the parent code.")
		}
		if f.codeLen > 0 && len(c) != f.codeLen {
			return fmt.Errorf("The code has %d digits, you typed %d. Nothing was sent.", f.codeLen, len(c))
		}
		if f.act == actRelease && !f.get("sure").on {
			return errors.New("Tick the box to confirm.")
		}
	}
	return nil
}

func (f *form) lockOptions() core.LockOptions {
	code := f.code()
	return core.LockOptions{
		DNS:        strings.TrimSpace(f.get("dns").input.Value()),
		KeepCode:   f.get("code").hidden(),
		AllowVPN:   !f.get("vpn").on,
		AllowReset: !f.get("reset").on,
		AllowDebug: !f.get("debug").on,
		Code:       func() (string, error) { return code, nil },
	}
}
