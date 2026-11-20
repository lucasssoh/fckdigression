// Package adb is a thin wrapper around the adb binary.
package adb

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client runs adb, optionally against one device serial.
type Client struct {
	Bin    string
	Serial string
}

// Find locates adb: $ADB, then PATH, then $ANDROID_HOME/platform-tools.
func Find() (string, error) {
	if p := os.Getenv("ADB"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("adb"); err == nil {
		return p, nil
	}
	p := filepath.Join(AndroidHome(), "platform-tools", "adb")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", errors.New("adb not found")
}

// AndroidHome returns $ANDROID_HOME or ~/Android/Sdk.
func AndroidHome() string {
	if h := os.Getenv("ANDROID_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Android", "Sdk")
}

// Device is one line of `adb devices`.
type Device struct {
	Serial string
	State  string // device, unauthorized, offline, ...
}

func (c *Client) cmd(ctx context.Context, args ...string) *exec.Cmd {
	if c.Serial != "" {
		args = append([]string{"-s", c.Serial}, args...)
	}
	return exec.CommandContext(ctx, c.Bin, args...)
}

func (c *Client) run(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var out bytes.Buffer
	cmd := c.cmd(ctx, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.ReplaceAll(out.String(), "\r", ""), err
}

// Devices lists connected devices (ignores the -s serial).
func (c *Client) Devices() ([]Device, error) {
	all := Client{Bin: c.Bin}
	out, err := all.run(10*time.Second, "devices")
	if err != nil {
		return nil, err
	}
	var devs []Device
	// Lines are "serial<TAB>state"; skip the header and "* daemon started" notices.
	for _, l := range strings.Split(out, "\n") {
		serial, state, ok := strings.Cut(strings.TrimSpace(l), "\t")
		if ok && !strings.HasPrefix(serial, "*") {
			devs = append(devs, Device{Serial: serial, State: strings.TrimSpace(state)})
		}
	}
	return devs, nil
}

// Shell runs a command on the device and returns its output.
func (c *Client) Shell(args ...string) (string, error) {
	return c.run(60*time.Second, append([]string{"shell"}, args...)...)
}

// ShellTimeout is Shell with a custom timeout (for commands that drop the connection).
func (c *Client) ShellTimeout(t time.Duration, args ...string) (string, error) {
	return c.run(t, append([]string{"shell"}, args...)...)
}

// Prop reads a system property.
func (c *Client) Prop(name string) string {
	out, _ := c.Shell("getprop", name)
	return strings.TrimSpace(out)
}

// Install installs or updates an APK.
func (c *Client) Install(apk string) (string, error) {
	return c.run(3*time.Minute, "install", "-r", apk)
}

// Uninstall removes a package.
func (c *Client) Uninstall(pkg string) (string, error) {
	return c.run(time.Minute, "uninstall", pkg)
}
