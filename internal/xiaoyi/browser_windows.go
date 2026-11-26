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
	"unsafe"
)

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

// LaunchLoginBrowser opens the sign-in page in the user's default browser. For
// Chromium-based defaults it keeps the plugin-owned isolated window (fresh
// profile); other browsers are opened through the system default handler.
func LaunchLoginBrowser(rawURL string) (*LoginBrowser, error) {
	browser, err := defaultBrowserExecutable()
	if err == nil && isChromiumBrowser(browser) {
		return launchIsolatedChromium(browser, rawURL)
	}
	if err := openWithDefaultBrowser(rawURL); err != nil {
		return nil, err
	}
	mode := "default"
	if err != nil {
		mode = "default (" + err.Error() + ")"
	}
	return &LoginBrowser{Mode: mode}, nil
}

func launchIsolatedChromium(browser, rawURL string) (*LoginBrowser, error) {
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
	instance := &LoginBrowser{Proc: cmd.Process, ProfileDir: profile, Mode: "chromium:" + browser}
	go func() {
		_ = cmd.Wait()
		// Chromium often exits its launcher immediately and keeps the real
		// browser alive under a child process, so Wait() is not a reliable
		// "window closed" signal. Cleanup happens when the login completes,
		// fails or times out; terminateBrowser also matches the profile.
	}()
	return instance, nil
}

// openWithDefaultBrowser hands the URL to the shell so Windows uses the
// user's default browser instead of a hard-coded one.
func openWithDefaultBrowser(rawURL string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(rawURL)
	if err != nil {
		return err
	}
	const swShowNormal = 1
	ret, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		swShowNormal,
	)
	if ret <= 32 {
		return fmt.Errorf("打开默认浏览器失败（ShellExecute=%d）: %v", ret, callErr)
	}
	return nil
}

// terminateBrowser stops a browser and its renderer/utility children. Some
// Chromium builds exit their launcher immediately and reparent the real
// processes, so killing the recorded PID is not enough; every process whose
// command line carries the throwaway profile directory is stopped as well.
func terminateBrowser(proc *os.Process, profile string) {
	if proc != nil {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(proc.Pid), "/T", "/F").Run()
	}
	if profile == "" {
		return
	}
	marker := filepath.Base(profile)
	script := `Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*` + marker + `*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }`
	_ = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script).Run()
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

// defaultBrowserExecutable resolves the executable behind the user's http URL
// association. Empty string means the association could not be resolved.
func defaultBrowserExecutable() (string, error) {
	progID, err := readRegistryString(
		`Software\Microsoft\Windows\Shell\Associations\UrlAssociations\http\UserChoice`,
		"ProgId",
	)
	if err != nil || strings.TrimSpace(progID) == "" {
		return "", fmt.Errorf("读取默认浏览器失败: %w", err)
	}
	command, err := readBrowserCommand(strings.TrimSpace(progID))
	if err != nil || strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("读取默认浏览器命令失败: %w", err)
	}
	return commandExecutable(command)
}

// readBrowserCommand reads the shell open command, preferring the per-user
// override and falling back to the machine-wide HKCR association.
func readBrowserCommand(progID string) (string, error) {
	if command, err := readRegistryString(`Software\Classes\`+progID+`\shell\open\command`, ""); err == nil && command != "" {
		return command, nil
	}
	return readRegistryStringRoot(syscall.HKEY_CLASSES_ROOT, progID+`\shell\open\command`, "")
}
