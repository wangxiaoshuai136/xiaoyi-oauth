//go:build windows

package xiaoyi

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"time"
)

const (
	// CloudBaseURL is the upstream the desktop app's local proxy forwards to.
	CloudBaseURL = "https://hag-drcn.op.dbankcloud.com/celia/v1/sse-api"
	// BusinessID is the x-businessId the desktop app sends.
	BusinessID = "WINPC_XIAOYI_WORK"

	credentialCreateURL = "https://hag-drcn.op.dbankcloud.com/user-credential/v1/business-credential/create"
	refreshTokenURL     = "https://hag-drcn.op.dbankcloud.com/user-credential/v1/xiaoyiwork/refresh-token"

	keychainRoot = `Software\ClawDesktop\keychain`
	defaultValue = "ZGVmYXVsdA"
)

// BusinessCredential is the cloud credential the desktop app forwards.
type BusinessCredential struct {
	Credential string
	UID        string
	ValidUntil int64
}

// AuthSession is the Huawei account session kept by the desktop app.
type AuthSession struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    int64
}

// ReadBusinessCredential decrypts keychain/business-credential.
func ReadBusinessCredential() (BusinessCredential, error) {
	payload, err := decryptKeychainValue("business-credential")
	if err != nil {
		return BusinessCredential{}, fmt.Errorf("读取 business-credential 失败: %w", err)
	}
	var doc struct {
		Credential   string `json:"credential"`
		UID          string `json:"uid"`
		ValidTimeSec int64  `json:"validTimeSec"`
	}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return BusinessCredential{}, fmt.Errorf("解析 business-credential 失败: %w", err)
	}
	if strings.TrimSpace(doc.Credential) == "" {
		return BusinessCredential{}, fmt.Errorf("business-credential 缺少 credential 字段")
	}
	return BusinessCredential{
		Credential: strings.TrimSpace(doc.Credential),
		UID:        strings.TrimSpace(doc.UID),
		ValidUntil: doc.ValidTimeSec,
	}, nil
}

// ReadAuthSession decrypts keychain/auth-session.
func ReadAuthSession() (AuthSession, error) {
	payload, err := decryptKeychainValue("auth-session")
	if err != nil {
		return AuthSession{}, fmt.Errorf("读取 auth-session 失败: %w", err)
	}
	var doc struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		IDToken      string `json:"idToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return AuthSession{}, fmt.Errorf("解析 auth-session 失败: %w", err)
	}
	return AuthSession{
		AccessToken:  strings.TrimSpace(doc.AccessToken),
		RefreshToken: strings.TrimSpace(doc.RefreshToken),
		IDToken:      strings.TrimSpace(doc.IDToken),
		ExpiresAt:    doc.ExpiresAt,
	}, nil
}

func decryptKeychainValue(service string) (string, error) {
	raw, err := readRegistryString(keychainRoot+`\`+service, defaultValue)
	if err != nil {
		return "", err
	}
	envelope, err := decodeBase64(raw)
	if err != nil {
		return "", err
	}
	if len(envelope) < 3+12+16 || string(envelope[:3]) != "v10" {
		return "", fmt.Errorf("keychain 信封格式不受支持")
	}
	master, err := readOSCryptMasterKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(master)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := append(append([]byte(nil), envelope[15:len(envelope)-16]...), envelope[len(envelope)-16:]...)
	plain, err := gcm.Open(nil, envelope[3:15], sealed, nil)
	if err != nil {
		return "", fmt.Errorf("keychain AES-GCM 解密失败: %w", err)
	}
	var outer struct {
		V string `json:"v"`
	}
	if err := json.Unmarshal(plain, &outer); err != nil {
		return "", fmt.Errorf("keychain 明文不是预期 JSON: %w", err)
	}
	return outer.V, nil
}

// MachineID mirrors node-machine-id on Windows: sha256 of the lowercased
// MachineGuid, dashes kept.
func MachineID() (string, error) {
	guid, err := readRegistryStringRoot(syscall.HKEY_LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, "MachineGuid")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(guid))))
	return hex.EncodeToString(sum[:]), nil
}

// DeviceID is the app's "[machineId]_[uid]".
func DeviceID(uid string) (string, error) {
	machine, err := MachineID()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(uid) == "" {
		return machine, nil
	}
	return machine + "_" + strings.TrimSpace(uid), nil
}

// DisplayName reads a human name from the Huawei id_token, if present.
func DisplayName(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	for len(payload)%4 != 0 {
		payload += "="
	}
	raw, err := decodeBase64(payload)
	if err != nil {
		return ""
	}
	var claims struct {
		GivenName   string `json:"given_name"`
		DisplayName string `json:"display_name"`
		Nickname    string `json:"nickname"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	for _, name := range []string{claims.GivenName, claims.Nickname, claims.DisplayName} {
		if strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

// ClientID returns the Huawei app id (id_token aud).
func ClientID(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	for len(payload)%4 != 0 {
		payload += "="
	}
	raw, err := decodeBase64(payload)
	if err != nil {
		return ""
	}
	var claims struct {
		Aud string `json:"aud"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	return strings.TrimSpace(claims.Aud)
}

// RefreshAccessToken exchanges the stored refresh token for a fresh UAT.
func RefreshAccessToken(ctx context.Context, cred, uid, deviceID, refreshToken string) (string, error) {
	body, _ := json.Marshal(map[string]string{"refreshToken": refreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("businessCredential", cred)
	req.Header.Set("x-uid", uid)
	req.Header.Set("x-device-id", deviceID)
	req.Header.Set("x-hag-trace-id", randomHex(16))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("刷新 access token 失败 HTTP %d: %s", resp.StatusCode, truncateText(string(raw), 200))
	}
	var doc struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || strings.TrimSpace(doc.Token) == "" {
		return "", fmt.Errorf("刷新 access token 响应缺少 token")
	}
	return strings.TrimSpace(doc.Token), nil
}

// CreateBusinessCredential issues a new business credential with a UAT.
func CreateBusinessCredential(ctx context.Context, accessToken, uid, deviceID, clientID string) (string, int64, error) {
	now := time.Now().UnixMilli()
	body, _ := json.Marshal(map[string]string{
		"deviceId": deviceID,
		"uid":      uid,
		"clientId": clientID,
		"sysVer":   "5.0.0",
		"version":  "1.0.0",
		"ts":       fmt.Sprintf("%d", now),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, credentialCreateURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("x-uid", uid)
	req.Header.Set("x-client-version", "1.0.0")
	req.Header.Set("x-hag-trace-id", randomHex(16))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("创建 business credential 失败 HTTP %d: %s", resp.StatusCode, truncateText(string(raw), 200))
	}
	var doc struct {
		Code             string `json:"code"`
		CredentialResult struct {
			BusinessCredential string `json:"businessCredential"`
			Credential         string `json:"credential"`
			ValidTime          int64  `json:"validTime"`
		} `json:"credentialResult"`
		Data struct {
			BusinessCredential string `json:"businessCredential"`
			Credential         string `json:"credential"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", 0, fmt.Errorf("创建 business credential 响应非 JSON")
	}
	cred := firstNonEmpty(doc.CredentialResult.BusinessCredential, doc.CredentialResult.Credential, doc.Data.BusinessCredential, doc.Data.Credential)
	if cred == "" {
		return "", 0, fmt.Errorf("创建 business credential 响应缺少 credential")
	}
	return cred, doc.CredentialResult.ValidTime, nil
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
