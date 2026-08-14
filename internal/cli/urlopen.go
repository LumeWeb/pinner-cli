package cli

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openURL opens the given URL in the user's default browser, cross-platform.
// It is a best-effort convenience: if the platform has no reliable opener or
// the launch fails, it returns an error the caller can surface (the URL is
// always printed by the caller so the user can open it manually).
func openURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		// Linux, BSDs, and anything else POSIX-y: xdg-open is the standard.
		if _, err := exec.LookPath("xdg-open"); err == nil {
			return exec.Command("xdg-open", url).Start()
		}
		return fmt.Errorf("no URL opener found for %s (open %s manually)", runtime.GOOS, url)
	}
}
