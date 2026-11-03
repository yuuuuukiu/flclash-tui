// Package engine 提供 TUI 与 CLI 共用的内核配置生成、内置内核释放、重载等动作，
// 保证两条调用路径行为完全一致。
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"flclash-tui/internal/clashapi"
	"flclash-tui/internal/coreasset"
	"flclash-tui/internal/corecfg"
	"flclash-tui/internal/coremgr"
	"flclash-tui/internal/paths"
	"flclash-tui/internal/profiles"
	"flclash-tui/internal/settings"
)

// Load 读取设置与订阅存储（不启动内核）。
func Load() (*settings.Settings, *profiles.Store, error) {
	if err := paths.EnsureHome(); err != nil {
		return nil, nil, err
	}
	s, err := settings.Load()
	if err != nil {
		return nil, nil, err
	}
	return s, profiles.NewStore(), nil
}

// EnsureCore 保证 CorePath 指向一个可用的 mihomo：优先内置内核（首次运行释放），
// 其次沿用设置里已配置的路径。返回最终使用的路径（可能为空）。
func EnsureCore(s *settings.Settings) (string, error) {
	if s.CoreMode != settings.CoreModeManaged {
		return s.CorePath, nil
	}
	if s.CorePath != "" {
		if _, err := os.Stat(s.CorePath); err == nil {
			return s.CorePath, nil
		}
	}
	if coreasset.Available() {
		p, err := coreasset.Ensure(paths.CoreBinFile())
		if err != nil {
			return "", err
		}
		if s.CorePath != p {
			s.CorePath = p
			_ = s.Save()
		}
		return p, nil
	}
	return s.CorePath, nil
}

// GenerateConfig 由激活订阅 + 设置生成最终内核配置。
func GenerateConfig(s *settings.Settings, ps *profiles.Store) (map[string]any, error) {
	var raw map[string]any
	if p, ok := ps.Active(); ok {
		data, err := os.ReadFile(p.File())
		if err != nil {
			return nil, fmt.Errorf("读取订阅失败: %w", err)
		}
		raw, err = corecfg.ParseYAML(data)
		if err != nil {
			return nil, err
		}
	}
	return corecfg.Generate(raw, s)
}

// WriteCoreConfig 生成并落盘 config.yaml。
func WriteCoreConfig(s *settings.Settings, ps *profiles.Store) error {
	cfg, err := GenerateConfig(s, ps)
	if err != nil {
		return err
	}
	out, err := corecfg.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.CoreDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(paths.CoreConfigFile(), out, 0o600)
}

// ApplyFetchConfig 把设置中的订阅拉取回退配置下发给订阅存储。
func ApplyFetchConfig(s *settings.Settings, ps *profiles.Store) {
	ps.SetFetchConfig(profiles.FetchConfig{
		Enabled:      s.ProxyFallback,
		FallbackPort: s.MixedPort + 1,
		MixedPort:    s.MixedPort,
	})
}

// Client 构造控制器客户端。
func Client(s *settings.Settings) *clashapi.Client {
	c := clashapi.New(s.APIBase(), s.Secret)
	c.TestURL = s.TestURL
	return c
}

// Reload 在 attach 模式下让外部控制器重载配置。
func Reload(s *settings.Settings) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return Client(s).ReloadConfig(ctx, "", true)
}

// Reachable 探测控制器是否可用。
func Reachable(s *settings.Settings, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := Client(s).Version(ctx)
	return err == nil
}

// StartCore 启动托管内核（阻塞直到就绪或失败）。
func StartCore(s *settings.Settings, m *coremgr.Manager) error {
	if _, err := EnsureCore(s); err != nil {
		return err
	}
	if s.CorePath == "" {
		return fmt.Errorf("未找到 mihomo 内核：当前平台未内置，且设置中未指定 corePath")
	}
	if err := WriteCoreConfig(s, profiles.NewStore()); err != nil {
		return err
	}
	return m.Start(s)
}

// Apply 应用当前设置：写配置 → 重启内核（managed）或热重载（attach）。
func Apply(s *settings.Settings, ps *profiles.Store, m *coremgr.Manager) error {
	if err := WriteCoreConfig(s, ps); err != nil {
		return err
	}
	if s.CoreMode == settings.CoreModeManaged {
		if m.State() == coremgr.StateRunning {
			return m.Restart(s)
		}
		return m.Start(s)
	}
	return Reload(s)
}

// HotReload 写配置后让正在运行的内核原地重载（不重启进程，不断连接）。
// 用 payload 内联下发而不是 path：避免内核的 SAFE_PATHS 限制把外部数据目录的路径拒掉。
// 控制器不可达时只写配置并返回 ErrNotRunning。
func HotReload(s *settings.Settings, ps *profiles.Store) error {
	cfg, err := GenerateConfig(s, ps)
	if err != nil {
		return err
	}
	out, err := corecfg.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := WriteCoreConfig(s, ps); err != nil {
		return err
	}
	if !Reachable(s, 1500*time.Millisecond) {
		return ErrNotRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Client(s).ReloadConfigPayload(ctx, string(out), true); err == nil {
		return nil
	}
	// 老版本内核不支持 payload：回退到按路径重载
	return Client(s).ReloadConfig(ctx, paths.CoreConfigFile(), true)
}

// ErrNotRunning 表示内核当前没在跑（配置已写入，下次启动生效）。
var ErrNotRunning = errors.New("内核未运行")
