package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type HTTPClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type sessionRecord struct {
	ID        string `json:"id"`
	ExpiresAt int64  `json:"expires_at"`
}

type sessionsResponse struct {
	Sessions []sessionRecord `json:"sessions"`
}

func NewHTTPClient(baseURL, apiKey string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *HTTPClient) Validate(ctx context.Context, userID int64, sessionID string) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/users/%d/sessions", c.baseURL, userID), nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("X-Internal-Api-Key", c.apiKey)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("session service returned status %d", response.StatusCode)
	}
	var payload sessionsResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return false, err
	}
	now := time.Now().Unix()
	for _, item := range payload.Sessions {
		if item.ID == sessionID && item.ExpiresAt > now {
			return true, nil
		}
	}
	return false, nil
}

func (c *HTTPClient) Health(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("session service health returned status %d", response.StatusCode)
	}
	return nil
}
