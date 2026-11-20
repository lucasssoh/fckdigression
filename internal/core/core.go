// Package core talks to the fckdigression app on the phone and runs the lock steps.
// It has no output of its own: the CLI and the TUI render the events it reports.
package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fckdigression/internal/adb"
)

// The package id is historical and must not change: the app is device owner on phones in use.
const (
	Pkg       = "fr.lucas.dnslock"
	Admin     = Pkg + "/.AdminReceiver"
	Receiver  = Pkg + "/.CommandReceiver"
	MinCode   = 6
	MaxCode   = 64
	GenLength = 30
	MinSDK    = 29
)

// Error is a failure with an optional fix the user can apply.
type Error struct {
	Msg string
	Fix string
}

func (e *Error) Error() string { return e.Msg }

func fail(msg, fix string) error { return &Error{Msg: msg, Fix: fix} }

// Phone is the connected device.
type Phone struct {
	Serial  string
	Model   string
	Android string
	SDK     int
	adb     *adb.Client
}

// Connect finds exactly one authorized phone. serial may be empty.
func Connect(serial string) (*Phone, error) {
	bin, err := adb.Find()
	if err != nil {
		return nil, fail("adb isn't installed.", "Install it (sudo dnf install android-tools) or set ANDROID_HOME.")
	}
	c := &adb.Client{Bin: bin}
	devs, err := c.Devices()
	if err != nil {
		return nil, fail("adb doesn't respond: "+err.Error(), "Try `adb kill-server`, then run this again.")
	}
	if serial != "" {
		var keep []adb.Device
		for _, d := range devs {
			if d.Serial == serial {
				keep = append(keep, d)
			}
		}
		devs = keep
	}
	switch {
	case len(devs) == 0:
		return nil, fail("No phone found.", "Connect it over USB and turn on USB debugging.")
	case len(devs) > 1:
		var s []string
		for _, d := range devs {
			s = append(s, d.Serial)
		}
		return nil, fail("Several phones are connected: "+strings.Join(s, ", ")+".", "Pick one with --serial.")
	}
	d := devs[0]
	switch d.State {
	case "device":
	case "unauthorized":
		return nil, fail("This computer isn't authorized yet.", `Unlock the phone and tap Allow on the "Allow USB debugging?" prompt.`)
	default:
		return nil, fail("The phone is "+d.State+".", "Unplug it, plug it back in, and check the phone screen.")
	}
	c.Serial = d.Serial
	p := &Phone{Serial: d.Serial, adb: c}
	p.Model = strings.TrimSpace(c.Prop("ro.product.manufacturer") + " " + c.Prop("ro.product.model"))
	p.Android = c.Prop("ro.build.version.release")
	p.SDK, _ = strconv.Atoi(c.Prop("ro.build.version.sdk"))
	return p, nil
}

// ADB exposes the underlying client.
func (p *Phone) ADB() *adb.Client { return p.adb }

var dataRe = regexp.MustCompile(`data="(.*)"`)

// Send broadcasts a command to the app and returns its reply.
// Replies starting with ERROR (or ERREUR, from app version 1) become errors.
func (p *Phone) Send(action string, extras ...string) (string, error) {
	args := append([]string{"am", "broadcast", "-n", Receiver, "-a", Pkg + "." + action}, extras...)
	out, _ := p.adb.Shell(args...)
	var data string
	for _, m := range dataRe.FindAllStringSubmatch(out, -1) {
		data = m[1]
	}
	if data == "" {
		return "", fail("The app didn't answer.", "Is it installed? Output: "+strings.TrimSpace(out))
	}
	for _, pre := range []string{"ERROR:", "ERREUR:"} {
		if strings.HasPrefix(data, pre) {
			return "", fail(strings.TrimSpace(strings.TrimPrefix(data, pre)), "")
		}
	}
	return data, nil
}

// AppInstalled reports whether the app is on the phone.
func (p *Phone) AppInstalled() bool {
	out, _ := p.adb.Shell("pm", "list", "packages", Pkg)
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "package:"+Pkg {
			return true
		}
	}
	return false
}

// DeviceOwner returns the package that owns the device, or "".
func (p *Phone) DeviceOwner() string {
	out, _ := p.adb.Shell("dumpsys", "device_policy")
	i := strings.Index(out, "Device Owner")
	if i < 0 {
		return ""
	}
	rest := out[i:]
	if j := strings.Index(rest, "\n\n"); j > 0 {
		rest = rest[:j]
	}
	if m := regexp.MustCompile(`package=(\S+)`).FindStringSubmatch(rest); m != nil {
		return m[1]
	}
	return ""
}

