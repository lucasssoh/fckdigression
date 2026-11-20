// Package cli implements the fckd command line.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"fckdigression/internal/apk"
	"fckdigression/internal/build"
	"fckdigression/internal/core"
)

// Version of the fckd tool.
const Version = "2.0"

// Global options.
type Global struct {
	Serial  string
	NoColor bool
}

// Usage prints the help.
func Usage() {
	b, d, p, a := sBold.Render, sDim.Render, sPine.Render, sAmber.Render
	fmt.Printf(`%s — lock distractions out of an Android phone

%s
  fckd                         open the interactive TUI
  fckd <command> [options]

%s
  %s [guards]      install the app and turn on the guards you pick
  %s             show the lock state of the connected phone
  %s             lift the lock (needs the parent code)
  %s            lift the lock and let the app be uninstalled
  %s              compile the Android app

%s
  --dns HOST         lock Private DNS to a DNS-over-TLS server
  %s

%s
  --keep-code        keep the parent code already on the phone (to update the app)
  --generate-code    make a random %d-digit parent code and show it once
  --allow-reset      keep factory reset available in Settings
  --allow-vpn        allow VPN apps %s
  --allow-debug      leave USB debugging on %s
  --apk FILE         install this APK instead of the built-in one

%s
  --json             machine-readable output

%s
  -s, --serial ID    pick a phone when several are connected
      --no-color     plain output
  -h, --help         show this help
      --version      show the version

%s
  Typed hidden, %d+ digits. For scripts, set FCKD_CODE. Only a salted hash goes to the phone.

%s
  fckd lock --dns dns.example.com    %s
  fckd lock --keep-code              %s
`,
		b("fckdigression "+Version),
		b("Usage"),
		b("Commands"), p("lock"), p("status"), p("unlock"), p("release"), p("build"),
		b("Guards"), d("(more guards later: apps, schedule…)"),
		b("Lock options"), core.GenLength, a("(a VPN bypasses Private DNS)"), a("(adb can undo the lock)"),
		b("Status options"),
		b("Global"),
		b("Parent code"), core.MinCode,
		b("Examples"), d("# any DNS-over-TLS server"), d("# update the app, same server and code"),
	)
}

// Run executes a command and returns the exit code.
func Run(g Global, cmd string, args []string) int {
	if g.NoColor || os.Getenv("NO_COLOR") != "" {
		NoColor()
	}
	var err error
	switch cmd {
	case "lock":
		err = cmdLock(g, args)
	case "status":
		err = cmdStatus(g, args)
	case "unlock":
		err = cmdUnlock(g, false)
	case "release":
		err = cmdUnlock(g, true)
	case "build":
		err = cmdBuild(args)
	default:
		errorOut("Unknown command: "+cmd, "See fckd --help.")
		return 2
	}
	if err != nil {
		var ce *core.Error
		if errors.As(err, &ce) {
			errorOut(ce.Msg, ce.Fix)
		} else {
			errorOut(err.Error(), "")
		}
		return 1
	}
	return 0
}

// ---------------------------------------------------------------- lock

