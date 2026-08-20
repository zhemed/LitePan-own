package cloud189

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/httpx"
)

func (d *Driver) RefreshAuth(ctx context.Context, _ driver.RefreshCaller) (driver.RefreshOutcome, error) {
	_, err := d.doRefresh(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "缺少 refresh_token") || strings.Contains(err.Error(), "重新扫码") {
			return driver.RefreshFatal, err
		}
		return driver.RefreshRetryable, err
	}
	return driver.RefreshSuccess, nil
}

func (d *Driver) doRefresh(ctx context.Context) (string, error) {
	d.mu.Lock()
	refresh := strings.TrimSpace(d.refreshToken)
	d.mu.Unlock()
	if refresh == "" {
		return "", domain.Errorf(domain.CodeAuthExpired, "缺少 refresh_token，请重新扫码登录")
	}

	form := url.Values{}
	form.Set("clientId", appID)
	form.Set("refreshToken", refresh)
	form.Set("grantType", "refresh_token")
	form.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authURL+"/api/oauth2/refreshToken.do", strings.NewReader(form.Encode()))
	if err != nil {
		return "", domain.Wrap(domain.CodeInternal, err)
	}
	set189Headers(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	resp, data, err := httpx.Execute(d.client, req, httpx.DefaultReadLimit)
	if err != nil {
		return "", domain.Wrap(domain.CodeDriverError, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", domain.Errorf(domain.CodeDriverError, "刷新令牌失败 HTTP %d: %s", resp.StatusCode, httpx.Truncate(data, 300))
	}
	var out oauthRefreshResp
	if err := decodeJSON(data, &out); err != nil {
		return "", err
	}
	if !successResCode(out.ResCode) {
		msg := strings.TrimSpace(out.ResMessage)
		if msg == "" {
			msg = "刷新令牌失败"
		}
		return "", domain.Errorf(domain.CodeAuthExpired, "%s", msg)
	}

	access := firstString(out.AccessToken, out.AccessToken2)
	newRefresh := firstString(out.RefreshToken, refresh)
	if access == "" {
		return "", domain.Errorf(domain.CodeAuthExpired, "刷新令牌成功但未返回 accessToken")
	}
	if err := d.refreshSession(ctx, access); err != nil {
		return "", err
	}
	expiresIn := firstPositiveNumber(out.ExpiresIn, out.ExpiresIn2, out.Expires)
	expiresAt := time.Time{}
	if expiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}

	d.mu.Lock()
	d.accessToken = access
	d.refreshToken = newRefresh
	d.mu.Unlock()

	if d.persist != nil {
		if err := d.persist(ctx, domain.AuthCredentials{
			AccessToken:  access,
			RefreshToken: newRefresh,
			TokenExpires: expiresAt,
		}); err != nil {
			return "", domain.Wrap(domain.CodeInternal, err)
		}
	}
	return access, nil
}

func (d *Driver) refreshSession(ctx context.Context, accessToken string) error {
	params := clientSuffix()
	params.Set("appId", appID)
	params.Set("accessToken", accessToken)
	var out sessionResp
	if err := d.rawJSON(ctx, http.MethodGet, apiURL+"/getSessionForPC.action", params, nil, map[string]string{"X-Request-ID": newRequestID()}, &out); err != nil {
		return err
	}
	if !successResCode(out.ResCode) {
		msg := strings.TrimSpace(out.ResMessage)
		if msg == "" {
			msg = "刷新会话失败"
		}
		return domain.Errorf(domain.CodeAuthExpired, "%s", msg)
	}
	if d.isFamily() {
		if out.FamilySessionKey == "" || out.FamilySessionSecret == "" {
			return domain.Errorf(domain.CodeAuthExpired, "刷新会话成功但缺少家庭云会话信息")
		}
	} else if out.SessionKey == "" || out.SessionSecret == "" {
		return domain.Errorf(domain.CodeAuthExpired, "刷新会话成功但缺少个人云会话信息")
	}
	d.mu.Lock()
	d.sessionKey = out.SessionKey
	d.sessionSecret = out.SessionSecret
	d.familyKey = out.FamilySessionKey
	d.familySecret = out.FamilySessionSecret
	d.loginName = out.LoginName
	if out.RefreshToken != "" {
		d.refreshToken = out.RefreshToken
	}
	d.mu.Unlock()
	return nil
}

func firstPositiveNumber(nums ...jsonNumber) int64 {
	for _, n := range nums {
		if n.String() == "" {
			continue
		}
		v, err := strconv.ParseInt(n.String(), 10, 64)
		if err == nil && v > 0 {
			return v
		}
	}
	return 0
}

type jsonNumber interface{ String() string }
