//go:build windows

package xiaoyi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// LaunchLoginBrowser starts an isolated Edge/Chrome window owned by the plugin.
// A visible window is required because Huawei sign-in needs the user (QR, SMS
// or password); the dedicated profile keeps the user's daily browser untouched.
func LaunchLoginBrowser(rawURL string) (*LoginBrowser, error) {
	browser, err := findBrowser()
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("启动登录浏览器失败: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return &LoginBrowser{Proc: cmd.Process, ProfileDir: profile}, nil
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
	return "", fmt.Errorf("未找到 Edge 或 Chrome，无法拉起登录浏览器")
}