func cmdLock(g Global, args []string) error {
	o := core.LockOptions{}
	var apkPath string
	generate := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := func() (string, error) {
			if _, v, ok := strings.Cut(a, "="); ok {
				return v, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch {
		case a == "--dns" || strings.HasPrefix(a, "--dns="):
			o.DNS, err = val()
		case a == "--apk" || strings.HasPrefix(a, "--apk="):
			apkPath, err = val()
		case a == "--allow-reset":
			o.AllowReset = true
		case a == "--allow-vpn":
			o.AllowVPN = true
		case a == "--allow-debug":
			o.AllowDebug = true
		case a == "--keep-code":
			o.KeepCode = true
		case a == "--generate-code":
			generate = true
		default:
			err = &core.Error{Msg: "Unknown option: " + a, Fix: "See fckd --help."}
		}
		if err != nil {
			return err
		}
	}
	if o.KeepCode && generate {
		return &core.Error{Msg: "--keep-code and --generate-code don't go together.", Fix: "Pick one."}
	}

	if o.DNS != "" && !core.ValidHost(o.DNS) {
		return &core.Error{Msg: "“" + o.DNS + "” isn't a valid server name.", Fix: "Use a DNS-over-TLS hostname, like dns.example.com."}
	}
	if apkPath == "" {
		if !apk.Available() {
			return &core.Error{Msg: "This fckd was built without the app inside.", Fix: "Run make in the fckdigression folder, or pass --apk FILE."}
		}
		p, err := apk.WriteTemp()
		if err != nil {
			return err
		}
		defer os.Remove(p)
		apkPath = p
	}
	o.APK = apkPath

	fmt.Println()
	fmt.Println(sBold.Render("fckdigression "+Version) + " " + sDim.Render("· dns guard"))
	fmt.Println()

	phone, err := core.Connect(g.Serial)
	if err != nil {
		return err
	}

	o.Code = func() (string, error) {
		if c := core.CodeFromEnv(); c != "" {
			return c, nil
		}
		fmt.Println()
		if generate {
			c := core.GenerateCode(core.GenLength)
			fmt.Println("      " + sAmber.Render("New parent code") + " " + sDim.Render("(shown once, give it to the person who keeps it)"))
			fmt.Println()
			fmt.Println("        " + sBold.Render(core.Group(c)))
			fmt.Println()
			fmt.Print("      " + sDim.Render("Press Enter once it's saved somewhere safe… "))
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
			if isTTY(os.Stdout) {
				// wipe the code from the screen (5 lines up)
				fmt.Print("\033[5A\033[J")
				fmt.Println("      " + sAmber.Render("New parent code") + " " + sDim.Render(fmt.Sprintf("%d digits, given to the keeper", len(c))))
			}
			fmt.Println()
			return c, nil
		}
		fmt.Println("      " + sAmber.Render("Parent code") + " " + sDim.Render(fmt.Sprintf("(digits only, %d or more, typed by the person who keeps it)", core.MinCode)))
		c1, err := readCode("      Code:    ")
		if err != nil {
			return "", err
		}
		if err := core.CheckCode(c1); err != nil {
			return "", err
		}
		c2, err := readCode("      Confirm: ")
		if err != nil {
			return "", err
		}
		if c1 != c2 {
			return "", &core.Error{Msg: "The two codes don't match.", Fix: "Run lock again."}
		}
		fmt.Println("      " + sDim.Render("Make sure the keeper has saved it. If it's lost, only a full wipe removes the lock."))
		fmt.Println()
		return c1, nil
	}

	tty := isTTY(os.Stdout)
	total := len(core.LockSteps)
	pending := ""
	stepLine := func(e core.Event) string {
		return fmt.Sprintf("%s %-14s", sPine.Render(fmt.Sprintf("[%d/%d]", e.Step, total)), e.Name)
	}
	err = phone.Lock(o, func(e core.Event) {
		switch e.Kind {
		case core.StepStart:
			if tty {
				pending = stepLine(e) + sDim.Render(e.Detail+"…")
				fmt.Print(pending)
			}
		case core.StepDone, core.StepSkip:
			if tty && pending != "" {
				fmt.Print("\r\033[K")
			}
			pending = ""
			if e.Kind == core.StepDone {
				fmt.Println(stepLine(e) + e.Detail + " " + okMark())
			} else {
				fmt.Println(stepLine(e) + sDim.Render(e.Detail))
			}
		case core.Warn:
			if tty && pending != "" {
				fmt.Print("\r\033[K")
			}
			warnOut(e.Detail)
			if pending != "" {
				fmt.Print(pending)
			}
		}
	})
	if pending != "" {
		fmt.Println()
	}
	if err != nil {
		fmt.Println()
		return err
	}
	fmt.Println()
	fmt.Println(okMark() + " " + sBold.Render("Phone locked.") + " You can add accounts now.")
	fmt.Println("  To unlock, open " + sBold.Render("fckd") + " on the phone and enter the parent code.")
	return nil
}

// readCode reads digits without echoing them, showing a dot per digit in groups of 5.
func readCode(prompt string) (string, error) {
	if c := core.CodeFromEnv(); c != "" {
		return c, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", &core.Error{Msg: "No parent code given.", Fix: "Type it, or set FCKD_CODE."}
		}
		return strings.TrimSpace(line), nil
	}
	fmt.Print(prompt)
	old, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, old)
	var code []byte
	draw := func() {
		dots := core.Group(strings.Repeat("•", len(code)))
		count := ""
		if len(code) > 0 {
			count = "  " + sDim.Render(fmt.Sprintf("%d digits", len(code)))
		}
		fmt.Print("\r\033[K" + prompt + sDim.Render(dots) + count)
	}
	buf := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			return "", err
		}
		switch c := buf[0]; {
		case c == 3: // Ctrl-C
			term.Restore(fd, old)
			fmt.Print("\r\n")
			os.Exit(130)
		case c == '\r' || c == '\n':
			fmt.Print("\r\n")
			return string(code), nil
		case c == 127 || c == 8:
			if len(code) > 0 {
				code = code[:len(code)-1]
			}
		case c == 21: // Ctrl-U
			code = code[:0]
		case c >= '0' && c <= '9':
			if len(code) < core.MaxCode {
				code = append(code, c)
			}
		}
		draw()
	}
}

// ---------------------------------------------------------------- status

