package transport

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"sentinel/agent/internal/models"
)

type Client struct {
	serverURL  string
	apiKey     string
	httpClient *http.Client
}

func New(serverURL, apiKey string, insecureSkipTLSVerify bool) *Client {
	transport := &http.Transport{}
	if insecureSkipTLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		serverURL:  serverURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second, Transport: transport},
	}
}

// Enroll exchanges a one-time enrollment token for a permanent host ID and
// API key.
func Enroll(serverURL, enrollmentToken string, insecureSkipTLSVerify bool) (hostID, apiKey string, err error) {
	c := New(serverURL, "", insecureSkipTLSVerify)

	body, _ := json.Marshal(map[string]string{"enrollment_token": enrollmentToken})
	resp, err := c.httpClient.Post(serverURL+"/api/v1/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("enroll failed: server returned %s", resp.Status)
	}

	var out struct {
		HostID string `json:"host_id"`
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.HostID, out.APIKey, nil
}

// Push sends a batch of samples to the ingest endpoint.
func (c *Client) Push(payload models.IngestPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, c.serverURL+"/api/v1/ingest", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("ingest failed: server returned %s", resp.Status)
	}
	return nil
}
