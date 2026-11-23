//go:build !windows

package xiaoyi

import (
	"fmt"
	"os"
)

func LaunchLoginBrowser(string) (*LoginBrowser, error) {
	return nil, fmt.Errorf("独立登录浏览器仅支持 Windows")
}

func terminateBrowser(proc *os.Process) {
	if proc == nil {
		return
	}
	_ = proc.Kill()
}
