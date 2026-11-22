//go:build windows

package xiaoyi

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	registryPath  = `Software\ClawDesktop\keychain\model-proxy`
	registryValue = "cHJveHlLZXk"

	cryptProtectUIForbidden = 0x1
)

var (
	crypt32DLL        = syscall.NewLazyDLL("crypt32.dll")
	procUnprotectData = crypt32DLL.NewProc("CryptUnprotectData")
	kernel32DLL       = syscall.NewLazyDLL("kernel32.dll")
	procLocalFree     = kernel32DLL.NewProc("LocalFree")
)

type dataBlob struct {
	size uint32
	data *byte
}

// DecryptProxyKey reads the XiaoyiWork proxy key from HKCU and decrypts the
// Chromium OSCrypt v10 envelope with the DPAPI-protected master key stored in
// the app's Local State file. Nothing is written to disk.
func DecryptProxyKey() (string, error) {
	envelopeText, err := readRegistryString(registryPath, registryValue)
	if err != nil {
		return "", fmt.Errorf("读取小艺Work 注册表凭据失败: %w", err)
	}
	envelope, err := decodeBase64(envelopeText)
	if err != nil {
		return "", fmt.Errorf("注册表凭据不是合法的 base64: %w", err)
	}
	if len(envelope) < 3+12+16 {
		return "", fmt.Errorf("注册表凭据长度异常（%d 字节）", len(envelope))
	}
	if string(envelope[:3]) != "v10" {
		return "", fmt.Errorf("不支持的凭据信封前缀 %q", string(envelope[:3]))
	}
	iv := envelope[3:15]
	body := envelope[15:]
	ciphertext, tag := body[:len(body)-16], body[len(body)-16:]

	master, err := readOSCryptMasterKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(master)
	if err != nil {
		return "", fmt.Errorf("初始化 AES 失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("初始化 AES-GCM 失败: %w", err)
	}
	sealed := make([]byte, 0, len(ciphertext)+len(tag))
	sealed = append(sealed, ciphertext...)
	sealed = append(sealed, tag...)
	plain, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("AES-GCM 解密凭据失败: %w", err)
	}

	var payload struct {
		V string `json:"v"`
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		return "", fmt.Errorf("凭据明文不是预期 JSON: %w", err)
	}
	if strings.TrimSpace(payload.V) == "" {
		return "", fmt.Errorf("凭据明文缺少 v 字段")
	}
	return strings.TrimSpace(payload.V), nil
}

func readOSCryptMasterKey() ([]byte, error) {
	var lastErr error
	for _, path := range localStateCandidates() {
		raw, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}
		var state struct {
			OSCrypt struct {
				EncryptedKey string `json:"encrypted_key"`
			} `json:"os_crypt"`
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			lastErr = fmt.Errorf("解析 %s 失败: %w", path, err)
			continue
		}
		encoded := strings.TrimSpace(state.OSCrypt.EncryptedKey)
		if encoded == "" {
			lastErr = fmt.Errorf("%s 中没有 os_crypt.encrypted_key", path)
			continue
		}
		blob, err := decodeBase64(encoded)
		if err != nil {
			lastErr = fmt.Errorf("encrypted_key 不是合法 base64: %w", err)
			continue
		}
		if len(blob) < 5 || string(blob[:5]) != "DPAPI" {
			lastErr = fmt.Errorf("encrypted_key 缺少 DPAPI 前缀")
			continue
		}
		key, err := dpapiUnprotect(blob[5:])
		if err != nil {
			lastErr = fmt.Errorf("DPAPI 解密主密钥失败: %w", err)
			continue
		}
		if len(key) != 32 {
			lastErr = fmt.Errorf("主密钥长度异常（%d 字节，预期 32）", len(key))
			continue
		}
		return key, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("找不到小艺Work 的 Local State 文件")
	}
	return nil, lastErr
}

func localStateCandidates() []string {
	var paths []string
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		paths = append(paths, filepath.Join(appData, "xiaoyiwork", "Local State"))
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		paths = append(paths, filepath.Join(localAppData, "xiaoyiwork", "Local State"))
	}
	if profile := strings.TrimSpace(os.Getenv("USERPROFILE")); profile != "" {
		paths = append(paths, filepath.Join(profile, "AppData", "Roaming", "xiaoyiwork", "Local State"))
	}
	return paths
}

func dpapiUnprotect(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, fmt.Errorf("DPAPI 输入为空")
	}
	in := dataBlob{size: uint32(len(blob)), data: &blob[0]}
	var out dataBlob
	ret, _, callErr := procUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, fmt.Errorf("CryptUnprotectData 返回失败")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	if out.data == nil || out.size == 0 {
		return nil, fmt.Errorf("DPAPI 输出为空")
	}
	result := unsafe.Slice(out.data, int(out.size))
	copied := make([]byte, len(result))
	copy(copied, result)
	return copied, nil
}

func readRegistryString(path, value string) (string, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, pathPtr, 0, syscall.KEY_READ, &key); err != nil {
		return "", fmt.Errorf("打开注册表键失败: %w", err)
	}
	defer syscall.RegCloseKey(key)

	valuePtr, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return "", err
	}
	const maxValueBytes = 64 * 1024
	buf := make([]byte, maxValueBytes)
	var valueType uint32
	size := uint32(len(buf))
	if err := syscall.RegQueryValueEx(key, valuePtr, nil, &valueType, &buf[0], &size); err != nil {
		return "", fmt.Errorf("读取注册表值失败: %w", err)
	}
	if size > uint32(len(buf)) {
		return "", fmt.Errorf("注册表值过大（%d 字节）", size)
	}
	switch valueType {
	case syscall.REG_SZ, syscall.REG_EXPAND_SZ:
		utf16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[0])), size/2)
		return strings.TrimRight(syscall.UTF16ToString(utf16), "\x00"), nil
	case syscall.REG_BINARY:
		return strings.TrimRight(string(buf[:size]), "\x00"), nil
	default:
		return "", fmt.Errorf("注册表值类型 %d 不受支持", valueType)
	}
}

func decodeBase64(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		if raw, err := enc.DecodeString(text); err == nil {
			return raw, nil
		} else {
			lastErr = err
		}
	}
	return nil, lastErr
}
