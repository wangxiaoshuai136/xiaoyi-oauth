package xiaoyi

import "testing"

func TestCommandExecutable(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe" --single-argument %1`, `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`},
		{`"C:\Program Files\Google\Chrome\Application\chrome.exe"`, `C:\Program Files\Google\Chrome\Application\chrome.exe`},
		{`C:\browser.exe --url %1`, `C:\browser.exe`},
	}
	for _, c := range cases {
		got, err := commandExecutable(c.in)
		if err != nil || got != c.want {
			t.Fatalf("commandExecutable(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := commandExecutable("  "); err == nil {
		t.Fatal("empty command must fail")
	}
}

func TestIsChromiumBrowser(t *testing.T) {
	for _, name := range []string{"msedge.exe", "chrome.exe", "brave.exe", "vivaldi.exe"} {
		if !isChromiumBrowser(`C:\x\` + name) {
			t.Fatalf("%s should be Chromium", name)
		}
	}
	for _, name := range []string{"firefox.exe", "iexplore.exe"} {
		if isChromiumBrowser(`C:\x\` + name) {
			t.Fatalf("%s should not be Chromium", name)
		}
	}
}
