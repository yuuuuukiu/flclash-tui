// Package clashapi 实现 mihomo external controller 的 REST 与 WebSocket 客户端。
package clashapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	Base    string
	Secret  string
	HTTP    *http.Client
	TestURL string
}

func New(base, secret string) *Client {
	return &Client{
		Base:   base,
		Secret: secret,
		HTTP:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	u := c.Base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Body: string(data)}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// ---------- 模型 ----------

type Version struct {
	Version string `json:"version"`
	Meta    bool   `json:"meta"`
}

type Proxy struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	UDP      bool        `json:"udp"`
	History  []DelayHist `json:"history"`
	All      []string    `json:"all,omitempty"`
	Now      string      `json:"now,omitempty"`
	XUDP     bool        `json:"xudp,omitempty"`
	Alive    bool        `json:"alive,omitempty"`
	Provider string      `json:"-"`
}

type DelayHist struct {
	Time  string `json:"time"`
	Delay int    `json:"delay"`
}

// ExtraGroup 是 mihomo v1.19+ 在组上附带的额外信息（例如 fallback 组的节点分组）。
type ExtraGroup struct {
	Proxies []string `json:"proxies"`
	Now     string   `json:"now"`
}

type ProxiesResp struct {
	Proxies map[string]Proxy `json:"proxies"`
}

type Rule struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
	Size    int    `json:"size"`
}

type RulesResp struct {
	Rules []Rule `json:"rules"`
}

type ConnMetadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	DestinationIP     string `json:"destinationIP"`
	SourcePort        string `json:"sourcePort"`
	DestinationPort   string `json:"destinationPort"`
	GeoIP             string `json:"geoip,omitempty"`
	Host              string `json:"host,omitempty"`
	DNSMode           string `json:"dnsMode,omitempty"`
	Process           string `json:"process,omitempty"`
	ProcessPath       string `json:"processPath,omitempty"`
	SpecialProxy      string `json:"specialProxy,omitempty"`
	SpecialRules      string `json:"specialRules,omitempty"`
	RemoteDestination string `json:"remoteDestination,omitempty"`
}

type Connection struct {
	ID          string       `json:"id"`
	Upload      int64        `json:"upload"`
	Download    int64        `json:"download"`
	Start       time.Time    `json:"start"`
	Chains      []string     `json:"chains"`
	Rule        string       `json:"rule"`
	RulePayload string       `json:"rulePayload"`
	Metadata    ConnMetadata `json:"metadata"`
}

type ConnectionsResp struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
	Memory        int64        `json:"memory"`
}

type GeneralConfig struct {
	Port          int            `json:"port"`
	SocksPort     int            `json:"socks-port"`
	MixedPort     int            `json:"mixed-port"`
	RedirPort     int            `json:"redir-port"`
	TProxyPort    int            `json:"tproxy-port"`
	AllowLan      bool           `json:"allow-lan"`
	BindAddress   string         `json:"bind-address"`
	Mode          string         `json:"mode"`
	LogLevel      string         `json:"log-level"`
	IPv6          bool           `json:"ipv6"`
	Tun           map[string]any `json:"tun"`
	TCPConcurrent bool           `json:"tcp-concurrent"`
	UnifiedDelay  bool           `json:"unified-delay"`
}

type ProxyProvider struct {
	Name             string   `json:"name"`
	Type             string   `json:"type"`        // Proxy / Rule
	VehicleType      string   `json:"vehicleType"` // HTTP / File / Compatible
	Proxies          []Proxy  `json:"proxies"`
	SubscriptionInfo *SubInfo `json:"subscriptionInfo"`
	TestUrl          string   `json:"testUrl"`
	UpdatedAt        string   `json:"updatedAt"`
	RuleCount        int      `json:"ruleCount"`
	Behavior         string   `json:"behavior"`
	Format           string   `json:"format"`
}

type SubInfo struct {
	Upload   int64 `json:"Upload"`
	Download int64 `json:"Download"`
	Total    int64 `json:"Total"`
	Expire   int64 `json:"Expire"`
}

type ProvidersResp struct {
	Providers map[string]ProxyProvider `json:"providers"`
}

