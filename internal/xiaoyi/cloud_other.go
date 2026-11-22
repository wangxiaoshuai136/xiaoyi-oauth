//go:build !windows

package xiaoyi

import (
	"context"
	"fmt"
)

const (
	CloudBaseURL = "https://hag-drcn.op.dbankcloud.com/celia/v1/sse-api"
	BusinessID   = "WINPC_XIAOYI_WORK"
)

type BusinessCredential struct {
	Credential string
	UID        string
	ValidUntil int64
}

type AuthSession struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    int64
}

func ReadBusinessCredential() (BusinessCredential, error) {
	return BusinessCredential{}, fmt.Errorf("小艺Work 云端凭据仅支持 Windows")
}

func ReadAuthSession() (AuthSession, error) {
	return AuthSession{}, fmt.Errorf("小艺Work 云端凭据仅支持 Windows")
}

func MachineID() (string, error) { return "", fmt.Errorf("仅支持 Windows") }

func DeviceID(string) (string, error) { return "", fmt.Errorf("仅支持 Windows") }

func DisplayName(string) string { return "" }

func ClientID(string) string { return "" }

func RefreshAccessToken(context.Context, string, string, string, string) (string, error) {
	return "", fmt.Errorf("仅支持 Windows")
}

func CreateBusinessCredential(context.Context, string, string, string, string) (string, int64, error) {
	return "", 0, fmt.Errorf("仅支持 Windows")
}

func QueryPrivilege(context.Context, string, string, string) ([]byte, error) {
	return nil, fmt.Errorf("仅支持 Windows")
}
