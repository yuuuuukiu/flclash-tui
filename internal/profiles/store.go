// Package profiles 管理订阅（profile）：元数据存储、订阅下载/更新。
// 订阅内容与 FLClash 一样以原始 Clash YAML 存储，元数据存 profiles.json。
package profiles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"flclash-tui/internal/corecfg"
	"flclash-tui/internal/paths"
)

const (
	TypeURL  = "url"
	TypeFile = "file"
)

type SubInfo struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"` // unix 秒，0 = 永不过期
}

type Profile struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"` // url | file
	Label      string    `json:"label"`
	URL        string    `json:"url,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
	SubInfo    *SubInfo  `json:"subscriptionInfo,omitempty"`
	Active     bool      `json:"active"`
	AutoUpdate bool      `json:"autoUpdate"`
	Interval   int       `json:"intervalMinutes"` // 来自 profile-update-interval 头，0 = 用全局
	ViaProxy   bool      `json:"-"`               // 最近一次拉取是否经代理重试成功
}

// BytesUsed / BytesTotal / ExpireAt 提供展示用的换算。
func (p *Profile) File() string { return filepath.Join(paths.ProfilesDir(), p.ID+".yaml") }

type Store struct {
	mu       sync.Mutex
	Profiles []Profile `json:"profiles"`

	fetchMu   sync.Mutex
	fetchConf FetchConfig
}

// FetchConfig 订阅拉取的代理回退配置。
type FetchConfig struct {
	Enabled      bool
	FallbackPort int // 专用回退监听端口（绑定当前主出口组），<=0 时用 MixedPort
	MixedPort    int
}

// SetFetchConfig 设置拉取回退配置（线程安全）。
func (s *Store) SetFetchConfig(c FetchConfig) {
	s.fetchMu.Lock()
	s.fetchConf = c
	s.fetchMu.Unlock()
}

func (s *Store) getFetchConfig() FetchConfig {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	return s.fetchConf
}

func NewStore() *Store {
	s := &Store{}
	s.load()
	return s
}

func (s *Store) load() {
	data, err := os.ReadFile(paths.ProfilesFile())
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, s)
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := paths.ProfilesFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, paths.ProfilesFile())
}

func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Profile, len(s.Profiles))
	copy(out, s.Profiles)
	return out
}

func (s *Store) Get(id string) (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func (s *Store) Active() (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.Profiles {
		if p.Active {
			return p, true
		}
	}
	return Profile{}, false
}

// SetActive 激活指定订阅（同时取消其他）。
func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i := range s.Profiles {
		s.Profiles[i].Active = s.Profiles[i].ID == id
		if s.Profiles[i].ID == id {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("订阅不存在: %s", id)
	}
	return s.save()
}

// AddFromURL 下载订阅并添加。返回新 profile 与正文。
func (s *Store) AddFromURL(ctx context.Context, rawURL string) (*Profile, []byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil, fmt.Errorf("URL 不能为空")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, nil, fmt.Errorf("URL 必须以 http(s):// 开头")
	}
	body, meta, viaProxy, err := s.fetchSubscription(ctx, rawURL)
	if err != nil {
		return nil, nil, err
	}
	id := newID()
	p := &Profile{
		ID:         id,
		Type:       TypeURL,
		URL:        rawURL,
		Label:      meta.label,
		UpdatedAt:  time.Now(),
		SubInfo:    meta.subInfo,
		AutoUpdate: true,
		Interval:   meta.interval,
		ViaProxy:   viaProxy,
	}
	if err := os.WriteFile(p.File(), body, 0o600); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	s.Profiles = append(s.Profiles, *p)
	if len(s.Profiles) == 1 {
		s.Profiles[0].Active = true
	}
	err = s.save()
	s.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	return p, body, nil
}

// AddFromFile 把本地 YAML 收编为 profile。
func (s *Store) AddFromFile(path, label string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("文件为空")
	}
	id := newID()
	p := &Profile{ID: id, Type: TypeFile, Label: label, UpdatedAt: time.Now()}
	if p.Label == "" {
		p.Label = filepath.Base(path)
	}
	if err := os.WriteFile(p.File(), data, 0o600); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.Profiles = append(s.Profiles, *p)
	if len(s.Profiles) == 1 {
		s.Profiles[0].Active = true
	}
	err = s.save()
	s.mu.Unlock()
	return p, err
}

// AddFromContent 从直接粘贴的配置文本（YAML 或 JSON）创建订阅。
// 内容会被规范化为 YAML 后落盘。
func (s *Store) AddFromContent(label string, content []byte) (*Profile, []byte, error) {
	normalized, _, err := corecfg.Normalize(content)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(label) == "" {
		label = "自定义配置"
	}
	id := newID()
	p := &Profile{ID: id, Type: TypeFile, Label: label, UpdatedAt: time.Now()}
	if err := os.WriteFile(p.File(), normalized, 0o600); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	s.Profiles = append(s.Profiles, *p)
	if len(s.Profiles) == 1 {
		s.Profiles[0].Active = true
	}
	err = s.save()
	s.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	return p, normalized, nil
}

// SetContent 覆写已有订阅的配置内容（YAML 或 JSON），返回规范化后的内容。
func (s *Store) SetContent(id string, content []byte) ([]byte, error) {
	normalized, _, err := corecfg.Normalize(content)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	found := false
	for i := range s.Profiles {
		if s.Profiles[i].ID == id {
			s.Profiles[i].UpdatedAt = time.Now()
			found = true
		}
	}
	if !found {
		s.mu.Unlock()
		return nil, fmt.Errorf("订阅不存在")
	}
	if err := s.save(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	return normalized, os.WriteFile(filepath.Join(paths.ProfilesDir(), id+".yaml"), normalized, 0o600)
}

// Update 重新下载并覆盖订阅内容（仅 url 类型）。
func (s *Store) Update(ctx context.Context, id string) (*Profile, error) {
	s.mu.Lock()
	var p Profile
	found := false
	for _, it := range s.Profiles {
		if it.ID == id {
			p = it
			found = true
		}
	}
	s.mu.Unlock()
	if !found {
		return nil, fmt.Errorf("订阅不存在")
	}
	if p.Type != TypeURL {
		return nil, fmt.Errorf("本地文件订阅无法在线更新")
	}
	body, meta, viaProxy, err := s.fetchSubscription(ctx, p.URL)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(p.File(), body, 0o600); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p.ViaProxy = viaProxy
	for i := range s.Profiles {
		if s.Profiles[i].ID == id {
			s.Profiles[i].UpdatedAt = time.Now()
			if meta.label != "" && s.Profiles[i].Label == "" {
				// 更新时仅当原 label 为空才覆盖，尊重用户命名
				s.Profiles[i].Label = meta.label
			}
			if meta.interval > 0 {
				s.Profiles[i].Interval = meta.interval
			}
			s.Profiles[i].SubInfo = meta.subInfo
		}
	}
	return &p, s.save()
}

// Delete 删除订阅及其文件。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Profiles[:0]
	found := false
	for _, p := range s.Profiles {
		if p.ID == id {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return fmt.Errorf("订阅不存在")
	}
	s.Profiles = out
	if err := s.save(); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(paths.ProfilesDir(), id+".yaml"))
	return nil
}

// Rename 修改订阅标签。
func (s *Store) Rename(id, label string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Profiles {
		if s.Profiles[i].ID == id {
			s.Profiles[i].Label = label
			return s.save()
		}
	}
	return fmt.Errorf("订阅不存在")
}

// DueUpdate 返回需要自动更新的订阅列表。
func (s *Store) DueUpdate(globalIntervalMin int) []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Profile
	now := time.Now()
	for _, p := range s.Profiles {
		if p.Type != TypeURL || !p.AutoUpdate {
			continue
		}
		interval := p.Interval
		if interval <= 0 {
			interval = globalIntervalMin
		}
		if interval <= 0 {
			continue
		}
		if p.UpdatedAt.IsZero() || now.Sub(p.UpdatedAt) > time.Duration(interval)*time.Minute {
			out = append(out, p)
		}
	}
	return out
}

// ---------- 下载实现 ----------

type fetchMeta struct {
	label    string
	subInfo  *SubInfo
	interval int // 分钟，来自 profile-update-interval 头（小时）
}

var contentDispRe = regexp.MustCompile(`filename\*?=(?:UTF-8''|")?([^";]+)`)

// fetchSubscription 按当前 FetchConfig 拉取订阅：先直连，失败且启用回退时经代理端口重试。
// 返回值 usedProxy 表示最终结果是否来自代理重试。
func (s *Store) fetchSubscription(ctx context.Context, rawURL string) (data []byte, meta *fetchMeta, usedProxy bool, err error) {
	cfg := s.getFetchConfig()
	clients := []*http.Client{newHTTPClient(0, false)}
	if cfg.Enabled {
		port := cfg.FallbackPort
		if port <= 0 {
			port = cfg.MixedPort
		}
		if port > 0 {
			clients = append(clients, newHTTPClient(port, true))
		}
	}
	return fetchWithClients(ctx, rawURL, clients...)
}

// fetchWithClients 依次尝试每个客户端；非首个成功视为经代理重试成功。
func fetchWithClients(ctx context.Context, rawURL string, clients ...*http.Client) ([]byte, *fetchMeta, bool, error) {
	var errs []error
	for i, c := range clients {
		if c == nil {
			continue
		}
		data, meta, err := fetchOnce(ctx, c, rawURL)
		if err == nil {
			return data, meta, i > 0, nil
		}
		errs = append(errs, err)
	}
	switch len(errs) {
	case 0:
		return nil, nil, false, fmt.Errorf("无可用的拉取通道")
	case 1:
		return nil, nil, false, errs[0]
	default:
		return nil, nil, true, fmt.Errorf("直连失败(%s)；经代理重试仍失败(%s)", briefErr(errs[0]), briefErr(errs[len(errs)-1]))
	}
}

// briefErr 提取错误最有信息量的末段，便于在有限宽度的通知里展示。
func briefErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

func newHTTPClient(proxyPort int, viaProxy bool) *http.Client {
	c := &http.Client{Timeout: 30 * time.Second}
	if viaProxy && proxyPort > 0 {
		c.Transport = &http.Transport{
			Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", proxyPort)}),
		}
	} else {
		c.Transport = &http.Transport{Proxy: nil} // 直连，忽略环境代理变量
	}
	return c
}

func fetchOnce(ctx context.Context, client *http.Client, rawURL string) ([]byte, *fetchMeta, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "clash.meta/v1.19.13")
	resp, err := client.Do(req)
	if err != nil {
		// 去掉 url.Error 冗长的 URL 前缀，保留底层原因
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
	if err != nil {
		return nil, nil, err
	}
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("订阅内容为空")
	}
	meta := &fetchMeta{}
	// 解析订阅流量信息（对齐 FLClash）
	if ui := resp.Header.Get("subscription-userinfo"); ui != "" {
		meta.subInfo = parseSubInfo(ui)
	}
	// 解析订阅名（对齐 FLClash 的 content-disposition 处理，filename* 优先）
	if cd := resp.Header.Get("content-disposition"); cd != "" {
		name := ""
		isExt := false
		for _, m := range contentDispRe.FindAllStringSubmatch(cd, -1) {
			name = m[1]
			isExt = strings.Contains(m[0], "filename*=")
			if isExt {
				break // filename*（RFC 5987）优先于普通 filename
			}
		}
		if name != "" {
			if isExt {
				// filename*=UTF-8''xxx 需要 URL 解码
				if dec, err := unescape(name); err == nil {
					name = dec
				}
			}
			meta.label = trimConfigExt(strings.TrimSpace(name))
		}
	}
	if h := resp.Header.Get("profile-update-interval"); h != "" {
		if hours, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && hours > 0 {
			meta.interval = hours * 60
		}
	}
	// 简单内容嗅探：base64 订阅（分享链接列表）不支持，仅支持 clash YAML
	body := string(data)
	if !strings.Contains(body, "proxies:") && !strings.Contains(body, "proxy-providers:") {
		if !strings.Contains(body, "rules:") {
			return nil, nil, fmt.Errorf("内容不是 Clash 订阅（缺少 proxies/rules 字段）")
		}
	}
	return data, meta, nil
}

// trimConfigExt 去掉订阅文件名里的配置后缀，只留可读名称。
func trimConfigExt(name string) string {
	for _, ext := range []string{".yaml", ".yml", ".conf", ".txt", ".json"} {
		if strings.HasSuffix(strings.ToLower(name), ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

func parseSubInfo(h string) *SubInfo {
	info := &SubInfo{}
	for _, part := range strings.Split(h, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "upload":
			info.Upload = v
		case "download":
			info.Download = v
		case "total":
			info.Total = v
		case "expire":
			info.Expire = v
		}
	}
	return info
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
