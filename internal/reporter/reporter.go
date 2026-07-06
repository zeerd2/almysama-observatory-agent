package reporter

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeerd2/almysama-observatory-agent/internal/config"
)

type Client struct {
	Config *config.Config
	HTTP   *http.Client
}

func New(cfg *config.Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &Client{
		Config: cfg,
		HTTP:   &http.Client{Timeout: 25 * time.Second, Transport: transport},
	}
}

type EnrollResponse struct {
	OK          bool   `json:"ok"`
	AgentID     string `json:"agent_id"`
	AgentSecret string `json:"agent_secret"`
	Error       string `json:"error"`
}

type ConfigResponse struct {
	OK                       bool   `json:"ok"`
	ReportIntervalSeconds    int    `json:"report_interval_seconds"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
	Error                    string `json:"error"`
}

func (c *Client) Enroll(payload interface{}) (*EnrollResponse, error) {
	var response EnrollResponse
	if err := c.postJSON("/agent/enroll", payload, false, &response); err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, fmt.Errorf("enroll rejected: %s", response.Error)
	}
	return &response, nil
}

func (c *Client) Heartbeat(payload interface{}) error {
	var response struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.postJSON("/agent/heartbeat", payload, true, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("heartbeat rejected: %s", response.Error)
	}
	return nil
}

func (c *Client) Report(payload interface{}) error {
	var response struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.postJSON("/agent/report", payload, true, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("report rejected: %s", response.Error)
	}
	return nil
}

func (c *Client) PullConfig() (*ConfigResponse, error) {
	var response ConfigResponse
	if err := c.getJSON("/agent/config", &response); err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, fmt.Errorf("config rejected: %s", response.Error)
	}
	return &response, nil
}

func (c *Client) postJSON(path string, payload interface{}, signed bool, out interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.url(path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if signed {
		c.sign(req, body)
	}
	return c.do(req, out)
}

func (c *Client) getJSON(path string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, c.url(path), nil)
	if err != nil {
		return err
	}
	c.sign(req, nil)
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out interface{}) error {
	req.Header.Set("User-Agent", "almysama-agent")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *Client) sign(req *http.Request, body []byte) {
	ts := time.Now().UTC().Format(time.RFC3339)
	req.Header.Set("X-Almy-Agent-ID", c.Config.AgentID)
	req.Header.Set("X-Almy-Timestamp", ts)
	req.Header.Set("X-Almy-Signature", signature(c.Config.AgentSecret, ts, body))
}

func (c *Client) url(path string) string {
	return strings.TrimRight(c.Config.Server, "/") + path
}

func signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
