//go:build !windows

package xiaoyi

import "fmt"

func LaunchLoginBrowser(string) (*LoginBrowser, error) {
	return nil, fmt.Errorf("登录浏览器仅支持 Windows")
}
