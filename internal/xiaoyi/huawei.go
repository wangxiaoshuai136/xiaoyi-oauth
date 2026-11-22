package xiaoyi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// HuaweiOAuthHost is the account host the desktop app uses.
	HuaweiOAuthHost = "https://oauth-login.cloud.huawei.com"
	// HuaweiAuthorizeURL is the account sign-in page.
	HuaweiAuthorizeURL = HuaweiOAuthHost + "/oauth2/v3/authorize"
	// HuaweiTokenURL exchanges codes and refreshes tokens.
	HuaweiTokenURL = HuaweiOAuthHost + "/oauth2/v3/token"
	// HuaweiClientID is the XiaoyiWork app id.
	HuaweiClientID = "6917612679526496902"
	// HuaweiRedirectURI is the loopback callback the app registers.
	HuaweiRedirectURI = "http://127.0.0.1:17355/auth/callback"
	// HuaweiScope is the same scope set the app requests.
	HuaweiScope = "openid profile https://www.huawei.com/auth/drive https://www.huawei.com/auth/drive.xiaoyiwork"
	// CallbackPort is the loopback port the redirect URI names.
	CallbackPort = 17355
	// CallbackPath is the redirect path.
	CallbackPath = "/auth/callback"
)

// OAuthTokens is one Huawei account session.
type OAuthTokens struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    int64 // unix milliseconds
}

// PKCEPair returns a verifier and its S256 challenge.
func PKCEPair() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// AuthorizeURL builds the account sign-in URL.
func AuthorizeURL(state, challenge string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("access_type", "offline")
	q.Set("client_id", HuaweiClientID)
	q.Set("redirect_uri", HuaweiRedirectURI)
	q.Set("scope", HuaweiScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("display", "page")
	return HuaweiAuthorizeURL + "?" + q.Encode()
}

// ExchangeCode turns an authorization code into tokens.
func ExchangeCode(ctx context.Context, code, verifier string) (OAuthTokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", HuaweiClientID)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", HuaweiRedirectURI)
	return postTokenForm(ctx, form)
}

// RefreshOAuth refreshes an account session with its refresh token.
func RefreshOAuth(ctx context.Context, refreshToken string) (OAuthTokens, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", HuaweiClientID)
	form.Set("refresh_token", refreshToken)
	return postTokenForm(ctx, form)
}

func postTokenForm(ctx context.Context, form url.Values) (OAuthTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, HuaweiTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthTokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return OAuthTokens{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthTokens{}, fmt.Errorf("华为账号换票失败 HTTP %d: %s", resp.StatusCode, truncateText(string(raw), 200))
	}
	var doc struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return OAuthTokens{}, fmt.Errorf("华为账号换票响应非 JSON")
	}
	if strings.TrimSpace(doc.AccessToken) == "" {
		return OAuthTokens{}, fmt.Errorf("华为账号换票响应缺少 access_token")
	}
	expiresIn := doc.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return OAuthTokens{
		AccessToken:  strings.TrimSpace(doc.AccessToken),
		RefreshToken: strings.TrimSpace(doc.RefreshToken),
		IDToken:      strings.TrimSpace(doc.IDToken),
		ExpiresAt:    time.Now().UnixMilli() + expiresIn*1000,
	}, nil
}

// IDClaims are the account facts the plugin needs from the id_token.
type IDClaims struct {
	UID      string
	Name     string
	ClientID string
}

// ParseIDToken reads uid, display name and client id from an id_token.
func ParseIDToken(idToken string) IDClaims {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return IDClaims{}
	}
	payload := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	for len(payload)%4 != 0 {
		payload += "="
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return IDClaims{}
	}
	var claims struct {
		UID         string `json:"uid"`
		GivenName   string `json:"given_name"`
		Nickname    string `json:"nickname"`
		DisplayName string `json:"display_name"`
		Aud         string `json:"aud"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return IDClaims{}
	}
	name := claims.GivenName
	if name == "" {
		name = claims.Nickname
	}
	if name == "" {
		name = claims.DisplayName
	}
	client := claims.Aud
	if client == "" {
		client = HuaweiClientID
	}
	return IDClaims{UID: strings.TrimSpace(claims.UID), Name: strings.TrimSpace(name), ClientID: strings.TrimSpace(client)}
}
