package xiaoyi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Identity is the signed-in XiaoyiWork account as the desktop app records it.
type Identity struct {
	UID       string
	LastLogin int64
}

// ReadIdentity returns the newest users/*/identity.json. The uid is enough to
// label a credential; no nickname or phone number is stored by the app.
func ReadIdentity() (Identity, error) {
	root := dataDir()
	if root == "" {
		return Identity{}, os.ErrNotExist
	}
	files, err := filepath.Glob(filepath.Join(root, "users", "*", "identity.json"))
	if err != nil || len(files) == 0 {
		return Identity{}, os.ErrNotExist
	}
	sort.Slice(files, func(i, j int) bool {
		si, _ := os.Stat(files[i])
		sj, _ := os.Stat(files[j])
		if si == nil || sj == nil {
			return files[i] < files[j]
		}
		return si.ModTime().After(sj.ModTime())
	})
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var doc struct {
			UID       string `json:"uid"`
			LastLogin int64  `json:"lastLoginAt"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		if uid := strings.TrimSpace(doc.UID); uid != "" {
			return Identity{UID: uid, LastLogin: doc.LastLogin}, nil
		}
	}
	return Identity{}, os.ErrNotExist
}

// ShortUID keeps a uid readable in account labels.
func ShortUID(uid string) string {
	uid = strings.TrimSpace(uid)
	if len(uid) <= 8 {
		return uid
	}
	return uid[:4] + "…" + uid[len(uid)-4:]
}
