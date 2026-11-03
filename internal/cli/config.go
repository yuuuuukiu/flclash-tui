package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"flclash-tui/internal/engine"
	"flclash-tui/internal/paths"
)

var configCmd = command{
	name:    "config",
	summary: "运行中的内核配置（show/path/mode/patch/reload）",
	usage: `flclash-tui config show [--json]        # 内核当前生效配置
flclash-tui config path                  # 生成的 config.yaml 路径
flclash-tui config mode rule|global|direct
flclash-tui config patch '{"mode":"global"}'
flclash-tui config reload [--force]
flclash-tui config flush-fakeip
flclash-tui config update-geo`,
	sub: []command{
		{name: "show", summary: "显示内核生效配置"},
		{name: "path", summary: "显示配置文件路径"},
		{name: "mode", summary: "切换代理模式"},
		{name: "patch", summary: "热更新任意配置项"},
		{name: "reload", summary: "让内核重载配置文件"},
		{name: "flush-fakeip", summary: "清空 FakeIP 缓存"},
		{name: "update-geo", summary: "更新 GeoIP/GeoSite"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("config"))
			return ExitUsage
		}
		switch args[0] {
		case "show", "get":
			return configShow(c, args[1:])
		case "path":
			return configPath(c, args[1:])
		case "mode":
			return configMode(c, args[1:])
		case "patch":
			return configPatch(c, args[1:])
		case "reload":
			return configReload(c, args[1:])
		case "flush-fakeip", "flush":
			return configFlushFakeIP(c, args[1:])
		case "update-geo", "geo":
			return configUpdateGeo(c, args[1:])
		}
		fmt.Fprintf(c.Err, "未知的 config 子命令: %s\n", args[0])
		return ExitUsage
	},
}

func configShow(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := engine.Client(c.Settings).GetConfigs(ctx)
	if err != nil {
		return c.fail(fmt.Errorf("控制器不可达（%s）: %w", c.Settings.ControllerAddr, err))
	}
	c.emit(map[string]any{"ok": true, "config": cfg}, func() {
		kvList(c.Out, [][2]string{
			{"混合端口", fmt.Sprint(cfg.MixedPort)},
			{"HTTP 端口", fmt.Sprint(cfg.Port)},
			{"SOCKS 端口", fmt.Sprint(cfg.SocksPort)},
			{"允许局域网", boolLabel(cfg.AllowLan)},
			{"模式", cfg.Mode},
			{"日志等级", cfg.LogLevel},
			{"IPv6", boolLabel(cfg.IPv6)},
			{"统一延迟", boolLabel(cfg.UnifiedDelay)},
			{"TCP 并发", boolLabel(cfg.TCPConcurrent)},
		})
		if tun, ok := cfg.Tun["enable"].(bool); ok {
			c.say("TUN: %s", boolLabel(tun))
		}
	})
	return ExitOK
}

func boolLabel(b bool) string {
	if b {
		return "开启"
	}
	return "关闭"
}

func configPath(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	p := paths.CoreConfigFile()
	if _, err := os.Stat(p); err != nil {
		c.emit(map[string]any{"ok": false, "path": p, "exists": false}, func() {
			c.say("配置文件不存在: %s（先执行一次 profile activate 或 core start）", p)
		})
		return ExitError
	}
	c.emit(map[string]any{"ok": true, "path": p, "exists": true}, func() { c.say("%s", p) })
	return ExitOK
}

func configMode(c *Ctx, argv []string) int {
	pa, err := parseNoOpts(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cfg, err := engine.Client(c.Settings).GetConfigs(ctx)
		if err != nil {
			return c.fail(err)
		}
		c.emit(map[string]any{"ok": true, "mode": cfg.Mode}, func() { c.say("%s", cfg.Mode) })
		return ExitOK
	}
	mode := pa.pos[0]
	switch mode {
	case "rule", "global", "direct":
	default:
		return c.fail(usageError("模式只能是 rule / global / direct"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Client(c.Settings).PatchConfigs(ctx, map[string]any{"mode": mode}); err != nil {
		return c.fail(err)
	}
	// 同步到设置，重启后保持一致
	c.Settings.Mode = mode
	if err := c.Settings.Save(); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true, "mode": mode}, func() {
		c.say("已切换模式: %s", modeLabel(mode))
	})
	return ExitOK
}

func modeLabel(m string) string {
	switch m {
	case "rule":
		return "规则"
	case "global":
		return "全局"
	case "direct":
		return "直连"
	}
	return m
}

func configPatch(c *Ctx, argv []string) int {
	pa, err := argSpec{short: map[string]string{"f": "file"}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	var raw string
	switch {
	case pa.get("file", "") != "":
		data, err := os.ReadFile(pa.get("file", ""))
		if err != nil {
			return c.fail(err)
		}
		raw = string(data)
	case len(pa.pos) > 0:
		raw = pa.pos[0]
	default:
		if isInteractive() {
			return c.fail(usageError("需要补丁内容：直接给出 JSON，或用 --file"))
		}
		data, err := readContent("")
		if err != nil {
			return c.fail(err)
		}
		raw = string(data)
	}
	var patch map[string]any
	if err := yaml.Unmarshal([]byte(raw), &patch); err != nil {
		return c.fail(fmt.Errorf("补丁解析失败: %w", err))
	}
	if len(patch) == 0 {
		return c.fail(usageError("补丁为空"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := engine.Client(c.Settings).PatchConfigs(ctx, patch); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true, "patch": patch}, func() {
		c.say("已应用配置补丁（%d 项）", len(patch))
	})
	return ExitOK
}

func configReload(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"force": false}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	if err := engine.WriteCoreConfig(c.Settings, c.Profiles); err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := engine.Client(c.Settings).ReloadConfig(ctx, paths.CoreConfigFile(), pa.boolOf("force")); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true, "path": paths.CoreConfigFile()}, func() {
		c.say("已让内核重载 %s", paths.CoreConfigFile())
	})
	return ExitOK
}

func configFlushFakeIP(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Client(c.Settings).FlushFakeIP(ctx); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true}, func() { c.say("FakeIP 缓存已清空") })
	return ExitOK
}

func configUpdateGeo(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := engine.Client(c.Settings).UpdateGeo(ctx); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true}, func() { c.say("GeoIP/GeoSite 数据库已更新") })
	return ExitOK
}
