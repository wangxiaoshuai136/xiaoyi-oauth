package xiaoyi

import (
	"fmt"
	"strings"
)

// commandExecutable extracts the executable path from a shell open command,
// e.g. `"C:\...\msedge.exe" --single-argument %1`.
func commandExecutable(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("默认浏览器命令为空")
	}
	var candidate string
	if command[0] == '"' {
		if end := strings.IndexByte(command[1:], '"'); end >= 0 {
			candidate = command[1 : end+1]
		}
	} else if index := strings.IndexAny(command, " \t"); index > 0 {
		candidate = command[:index]
	} else {
		candidate = command
	}
	candidate = strings.Trim(candidate, `"`)
	if candidate == "" {
		return "", fmt.Errorf("无法解析默认浏览器路径: %s", command)
	}
	return candidate, nil
}

func isChromiumBrowser(path string) bool {
	name := path
	if index := strings.LastIndexAny(name, `\/`); index >= 0 {
		name = name[index+1:]
	}
	switch strings.ToLower(name) {
	case "msedge.exe", "chrome.exe", "brave.exe", "vivaldi.exe", "opera.exe", "thorium.exe":
		return true
	default:
		return false
	}
}
