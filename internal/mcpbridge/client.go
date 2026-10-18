// Package mcpbridge exposes the authenticated Task Manager API over local MCP.
package mcpbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	APIURL string
	APIKey string
	UserID string
	AppID  string
}

type Client struct {
	baseURL string
	config  Config
	http    *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.APIURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("TASKMANAGER_API_URL must be an absolute API URL without credentials, query, or fragment")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, errors.New("TASKMANAGER_API_URL requires HTTPS except on loopback")
		}
	}
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.UserID) == "" || strings.TrimSpace(cfg.AppID) == "" {
		return nil, errors.New("TASKMANAGER_API_KEY, TASKMANAGER_USER_ID and TASKMANAGER_APP_ID are required")
	}
	return &Client{baseURL: strings.TrimRight(cfg.APIURL, "/"), config: cfg, http: &http.Client{
		Timeout: 30 * time.Second,
		// Never forward credentials across a redirect, including same-host redirects.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) call(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, errors.New("cannot encode request")
		}
	}
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("cannot create API request")
	}
	req.Header.Set("X-APIKEY", c.config.APIKey)
	req.Header.Set("USER-ID", c.config.UserID)
	req.Header.Set("APP-ID", c.config.AppID)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Task Manager API unavailable or request timed out")
	}
	defer res.Body.Close()
	// Error bodies may contain infrastructure details or credentials; do not echo them.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case 401:
			return nil, errors.New("API authentication failed; check or renew your credentials")
		case 403:
			return nil, errors.New("permission denied by Task Manager")
		case 404:
			return nil, errors.New("resource not found or inaccessible")
		case 409:
			return nil, errors.New("conflict: task is already assigned to another user; refresh before proceeding")
		case 400:
			return nil, errors.New("API rejected the arguments; check IDs, filters, status and comment length")
		default:
			return nil, fmt.Errorf("Task Manager API returned HTTP %d; do not assume the operation succeeded", res.StatusCode)
		}
	}
	const maxResponse = 4 << 20
	data, err = io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return nil, errors.New("cannot read API response or response too large; use a smaller page")
	}
	if !json.Valid(data) {
		return nil, errors.New("API returned invalid JSON")
	}
	return data, nil
}
