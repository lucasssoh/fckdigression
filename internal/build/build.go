// Package build compiles the Android app with the SDK command-line tools (no Gradle).
package build

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fckdigression/internal/adb"
)

// The signing key must never change: the app is device owner on phones in use,
// and Android refuses updates signed with another key.
const (
	keystoreName = "dnslock.keystore"
	keyPass      = "dnslock"
	keyAlias     = "dnslock"
)

// FindSource looks for the android/ folder from the working directory and the executable's directory.
func FindSource() (string, error) {
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		starts = append(starts, filepath.Dir(exe))
	}
	for _, s := range starts {
		for d := s; ; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, "android", "AndroidManifest.xml")); err == nil {
				return filepath.Join(d, "android"), nil
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return "", fmt.Errorf("can't find the android/ source folder; run this from the fckdigression folder or pass --src")
}

// Build compiles src (the android/ folder) into out, logging each stage.
func Build(src, out string, log func(string)) error {
	home := adb.AndroidHome()
	bt := latest(filepath.Join(home, "build-tools"), "")
	plat := latest(filepath.Join(home, "platforms"), "android-")
	if bt == "" || plat == "" {
		return fmt.Errorf("incomplete Android SDK in %s (needs build-tools and platforms)", home)
	}
	if _, err := exec.LookPath("javac"); err != nil {
		return fmt.Errorf("javac not found, install a JDK")
	}
	jar := filepath.Join(plat, "android.jar")
	log(fmt.Sprintf("build-tools %s, %s", filepath.Base(bt), filepath.Base(plat)))

	tmp, err := os.MkdirTemp("", "fckd-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	tool := func(name string) string { return filepath.Join(bt, name) }

	log("resources")
	if err := run(tool("aapt2"), "compile", "--dir", filepath.Join(src, "res"), "-o", filepath.Join(tmp, "res.zip")); err != nil {
		return err
	}
	unsigned := filepath.Join(tmp, "unsigned.apk")
	if err := run(tool("aapt2"), "link", "-I", jar, "--manifest", filepath.Join(src, "AndroidManifest.xml"),
		"--min-sdk-version", "29", "--target-sdk-version", "34", "-o", unsigned, filepath.Join(tmp, "res.zip")); err != nil {
		return err
	}

	log("java")
	var javaFiles []string
	_ = filepath.Walk(filepath.Join(src, "src"), func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".java") {
			javaFiles = append(javaFiles, p)
		}
		return nil
	})
	classes := filepath.Join(tmp, "classes")
	args := append([]string{"-nowarn", "-Xlint:-options", "--release", "11", "-classpath", jar, "-d", classes}, javaFiles...)
	if err := run("javac", args...); err != nil {
		return err
	}

	log("dex")
	var classFiles []string
	_ = filepath.Walk(classes, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".class") {
			classFiles = append(classFiles, p)
		}
		return nil
	})
	dex := filepath.Join(tmp, "dex")
	_ = os.MkdirAll(dex, 0o755)
	if err := run(tool("d8"), append([]string{"--release", "--min-api", "29", "--lib", jar, "--output", dex}, classFiles...)...); err != nil {
		return err
	}
	withDex := filepath.Join(tmp, "dex.apk")
	if err := addToZip(unsigned, withDex, filepath.Join(dex, "classes.dex")); err != nil {
		return err
	}

	log("sign")
	aligned := filepath.Join(tmp, "aligned.apk")
	if err := run(tool("zipalign"), "-f", "4", withDex, aligned); err != nil {
		return err
	}
	ks := filepath.Join(src, keystoreName)
	if _, err := os.Stat(ks); err != nil {
		log("no keystore found, creating " + ks + " (keep it safe: updates need the same key)")
		if err := run("keytool", "-genkeypair", "-keystore", ks, "-storepass", keyPass, "-keypass", keyPass,
			"-alias", keyAlias, "-keyalg", "RSA", "-keysize", "2048", "-validity", "36500", "-dname", "CN=DNS Lock"); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := run(tool("apksigner"), "sign", "--ks", ks, "--ks-pass", "pass:"+keyPass, "--out", out, aligned); err != nil {
		return err
	}
	_ = os.Remove(out + ".idsig")
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %v\n%s", filepath.Base(name), err, strings.TrimSpace(string(b)))
	}
	return nil
}

// latest returns the highest-versioned entry of dir (entries named prefix+version).
func latest(dir, prefix string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Slice(names, func(i, j int) bool {
		return less(strings.TrimPrefix(names[i], prefix), strings.TrimPrefix(names[j], prefix))
	})
	return filepath.Join(dir, names[len(names)-1])
}

func less(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, e1 := strconv.Atoi(pa[i])
		y, e2 := strconv.Atoi(pb[i])
		if e1 != nil || e2 != nil {
			if pa[i] != pb[i] {
				return pa[i] < pb[i]
			}
			continue
		}
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

// addToZip copies src to dst and adds file at the archive root.
func addToZip(src, dst, file string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, e := range r.File {
		if err := w.Copy(e); err != nil {
			return err
		}
	}
	dw, err := w.CreateHeader(&zip.FileHeader{Name: filepath.Base(file), Method: zip.Deflate})
	if err != nil {
		return err
	}
	in, err := os.Open(file)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := io.Copy(dw, in); err != nil {
		return err
	}
	return w.Close()
}
