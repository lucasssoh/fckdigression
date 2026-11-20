# Development

## Layout

```
cmd/fckd/          entry point: CLI, or the TUI with no arguments
internal/adb/      runs the adb binary
internal/core/     talks to the app, runs the lock steps, parses status
internal/cli/      command-line output
internal/tui/      full-screen interface (Bubble Tea)
internal/build/    builds the APK with the SDK tools, no Gradle
internal/apk/      embeds dist/fckd.apk into the binary
android/           the Android app (plain Java, no AndroidX)
design/            mockups
legacy/phonelock   the original bash tool
```

`internal/core` has no output. It reports progress as events, and the CLI and TUI render them.

## Build

```sh
make            # android/ → internal/apk/dist/fckd.apk → bin/fckd
make test
fckd build      # APK only, to android/build/fckd.apk
```

The app is built with `aapt2`, `javac --release 11`, `d8`, `zipalign` and `apksigner`, using the newest `build-tools` and `platforms` found in the SDK. Min SDK 29, target 34.

d8 from build-tools 34 crashes on inner and anonymous classes compiled by recent JDKs (it chokes on their `MethodParameters` attribute). The app uses lambdas instead.

## How the PC talks to the app

The app has two receivers:

- `AdminReceiver`: the device admin component. `dpm set-device-owner` points at it.
- `CommandReceiver`: commands from the PC. It requires the `DUMP` permission, so only the adb shell can call it, not other apps.

Commands are broadcasts, and the reply comes back in the broadcast data:

```sh
adb shell am broadcast -n fr.lucas.dnslock/.CommandReceiver -a fr.lucas.dnslock.STATUS
```

| Action | Extras | Does |
|---|---|---|
| `LOCK` | `dns`, `salt`, `hash`, `len`, `keep_code`, `block_reset`, `block_vpn`, `block_debug` | sets Private DNS and the restrictions |
| `FINALIZE` | | blocks USB debugging (the adb connection drops) |
| `STATUS` | | returns `key=value` pairs |
| `UNLOCK` | `pin`, `release` | checks the code, lifts the lock, optionally gives up device owner |

Replies start with `OK:` or `ERROR:`. App version 1 answered in French (`ERREUR:`, `proprietaire=`, `verrou_dns=`…), and `core.ParseStatus` reads both.

The code never reaches the phone in clear text when locking: the PC sends `sha256(salt + ":" + code)` and the salt. On unlock the code is sent and hashed on the phone.

## Lock state on the phone

Kept in the app's private `SharedPreferences` (`lock`): `dns`, `salt`, `hash`, `len`, `fails`, `lockedUntil`, `restrictions`, `lastRestrictions` (for "Lock again"), `pendingDebugBlock`. A sideloaded update keeps them.

## Adding a guard

A guard is a set of device-owner policies plus their status keys. Add the policies in `Lock.java` (apply, status, unlock), the options in `core.LockOptions`, and a row in the app, the CLI status and the TUI dashboard.
