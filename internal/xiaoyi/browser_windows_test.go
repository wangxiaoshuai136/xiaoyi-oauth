//go:build windows

package xiaoyi

import (
	"os"
	"testing"
	"time"
)

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
