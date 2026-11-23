//go:build windows

package xiaoyi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// LaunchLoginBrowser starts an isolated Edge/Chrome window owned by the plugin.
// Each call uses a fresh profile, so several Huawei accounts can be signed in
// without touching the user's daily browser session.
func LaunchLoginBrowser(rawURL string) (*LoginBrowser, error) {
	browser, err := findBrowser()
	if err != nil {
		return nil, err
	}
	cleanupStaleProfiles()
	profile, err := os.MkdirTemp("", "xiaoyi-oauth-profile-*")
	if err != nil {
		return nil, err
	}
	args := []string{
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=msEdgeFirstRunExperience,msEdgeSidebarV2",
		"--disable-sync",
		"--app=" + rawURL,
	}
	cmd := exec.Command(browser, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(profile)
		return nil, fmt.Errorf("启动登录浏览器失败: %w", err)
	}
	instance := &LoginBrowser{Proc: cmd.Process, ProfileDir: profile}
	go func() {
		_ = cmd.Wait()
		// The window is gone (login finished or the user closed it): drop the
		// throwaway profile once the remaining child processes have exited.
		time.Sleep(2 * time.Second)
		instance.Close()
	}()
	return instance, nil
}

// terminateBrowser stops a browser and its renderer/utility children. Killing
// only the launcher leaves Edge children alive and keeps the profile locked.
func terminateBrowser(proc *os.Process) {
	if proc == nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(proc.Pid), "/T", "/F").Run()
}

// cleanupStaleProfiles removes leftover profiles from earlier runs. Anything
// younger than six hours may still belong to a live sign-in, so keep it.
func cleanupStaleProfiles() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-6 * time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "xiaoyi-oauth-profile-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(os.TempDir(), entry.Name()))
	}
}

func findBrowser() (string, error) {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("LocalAppData"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("LocalAppData"), "Google", "Chrome", "Application", "chrome.exe"),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("未找到 Edge 或 Chrome，无法拉起独立登录窗口")
}
