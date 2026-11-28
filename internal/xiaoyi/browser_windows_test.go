//go:build windows

package xiaoyi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultBrowserExecutable(t *testing.T) {
	exe, err := defaultBrowserExecutable()
	if err != nil {
		t.Skipf("cannot resolve default browser: %v", err)
	}
	if !filepath.IsAbs(exe) {
		t.Fatalf("default browser path is not absolute: %q", exe)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("default browser executable missing: %v", err)
	}
	t.Logf("default browser: %s (chromium=%v)", exe, isChromiumBrowser(exe))
}

// TestLaunchLoginBrowserCleanup opens a real isolated browser window and
// checks that Close stops the whole process tree and removes the profile.
// It only runs when XIAOYI_UI_TEST=1 because it needs a desktop session.
func TestLaunchLoginBrowserCleanup(t *testing.T) {
	if os.Getenv("XIAOYI_UI_TEST") != "1" {
		t.Skip("set XIAOYI_UI_TEST=1 to run the browser window test")
	}
	browser, err := LaunchLoginBrowser("https://oauth-login.cloud.huawei.com/")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	profile := browser.ProfileDir
	if profile == "" {
		// Non-Chromium default browsers are opened through the shell handler
		// without a profile owned by the plugin.
		browser.Close()
		t.Skip("default browser is not Chromium-based; nothing to clean up")
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("profile missing right after launch: %v", err)
	}
	time.Sleep(3 * time.Second)
	browser.Close()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(profile); os.IsNotExist(err) {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("profile still present after close: %s", profile)
}
