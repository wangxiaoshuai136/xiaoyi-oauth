package xiaoyi

import "os"

// LoginBrowser is a browser instance the plugin owns for one sign-in.
type LoginBrowser struct {
	Proc       *os.Process
	ProfileDir string
}

// Close stops the browser. The temporary profile is left for the OS to clean.
func (b *LoginBrowser) Close() {
	if b == nil || b.Proc == nil {
		return
	}
	_ = b.Proc.Kill()
	b.Proc = nil
}
