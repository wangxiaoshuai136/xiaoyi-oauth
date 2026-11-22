//go:build !windows

package xiaoyi

import "fmt"

func DecryptProxyKey() (string, error) {
	return "", fmt.Errorf("小艺Work 本地代理仅支持 Windows（注册表 + DPAPI）")
}
