package llm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const gigaTokenURL = "https://ngw.devices.sberbank.ru:9443/api/v2/oauth"

type gigaAuth struct {
	credentials string
	scope       string
	url         string
	gate        chan struct{}
	accessToken string
	expires     time.Time
}

func (c *Client) configureGiga(cfg Config) error {
	u, _ := url.Parse(c.settings.BaseURL)
	if u.Scheme != "https" || u.Port() != "" || (u.Path != "/v1" && u.Path != "/api/v1") {
		return errors.New("для GigaChat используйте https://api.giga.chat/v1")
	}
	credentials := strings.TrimSpace(cfg.GigaCredentials)
	if credentials == "" {
		return errors.New("задайте GIGACHAT_CREDENTIALS: Authorization key из кабинета GigaChat API")
	}
	scope := cfg.GigaScope
	if scope == "" {
		scope = "GIGACHAT_API_PERS"
	}
	if scope != "GIGACHAT_API_PERS" {
		return errors.New("основной профиль GigaChat использует персональный Freemium scope GIGACHAT_API_PERS")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return errors.New("не удалось прочитать GIGACHAT_CA_FILE")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return errors.New("GIGACHAT_CA_FILE не содержит PEM-сертификат")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		hash := sha256.Sum256(pem)
		c.settings.CASHA256 = hex.EncodeToString(hash[:])
	}
	c.httpClient.Transport = transport
	c.giga = &gigaAuth{credentials: strings.TrimPrefix(credentials, "Basic "), scope: scope, url: gigaTokenURL, gate: make(chan struct{}, 1)}
	c.apiKey = ""
	c.settings.AuthMode, c.settings.Scope, c.settings.ReasoningEffort = "gigachat_oauth", scope, ""
	return nil
}

func (c *Client) bearer(ctx context.Context) (string, error) {
	if c.giga == nil {
		return c.apiKey, nil
	}
	g := c.giga
	select {
	case g.gate <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-g.gate }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if g.accessToken != "" && time.Now().Add(30*time.Second).Before(g.expires) {
		return g.accessToken, nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", errors.New("GigaChat: не удалось создать RqUID")
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	rquid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	body := url.Values{"scope": {g.scope}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, strings.NewReader(body))
	if err != nil {
		return "", errors.New("GigaChat: не удалось создать запрос токена")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("RqUID", rquid)
	req.Header.Set("Authorization", "Basic "+g.credentials)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("GigaChat: OAuth недоступен; проверьте соединение и сертификат")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GigaChat: OAuth HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil || len(data) > maxBodyBytes {
		return "", errors.New("GigaChat: не удалось прочитать токен")
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"`
	}
	if json.Unmarshal(data, &token) != nil || token.AccessToken == "" || token.ExpiresAt <= time.Now().UnixMilli() {
		return "", errors.New("GigaChat: некорректный ответ OAuth")
	}
	g.accessToken, g.expires = token.AccessToken, time.UnixMilli(token.ExpiresAt)
	return g.accessToken, nil
}
