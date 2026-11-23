package xiaoyi

import (
	"os"
	"sync"
	"time"
)

// LoginBrowser is a browser instance the plugin owns for one sign-in.
type LoginBrowser struct {
	mu         sync.Mutex
	Proc       *os.Process
	ProfileDir string
}

// Close stops the browser and removes its throwaway profile. It is safe to
// call more than once and from several goroutines.
func (b *LoginBrowser) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	proc := b.Proc
	profile := b.ProfileDir
	b.Proc = nil
	b.ProfileDir = ""
	b.mu.Unlock()
	if proc != nil {
		terminateBrowser(proc)
	}
	if profile != "" {
		removeProfile(profile)
	}
}

// removeProfile deletes a temporary browser profile. Browsers can keep file
// handles for a moment after termination, so retry briefly before giving up.
func removeProfile(path string) {
	for attempt := 0; attempt < 12; attempt++ {
		if err := os.RemoveAll(path); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = os.RemoveAll(path)
}
