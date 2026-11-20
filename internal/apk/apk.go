// Package apk embeds the Android app, so the fckd binary works on its own.
// `make` builds android/ into dist/fckd.apk first, then compiles the binary.
package apk

import (
	"embed"
	"os"
)

//go:embed all:dist
var dist embed.FS

func data() []byte {
	b, _ := dist.ReadFile("dist/fckd.apk")
	return b
}

// Available reports whether this binary was built with the app inside.
func Available() bool { return len(data()) > 0 }

// WriteTemp writes the embedded APK to a temporary file and returns its path.
func WriteTemp() (string, error) {
	f, err := os.CreateTemp("", "fckd-*.apk")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data()); err != nil {
		return "", err
	}
	return f.Name(), nil
}