// Accounts returns the account types on the phone (one entry per account).
func (p *Phone) Accounts() []string {
	out, _ := p.adb.Shell("dumpsys", "account")
	var types []string
	re := regexp.MustCompile(`^\s*Account \{name=[^,]*, type=([^}]*)\}`)
	for _, l := range strings.Split(out, "\n") {
		if m := re.FindStringSubmatch(l); m != nil {
			types = append(types, m[1])
		}
	}
	return types
}

// ---------------------------------------------------------------- status

// Status is the app's view of the lock. Fields are filled from app versions 1 and 2.
type Status struct {
	Installed  bool   `json:"installed"`
	AppVersion int    `json:"app_version"`
	Owner      bool   `json:"owner"`
	DNSMode    string `json:"dns_mode,omitempty"`
	DNS        string `json:"dns,omitempty"`
	LockDNS    bool   `json:"lock_dns"`
	LockVPN    bool   `json:"lock_vpn"`
	LockReset  bool   `json:"lock_reset"`
	LockDebug  bool   `json:"lock_debug"`
	LockUsers  bool   `json:"lock_users"`
	UsersKnown bool   `json:"-"`
	CodeSet    bool   `json:"code_set"`
	CodeLen    int    `json:"code_len,omitempty"`
	Fails      int    `json:"fails"`
	WaitS      int    `json:"wait_s"`
	Raw        string `json:"-"`
}

// Locked means the DNS guard is on.
func (s Status) Locked() bool { return s.Owner && s.LockDNS }

// ParseStatus reads "key=value" pairs, accepting the French keys of app version 1.
func ParseStatus(raw string) Status {
	s := Status{Installed: true, AppVersion: 1, Raw: raw}
	kv := map[string]string{}
	for _, f := range strings.Fields(raw) {
		if k, v, ok := strings.Cut(f, "="); ok {
			kv[k] = v
		}
	}
	get := func(keys ...string) (string, bool) {
		for _, k := range keys {
			if v, ok := kv[k]; ok {
				return v, true
			}
		}
		return "", false
	}
	b := func(keys ...string) bool { v, _ := get(keys...); return v == "true" }
	n := func(keys ...string) int { v, _ := get(keys...); i, _ := strconv.Atoi(v); return i }

	if v, ok := get("app_version"); ok {
		s.AppVersion, _ = strconv.Atoi(v)
	}
	s.Owner = b("owner", "proprietaire")
	mode, _ := get("dns_mode")
	s.DNSMode = map[string]string{"serveur": "hostname", "automatique": "opportunistic", "desactive": "off", "inconnu": "unknown"}[mode]
	if s.DNSMode == "" {
		s.DNSMode = mode
	}
	s.DNS, _ = get("dns")
	s.LockDNS = b("lock_dns", "verrou_dns")
	s.LockVPN = b("lock_vpn", "verrou_vpn")
	s.LockReset = b("lock_reset", "verrou_reset")
	s.LockDebug = b("lock_debug", "verrou_debug")
	_, s.UsersKnown = get("lock_users")
	s.LockUsers = b("lock_users")
	s.CodeSet = b("code_set", "code_defini")
	s.CodeLen = n("code_len")
	s.Fails = n("fails")
	s.WaitS = n("wait_s")
	return s
}

// Status asks the app for its state. A missing app gives Installed=false, no error.
func (p *Phone) Status() (Status, error) {
	if !p.AppInstalled() {
		return Status{}, nil
	}
	raw, err := p.Send("STATUS")
	if err != nil {
		return Status{Installed: true}, err
	}
	return ParseStatus(raw), nil
}

// ---------------------------------------------------------------- code

// CheckCode validates a parent code.
func CheckCode(code string) error {
	if code == "" {
		return fail("The parent code is empty.", "")
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return fail("The parent code must contain digits only.", "")
		}
	}
	if len(code) < MinCode {
		return fail(fmt.Sprintf("The parent code is %d digits long. Use at least %d.", len(code), MinCode),
			"fckd lock --generate-code makes a random 30-digit code.")
	}
	if len(code) > MaxCode {
		return fail(fmt.Sprintf("The parent code is longer than %d digits.", MaxCode), "")
	}
	return nil
}

// GenerateCode returns n random digits.
func GenerateCode(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String()
}

