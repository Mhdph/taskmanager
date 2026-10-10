package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	baseURL string

	httpClient *http.Client
}

func NewClient(
	baseURL string,
	httpClient *http.Client,
) *Client {
	return &Client{
		baseURL: strings.TrimRight(
			baseURL,
			"/",
		),

		httpClient: httpClient,
	}
}

func (c *Client) Send(
	ctx context.Context,
	input SendInput,
) error {
	payload, err := json.Marshal(
		input,
	)
	if err != nil {
		return fmt.Errorf(
			"marshal notification payload: %w",
			err,
		)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/notifications",
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf(
			"create notification request: %w",
			err,
		)
	}

	req.Header.Set(
		"Content-Type",
		"application/json",
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf(
			"send notification request: %w",
			err,
		)
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 200 &&
		resp.StatusCode < 300 {

		return nil
	}

	body, _ := io.ReadAll(
		io.LimitReader(
			resp.Body,
			4096,
		),
	)

	return fmt.Errorf(
		"notification service returned status %d: %s",
		resp.StatusCode,
		strings.TrimSpace(
			string(body),
		),
	)
}