type DNSQueryResult struct {
	Answer []struct {
		Name string `json:"name"`
		TTL  int    `json:"TTL"`
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

// ---------- REST ----------

func (c *Client) Version(ctx context.Context) (*Version, error) {
	var v Version
	if err := c.do(ctx, http.MethodGet, "/version", nil, nil, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) GetProxies(ctx context.Context) (*ProxiesResp, error) {
	var r ProxiesResp
	if err := c.do(ctx, http.MethodGet, "/proxies", nil, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SelectProxy 切换组内节点。
func (c *Client) SelectProxy(ctx context.Context, group, name string) error {
	return c.do(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), nil, map[string]string{"name": name}, nil)
}

// ProxyDelay 单节点延迟测试。
func (c *Client) ProxyDelay(ctx context.Context, name, testURL string, timeoutMS int) (int, error) {
	q := url.Values{"url": {testURL}, "timeout": {fmt.Sprint(timeoutMS)}}
	var r struct {
		Delay   int    `json:"delay"`
		Message string `json:"message"`
	}
	if err := c.do(ctx, http.MethodGet, "/proxies/"+url.PathEscape(name)+"/delay", q, nil, &r); err != nil {
		if apiErr, ok := err.(*APIError); ok {
			if apiErr.Status == http.StatusNotFound {
				return 0, fmt.Errorf("节点不存在或不可测速: %s", name)
			}
			if r.Message != "" {
				apiErr.Body = r.Message
			}
		}
		return 0, err
	}
	return r.Delay, nil
}

// GroupDelay 组内全部节点并发测速，返回 name->delay。
func (c *Client) GroupDelay(ctx context.Context, group, testURL string, timeoutMS int) (map[string]int, error) {
	q := url.Values{"url": {testURL}, "timeout": {fmt.Sprint(timeoutMS)}}
	var r map[string]int
	if err := c.do(ctx, http.MethodGet, "/group/"+url.PathEscape(group)+"/delay", q, nil, &r); err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.Status == http.StatusNotFound {
			return nil, fmt.Errorf("策略组不存在或不可测速: %s", group)
		}
		return nil, err
	}
	return r, nil
}

func (c *Client) GetRules(ctx context.Context) (*RulesResp, error) {
	var r RulesResp
	if err := c.do(ctx, http.MethodGet, "/rules", nil, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) GetConnections(ctx context.Context) (*ConnectionsResp, error) {
	var r ConnectionsResp
	if err := c.do(ctx, http.MethodGet, "/connections", nil, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) CloseConnection(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil, nil, nil)
}

func (c *Client) CloseAllConnections(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/connections", nil, nil, nil)
}

func (c *Client) GetConfigs(ctx context.Context) (*GeneralConfig, error) {
	var r GeneralConfig
	if err := c.do(ctx, http.MethodGet, "/configs", nil, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// PatchConfigs 热更新配置。
func (c *Client) PatchConfigs(ctx context.Context, patch map[string]any) error {
	return c.do(ctx, http.MethodPatch, "/configs", nil, patch, nil)
}

// ReloadConfig 让内核从指定路径重载配置文件。
// mihomo 的 PUT /configs 把路径与内容放在请求体里（query 只接受 force）。
func (c *Client) ReloadConfig(ctx context.Context, path string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	body := map[string]any{}
	if path != "" {
		body["path"] = path
	}
	return c.do(ctx, http.MethodPut, "/configs", q, body, nil)
}

// ReloadConfigPayload 用内联 YAML 内容热重载（不依赖磁盘文件）。
func (c *Client) ReloadConfigPayload(ctx context.Context, payload string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	return c.do(ctx, http.MethodPut, "/configs", q, map[string]any{"payload": payload}, nil)
}

func (c *Client) FlushFakeIP(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/cache/fakeip/flush", nil, nil, nil)
}

func (c *Client) UpdateGeo(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/configs/geo", nil, nil, nil)
}

func (c *Client) UpgradeCore(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/upgrade", nil, nil, nil)
}

func (c *Client) GetProviders(ctx context.Context) (*ProvidersResp, error) {
	var r ProvidersResp
	if err := c.do(ctx, http.MethodGet, "/providers/proxies", nil, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) UpdateProvider(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPut, "/providers/proxies/"+url.PathEscape(name), nil, nil, nil)
}

func (c *Client) HealthCheckProvider(ctx context.Context, name string) error {
	q := url.Values{"url": {c.TestURL}, "timeout": {"5000"}}
	return c.do(ctx, http.MethodGet, "/providers/proxies/"+url.PathEscape(name)+"/healthcheck", q, nil, nil)
}

func (c *Client) UpdateRuleProvider(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPut, "/providers/rules/"+url.PathEscape(name), nil, nil, nil)
}

func (c *Client) DNSQuery(ctx context.Context, name, qtype string) (*DNSQueryResult, error) {
	q := url.Values{"name": {name}}
	if qtype != "" {
		q.Set("type", qtype)
	}
	var r DNSQueryResult
	if err := c.do(ctx, http.MethodGet, "/dns/query", q, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
