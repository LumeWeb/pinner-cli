// Package urlopen opens URLs in the user's default browser, cross-platform.
// It is a stdlib-only leaf package so both internal/cli and internal/mcp can
// import it without an import cycle (cli imports mcp, so mcp cannot import
// cli directly).
package urlopen

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Open opens the given URL in the user's default browser, cross-platform.
// It is best-effort: on failure it returns an error the caller can surface,
// and the caller always prints the URL first so the user can open it manually.
//
// Portability notes:
//   - darwin:  `open <url>`
//   - windows: `cmd /c start "" <url>` (shell-builtin, handles quoting), with
//     rundll32 as a fallback for minimal environments that lack cmd start.
//   - other:   POSIX desktops. xdg-open is the canonical opener; if it is not
//     installed we fall back to sensible-browser (Debian family) and then
//     x-www-browser (generic X11) so headless/minimal systems still work.
//
// The command is launched detached (Start, not Run) so the caller does not
// block on the browser process.
func Open(url string) error {
	if url == "" {
		return errors.New("urlopen: empty URL")
	}

	switch runtime.GOOS {
	case "darwin":
		return detach("open", url)

	case "windows":
		// cmd /c start "" <url> handles URLs with & and spaces. Empty first
		// quoted arg is the (optional) window title. The URL must be quoted:
		// in cmd, & is a command separator and spaces split arguments, so an
		// unquoted URL with a query string (&) or spaces breaks into a second
		// failing command while cmd.Start() itself succeeds. Strip embedded
		// quotes, then wrap in quotes so cmd treats the URL as one token.
		safe := strings.NewReplacer(`"`, "").Replace(url)
		cmd := exec.Command("cmd", "/c", "start", "", `"`+safe+`"`)
		if err := cmd.Start(); err == nil {
			return nil
		}
		// Fallback for environments without cmd start (e.g. busybox/git-bash
		// quirks): rundll32's FileProtocolHandler hands the URL to the shell.
		return detach("rundll32", "url.dll,FileProtocolHandler", url)

	default:
		// POSIX (linux, *bsd, ...). Try each opener in order.
		for _, opener := range []string{"xdg-open", "sensible-browser", "x-www-browser"} {
			if _, err := exec.LookPath(opener); err == nil {
				if err := detach(opener, url); err == nil {
					return nil
				}
			}
		}
		// gio is the GLib opener on GNOME; include it as a final POSIX attempt.
		if _, err := exec.LookPath("gio"); err == nil {
			if err := detach("gio", "open", url); err == nil {
				return nil
			}
		}
		return fmt.Errorf("no URL opener available on %s (open %q manually)", runtime.GOOS, url)
	}
}

// detach starts a command without waiting for it to finish.
func detach(name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return err
	}
	return exec.Command(name, args...).Start()
}
