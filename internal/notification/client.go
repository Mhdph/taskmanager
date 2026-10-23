package notification

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

	"github.com/google/uuid"
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
	return c.SendWithID(ctx, input, uuid.NewString())
}

// SendWithID waits for provider acknowledgement and preserves the caller's
// idempotency key across retries of a durable notification.
func (c *Client) SendWithID(ctx context.Context, input SendInput, deliveryID string) error {
	payload, err := json.Marshal(
		input,
	)
	if err != nil {
		return fmt.Errorf(
			"marshal notification payload: %w",
			err,
		)
	}
	return c.sendPayload(ctx, payload, deliveryID)
}

type deliveryError struct {
	err       error
	retryable bool
}

func (e *deliveryError) Error() string { return e.err.Error() }
func (e *deliveryError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var delivery *deliveryError
	return errors.As(err, &delivery) && delivery.retryable
}

func (c *Client) sendPayload(ctx context.Context, payload []byte, deliveryID string) error {

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
	req.Header.Set("Idempotency-Key", deliveryID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		cause := err
		var urlError *url.Error
		if errors.As(err, &urlError) {
			cause = urlError.Err
		}
		var networkError net.Error
		return &deliveryError{err: fmt.Errorf(
			"send notification request: %w",
			err,
		), retryable: errors.As(cause, &networkError) || errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrUnexpectedEOF)}
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

	return &deliveryError{err: fmt.Errorf(
		"notification service returned status %d: %s",
		resp.StatusCode,
		strings.TrimSpace(
			string(body),
		),
	), retryable: resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500}
}
