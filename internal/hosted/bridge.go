package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var ErrInvalidBridgeToken = errors.New("invalid bridge token")

type BridgeSession struct {
	Token           string `json:"token"`
	AppID           string `json:"appId"`
	UserID          string `json:"userId"`
	FromSupportMenu string `json:"fromSupportMenu"`
}

func (c *Client) ConfirmBridgeToken(ctx context.Context, token string) (BridgeSession, error) {
	payload, err := json.Marshal(graphQLRequest{
		Query: `query nupConfirmBridgeToken($bridgeToken: String) {
			nupConfirmBridgeToken(bridgeToken: $bridgeToken) {
				token appId userId fromSupportMenu
			}
		}`,
		Variables: map[string]any{"bridgeToken": token},
	})
	if err != nil {
		return BridgeSession{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return BridgeSession{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return BridgeSession{}, fmt.Errorf("call bridge exchange: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return BridgeSession{}, fmt.Errorf("bridge exchange returned status %d", res.StatusCode)
	}
	var response struct {
		Data struct {
			Session BridgeSession `json:"nupConfirmBridgeToken"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil {
		return BridgeSession{}, fmt.Errorf("decode bridge exchange: %w", err)
	}
	session := response.Data.Session
	if len(response.Errors) != 0 || session.Token == "" || session.AppID == "" || session.UserID == "" {
		return BridgeSession{}, ErrInvalidBridgeToken
	}
	return session, nil
}