func cmdStatus(g Global, args []string) error {
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
		} else {
			return &core.Error{Msg: "Unknown option: " + a, Fix: "See fckd --help."}
		}
	}
	phone, err := core.Connect(g.Serial)
	if err != nil {
		return err
	}
	st, err := phone.Status()
	if err != nil {
		return err
	}
	if asJSON {
		out := struct {
			Serial string `json:"serial"`
			Model  string `json:"model"`
			core.Status
			Locked bool `json:"locked"`
		}{phone.Serial, phone.Model, st, st.Locked()}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	PrintStatus(phone, st)
	return nil
}

// PrintStatus renders the status block.
func PrintStatus(phone *core.Phone, st core.Status) {
	var b string
	switch {
	case !st.Installed:
		b = badge("NOT INSTALLED", Dim)
	case !st.Owner:
		b = badge("NOT SET UP", Amber)
	case st.Locked():
		b = badge("LOCKED", Pine)
	default:
		b = badge("UNLOCKED", Amber)
	}
	fmt.Println()
	fmt.Println(sBold.Render(phone.Model) + "  " + b)
	fmt.Println()
	row := func(k, v string) { fmt.Printf("  %-16s%s\n", k, v) }
	if !st.Installed || !st.Owner {
		fmt.Println("  " + sDim.Render("The app isn't device owner on this phone. Set it up with:"))
		fmt.Println("  fckd lock --dns HOST")
		fmt.Println()
		return
	}
	bl := func(on bool) string {
		if on {
			return sGreen.Render("blocked")
		}
		return sAmber.Render("allowed")
	}
	dns := st.DNS
	if dns == "" {
		dns = "not set"
	}
	row("Private DNS", dns+" "+sDim.Render("("+st.DNSMode+")"))
	row("DNS changes", bl(st.LockDNS))
	row("VPN", bl(st.LockVPN))
	row("Factory reset", bl(st.LockReset))
	dbg := bl(st.LockDebug)
	if !st.LockDebug {
		dbg += "  " + sDim.Render("(you're connected over adb, so yes)")
	}
	row("USB debugging", dbg)
	if st.UsersKnown {
		row("Extra users", bl(st.LockUsers))
	}
	code := sAmber.Render("not set")
	if st.CodeSet {
		code = "set"
		if st.CodeLen > 0 {
			code += sDim.Render(fmt.Sprintf(" · %d digits", st.CodeLen))
		}
		code += sDim.Render(fmt.Sprintf(" · %d failed attempts", st.Fails))
		if st.WaitS > 0 {
			code += sAmber.Render(fmt.Sprintf(" · next try in %d min", st.WaitS/60+1))
		}
	}
	row("Parent code", code)
	app := fmt.Sprintf("v%d", st.AppVersion)
	if st.AppVersion < 2 {
		app += sDim.Render(" · old version, update with: fckd lock --keep-code (unlock first)")
	}
	row("App", app)
	fmt.Println()
}

// ---------------------------------------------------------------- unlock / release

func cmdUnlock(g Global, release bool) error {
	phone, err := core.Connect(g.Serial)
	if err != nil {
		return err
	}
	if release {
		fmt.Println(sAmber.Render("This removes fckdigression from the phone for good."))
		fmt.Println(sDim.Render("Locking again will need a phone with no accounts, so in practice a factory reset."))
		fmt.Print("Type " + sBold.Render("remove") + " to continue: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != "remove" {
			return &core.Error{Msg: "Cancelled.", Fix: ""}
		}
	}
	code, err := readCode("Parent code: ")
	if err != nil {
		return err
	}
	if _, err := phone.Unlock(code, release); err != nil {
		return err
	}
	if release {
		fmt.Println(okMark() + " Lock removed and app uninstalled.")
	} else {
		fmt.Println(okMark() + " Unlocked. Settings can be changed until you lock again.")
	}
	return nil
}

// ---------------------------------------------------------------- build

func cmdBuild(args []string) error {
	src, out := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--src":
			if i+1 < len(args) {
				i++
				src = args[i]
			}
		case "-o", "--out":
			if i+1 < len(args) {
				i++
				out = args[i]
			}
		default:
			return &core.Error{Msg: "Unknown option: " + args[i], Fix: "Usage: fckd build [--src DIR] [-o FILE]"}
		}
	}
	if src == "" {
		var err error
		if src, err = build.FindSource(); err != nil {
			return err
		}
	}
	if out == "" {
		out = filepath.Join(src, "build", "fckd.apk")
	}
	fmt.Println(sBold.Render("Building the Android app"))
	err := build.Build(src, out, func(s string) { fmt.Println("  " + sDim.Render("· "+s)) })
	if err != nil {
		return err
	}
	fmt.Println(okMark() + " " + out)
	return nil
}
