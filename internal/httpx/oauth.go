package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"litepan/internal/domain"
)

func WrapTransportError(err error) error {
	if err == nil {
		return nil
	}
	return domain.Wrap(domain.CodeDriverError, err)
}

// PostOAuthProxyJSON 向 OAuth 代理发送 JSON POST，并按代理约定解析响应与错误。
func PostOAuthProxyJSON(ctx context.Context, client *http.Client, fullURL string, body, out any) error {
	if client == nil {
		client = NewClient(ClientOptions{})
	}
	resp, data, err := DoJSON(ctx, client, http.MethodPost, fullURL, nil, body, map[string]string{
		"Content-Type": "application/json",
	}, 1<<20)
	if err != nil {
		return WrapTransportError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return OAuthProxyHTTPError(resp.StatusCode, string(data))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return OAuthProxyDecodeError(err)
		}
	}
	return nil
}

func OAuthProxyHTTPError(status int, body string) error {
	msg := "oauth 代理刷新失败 HTTP " + strconv.Itoa(status) + "：" + Truncate([]byte(body), 300)
	if status == http.StatusUnauthorized || status == http.StatusForbidden || domain.TokenAuthFailureMessage(msg) {
		return domain.Errorf(domain.CodeAuthExpired, "%s", msg)
	}
	return domain.Errorf(domain.CodeDriverError, "%s", msg)
}

func OAuthProxyDecodeError(err error) error {
	if err == nil {
		return nil
	}
	return domain.Wrap(domain.CodeDriverError, err)
}
