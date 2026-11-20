# make          build the Android app, then the fckd binary with the app inside
# make install  copy fckd to ~/.local/bin
# make test     run the Go tests

BIN    := bin/fckd
APK    := internal/apk/dist/fckd.apk
PREFIX ?= $(HOME)/.local

.PHONY: all apk bin install test clean

all: bin

apk: $(APK)

$(APK): $(shell find android -type f -not -path 'android/build/*')
	go run ./cmd/fckd build -o $(APK)

bin: $(APK)
	go build -o $(BIN) ./cmd/fckd

install: bin
	install -Dm755 $(BIN) $(PREFIX)/bin/fckd

test:
	go test ./...

clean:
	rm -rf bin android/build
	rm -f $(APK)