// Group formats digits (or any runes) in groups of 5.
func Group(s string) string {
	var b strings.Builder
	for i, r := range []rune(s) {
		if i > 0 && i%5 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func hashCode(code string) (salt, hash string) {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	salt = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(salt + ":" + code))
	return salt, hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- lock

// Step names, in order, for progress displays.
var LockSteps = []string{"Phone", "Accounts", "App", "Owner", "Lock", "USB debugging"}

// EventKind tells what an Event means.
type EventKind int

const (
	StepStart EventKind = iota // a step begins (Detail says what is happening)
	StepDone                   // a step succeeded
	StepSkip                   // a step wasn't needed
	Warn                       // something to know, not fatal
)

// Event is a progress report from Lock.
type Event struct {
	Kind   EventKind
	Step   int // 1-based index in LockSteps
	Name   string
	Detail string
}

// LockOptions configures Lock.
type LockOptions struct {
	DNS        string // empty: keep the server already set by the app
	AllowReset bool
	AllowVPN   bool
	AllowDebug bool
	KeepCode   bool
	APK        string                 // path to the APK to install
	Code       func() (string, error) // asked only when needed
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?\.[A-Za-z]{2,}$`)

// ValidHost checks a DNS-over-TLS hostname.
func ValidHost(h string) bool { return hostRe.MatchString(h) }

// Lock installs the app, makes it device owner, sets Private DNS and locks it.
func (p *Phone) Lock(o LockOptions, report func(Event)) error {
	n := len(LockSteps)
	start := func(i int, d string) { report(Event{Kind: StepStart, Step: i, Name: LockSteps[i-1], Detail: d}) }
	done := func(i int, d string) { report(Event{Kind: StepDone, Step: i, Name: LockSteps[i-1], Detail: d}) }
	skip := func(i int, d string) { report(Event{Kind: StepSkip, Step: i, Name: LockSteps[i-1], Detail: d}) }
	warn := func(d string) { report(Event{Kind: Warn, Detail: d}) }
	_ = n

	if o.DNS != "" && !ValidHost(o.DNS) {
		return fail("“"+o.DNS+"” isn't a valid server name.", "Use a DNS-over-TLS hostname, like dns.example.com.")
	}

	// 1. phone
	start(1, p.Model)
	if p.SDK < MinSDK {
		return fail(fmt.Sprintf("Android 10 or newer is needed. This phone runs SDK %d.", p.SDK), "")
	}
	done(1, fmt.Sprintf("%s · Android %s · %s", p.Model, p.Android, p.Serial))

	// 2. accounts / current owner
	start(2, "checking")
	owner := p.DeviceOwner()
	before, _ := p.Status()
	if o.DNS == "" {
		o.DNS = before.DNS
		if o.DNS == "" {
			return fail("No DNS server given.", "Add --dns HOST, for example --dns dns.example.com.")
		}
	}
	if owner == Pkg {
		skip(2, "app is already device owner, updating the lock")
	} else {
		if owner != "" {
			return fail("Another app is already device owner ("+owner+").", "Only one device owner is allowed. Remove that app's management first.")
		}
		if acc := p.Accounts(); len(acc) > 0 {
			return fail(fmt.Sprintf("This phone has %d account(s): %s.", len(acc), strings.Join(uniq(acc), ", ")),
				"Android only grants device owner on a phone with no accounts. Remove them in Settings → Accounts,\nor factory-reset and skip sign-in, then run this again.")
		}
		done(2, "none")
	}

	// 3. app
	start(3, "installing")
	out, _ := p.adb.Install(o.APK)
	if !strings.Contains(out, "Success") {
		switch {
		case strings.Contains(out, "INSTALL_FAILED_USER_RESTRICTED"):
			return fail("The phone refused the install.", "In Developer options, allow installing apps over USB.\nSome phones have extra switches for this, such as \"Install via USB\"\nor \"USB debugging (Security settings)\". Turn them on, then run this again.")
		case strings.Contains(out, "INSTALL_FAILED_UPDATE_INCOMPATIBLE"):
			return fail("The app on the phone was signed with another key.", "Build with the original dnslock.keystore (android/dnslock.keystore).")
		case strings.Contains(out, "INSTALL_FAILED_VERSION_DOWNGRADE"):
			return fail("The phone has a newer version of the app.", "Rebuild from the latest sources.")
		}
		return fail("The install failed.", strings.TrimSpace(out))
	}
	st, err := p.Status()
	if err != nil {
		return err
	}
	done(3, fmt.Sprintf("fckdigression app v%d installed", st.AppVersion))

	// 4. owner
	if owner == Pkg {
		skip(4, "already device owner")
	} else {
		start(4, "dpm set-device-owner")
		res, _ := p.adb.Shell("dpm", "set-device-owner", Admin)
		if !strings.Contains(strings.ToLower(res), "success") {
			return fail("Android refused to make the app device owner.",
				strings.TrimSpace(res)+"\nSome phones need an extra developer option for this, such as \"USB debugging (Security settings)\".")
		}
		done(4, "app is device owner")
	}

	// code
	extras := []string{"--es", "dns", o.DNS}
	if o.KeepCode {
		if !st.CodeSet {
			return fail("There is no parent code on the phone to keep.", "Run lock without --keep-code.")
		}
		extras = append(extras, "--ez", "keep_code", "true")
	} else {
		code, err := o.Code()
		if err != nil {
			return err
		}
		if err := CheckCode(code); err != nil {
			return err
		}
		salt, hash := hashCode(code)
		extras = append(extras, "--es", "salt", salt, "--es", "hash", hash, "--ei", "len", strconv.Itoa(len(code)))
	}
	extras = append(extras,
		"--ez", "block_reset", strconv.FormatBool(!o.AllowReset),
		"--ez", "block_vpn", strconv.FormatBool(!o.AllowVPN),
		"--ez", "block_debug", strconv.FormatBool(!o.AllowDebug))

	// 5. lock
	start(5, "Private DNS → "+o.DNS)
	if _, err := p.Send("LOCK", extras...); err != nil {
		return err
	}
	mode, _ := p.adb.Shell("settings", "get", "global", "private_dns_mode")
	spec, _ := p.adb.Shell("settings", "get", "global", "private_dns_specifier")
	if strings.TrimSpace(mode) != "hostname" || strings.TrimSpace(spec) != o.DNS {
		warn(fmt.Sprintf("Unexpected system Private DNS: mode=%s server=%s", strings.TrimSpace(mode), strings.TrimSpace(spec)))
	}
	st, err = p.Status()
	if err != nil {
		return err
	}
	if !st.LockDNS {
		return fail("The DNS lock isn't active after locking.", "Status: "+st.Raw)
	}
	blocked := []string{"Private DNS", "extra users"}
	if st.LockReset {
		blocked = append(blocked, "factory reset")
	}
	if st.LockVPN {
		blocked = append(blocked, "VPN")
	}
	code := "new parent code"
	if o.KeepCode {
		code = "parent code kept"
	}
	done(5, fmt.Sprintf("Private DNS → %s · blocked: %s · %s", o.DNS, strings.Join(blocked, ", "), code))

	// 6. usb debugging
	if o.AllowDebug {
		skip(6, "left on (--allow-debug)")
		warn("USB debugging is still on. Anyone with a cable and adb can undo the lock.")
		return nil
	}
	start(6, "turning off USB debugging, the connection drops now")
	_, _ = p.adb.ShellTimeout(15*time.Second, "am", "broadcast", "-n", Receiver, "-a", Pkg+".FINALIZE")
	time.Sleep(2 * time.Second)
	if v, err := p.adb.ShellTimeout(5*time.Second, "settings", "get", "global", "adb_enabled"); err == nil && strings.TrimSpace(v) == "1" {
		warn("USB debugging still looks on. Check the phone.")
	}
	done(6, "turned off")
	return nil
}

// Unlock lifts the lock with the parent code. With release, the app also gives up device owner and is uninstalled.
func (p *Phone) Unlock(code string, release bool) (string, error) {
	if !p.AppInstalled() {
		return "", fail("The fckdigression app isn't installed on this phone.", "")
	}
	r, err := p.Send("UNLOCK", "--es", "pin", code, "--ez", "release", strconv.FormatBool(release))
	if err != nil {
		return "", err
	}
	if release {
		if out, err := p.adb.Uninstall(Pkg); err != nil || !strings.Contains(out, "Success") {
			return r, fail("Lock removed, but the app couldn't be uninstalled.", "Uninstall it from the phone's Settings → Apps.")
		}
	}
	return r, nil
}

func uniq(s []string) []string {
	seen := map[string]int{}
	var out []string
	for _, v := range s {
		if seen[v] == 0 {
			out = append(out, v)
		}
		seen[v]++
	}
	return out
}

// CodeFromEnv returns a code given through the environment, for scripts.
func CodeFromEnv() string {
	if v := os.Getenv("FCKD_CODE"); v != "" {
		return v
	}
	return os.Getenv("PHONELOCK_PIN")
}
