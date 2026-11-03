// Package settings 管理应用设置的持久化。
package settings

import (
	"encoding/json"
	"os"

	"flclash-tui/internal/paths"
)

// CoreMode：managed = 由 TUI 启动/停止 mihomo 内核；attach = 连接已有的 external controller。
const (
	CoreModeManaged = "managed"
	CoreModeAttach  = "attach"
)

type Settings struct {
	CoreMode string `json:"coreMode"` // managed | attach

	// managed 模式
	CorePath string `json:"corePath"` // mihomo 可执行文件路径

	// 控制器（两种模式共用）
	ControllerAddr string `json:"controllerAddr"` // 如 127.0.0.1:9090
	Secret         string `json:"secret"`

	// 常规覆写（写进生成的内核配置）
	MixedPort     int    `json:"mixedPort"`
	AllowLan      bool   `json:"allowLan"`
	Mode          string `json:"mode"` // rule | global | direct
	LogLevel      string `json:"logLevel"`
	IPv6          bool   `json:"ipv6"`
	UnifiedDelay  bool   `json:"unifiedDelay"`
	TCPConcurrent bool   `json:"tcpConcurrent"`
	Tun           bool   `json:"tun"`
	TunStack      string `json:"tunStack"` // gvisor | system | mixed

	// 测速
	TestURL      string `json:"testUrl"`
	DelayTimeout int    `json:"delayTimeout"` // 毫秒

	// 订阅自动更新
	AutoUpdate         bool `json:"autoUpdate"`
	AutoUpdateInterval int  `json:"autoUpdateInterval"` // 分钟

	// 订阅拉取失败时自动经代理端口重试（需要生成回退监听器，应用后生效）
	ProxyFallback bool `json:"proxyFallback"`

	// 右下角事件弹窗（操作结果 + 内核错误日志）
	EventPopup bool `json:"eventPopup"`

	// 系统代理（macOS: networksetup）
	SystemProxy        bool   `json:"systemProxy"`
	SystemProxyService string `json:"systemProxyService"`

	// 退出 TUI 时是否保留内核进程（默认 false：随 TUI 退出而停止）
	KeepCoreOnExit bool `json:"keepCoreOnExit"`
}

func Default() *Settings {
	return &Settings{
		CoreMode:           CoreModeManaged,
		ControllerAddr:     "127.0.0.1:9090",
		MixedPort:          7890,
		Mode:               "rule",
		LogLevel:           "info",
		IPv6:               false,
		UnifiedDelay:       true,
		TCPConcurrent:      true,
		TunStack:           "gvisor",
		TestURL:            "https://www.gstatic.com/generate_204",
		DelayTimeout:       5000,
		AutoUpdate:         true,
		AutoUpdateInterval: 360,
		ProxyFallback:      true,
		EventPopup:         true,
		SystemProxyService: "Wi-Fi",
	}
}

func Load() (*Settings, error) {
	s := Default()
	data, err := os.ReadFile(paths.SettingsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return Default(), nil // 配置损坏时回退默认值
	}
	if s.MixedPort <= 0 {
		s.MixedPort = 7890
	}
	if s.TestURL == "" {
		s.TestURL = "https://www.gstatic.com/generate_204"
	}
	// 旧版本配置缺少新增布尔键时保持默认开启（json 零值是 false）
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		if _, ok := raw["proxyFallback"]; !ok {
			s.ProxyFallback = true
		}
		if _, ok := raw["eventPopup"]; !ok {
			s.EventPopup = true
		}
	}
	return s, nil
}

func (s *Settings) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := paths.SettingsFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, paths.SettingsFile())
}

// APIBase 返回控制器 HTTP 基地址。
func (s *Settings) APIBase() string {
	addr := s.ControllerAddr
	if addr == "" {
		addr = "127.0.0.1:9090"
	}
	return "http://" + addr
}
