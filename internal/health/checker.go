package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

const DefaultEndpoint = "https://api.pushover.net/1/"

type Checker struct {
	endpoint   string
	httpClient *http.Client
}

func NewChecker(endpoint string, httpClient *http.Client) *Checker {
	return &Checker{
		endpoint:   endpoint,
		httpClient: httpClient,
	}
}

// Check verifies external connectivity. Any HTTP response is considered
// successful because authentication and API health are outside its scope.
func (c *Checker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return fmt.Errorf("create connectivity request: %w", err)
	}
	req.Header.Set("User-Agent", "MailNotifyer-Healthcheck/1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("external connectivity check failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}
