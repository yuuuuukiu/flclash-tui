package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"flclash-tui/internal/engine"
	"flclash-tui/internal/osshare"
	"flclash-tui/internal/paths"
	"flclash-tui/internal/settings"
)

var settingsCmd = command{
	name:    "settings",
	summary: "应用设置（get/set/list/reset/apply）",
	usage: `flclash-tui settings list [--json]
flclash-tui settings get <键>
flclash-tui settings set <键> <值> [--apply]
flclash-tui settings reset
flclash-tui settings apply            # 重新生成配置并重启内核

键名: coreMode corePath controllerAddr secret mixedPort allowLan mode logLevel
      ipv6 unifiedDelay tcpConcurrent tun tunStack testUrl delayTimeout
      autoUpdate autoUpdateInterval proxyFallback eventPopup
      systemProxy systemProxyService keepCoreOnExit`,
	sub: []command{
		{name: "list", summary: "列出全部设置"},
		{name: "get", summary: "读取单个设置"},
		{name: "set", summary: "修改设置"},
		{name: "reset", summary: "恢复默认设置"},
		{name: "apply", summary: "应用设置并重启内核"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("settings"))
			return ExitUsage
		}
		switch args[0] {
		case "list", "ls", "show":
			return settingsList(c, args[1:])
		case "get":
			return settingsGet(c, args[1:])
		case "set":
			return settingsSet(c, args[1:])
		case "reset":
			return settingsReset(c, args[1:])
		case "apply":
			return settingsApply(c, args[1:])
		}
		fmt.Fprintf(c.Err, "未知的 settings 子命令: %s\n", args[0])
		return ExitUsage
	},
}

func settingsList(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	fields := settingsFields()
	rows := make([][2]string, 0, len(fields))
	for _, f := range fields {
		rows = append(rows, [2]string{f.name, f.str(c.Settings)})
	}
	if c.JSON {
		m := map[string]any{}
		for _, f := range fields {
			m[f.name] = f.value(c.Settings)
		}
		c.emit(map[string]any{"ok": true, "settings": m, "path": paths.SettingsFile()}, func() {})
		return ExitOK
	}
	kvList(c.Out, rows)
	c.say("\n配置文件: %s", paths.SettingsFile())
	return ExitOK
}

func settingsGet(c *Ctx, argv []string) int {
	pa, err := parseNoOpts(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) == 0 {
		return c.fail(usageError("需要一个键名（用 settings list 查看）"))
	}
	f, ok := findField(pa.pos[0])
	if !ok {
		return c.fail(usageError("未知的设置键: %s", pa.pos[0]))
	}
	c.emit(map[string]any{"ok": true, "key": f.name, "value": f.value(c.Settings)}, func() {
		c.say("%s", f.str(c.Settings))
	})
	return ExitOK
}

func settingsSet(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"apply": false}, alias: map[string]string{"a": "apply"}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) < 2 {
		return c.fail(usageError("用法: settings set <键> <值> [--apply]"))
	}
	f, ok := findField(pa.pos[0])
	if !ok {
		return c.fail(usageError("未知的设置键: %s", pa.pos[0]))
	}
	raw := strings.Join(pa.pos[1:], " ")
	if err := f.set(c.Settings, raw); err != nil {
		return c.fail(err)
	}
	if err := c.Settings.Save(); err != nil {
		return c.fail(err)
	}
	applied := false
	if pa.boolOf("apply") {
		if code := applySettings(c); code != ExitOK {
			return code
		}
		applied = true
	}
	c.emit(map[string]any{"ok": true, "key": f.name, "value": f.value(c.Settings), "applied": applied}, func() {
		c.say("%s = %s", f.name, f.str(c.Settings))
		if !applied {
			c.say("（用 settings apply 或 --apply 让它生效）")
		}
	})
	return ExitOK
}

func settingsReset(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"apply": false}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	def := settings.Default()
	*c.Settings = *def
	if err := c.Settings.Save(); err != nil {
		return c.fail(err)
	}
	applied := false
	if pa.boolOf("apply") {
		if code := applySettings(c); code != ExitOK {
			return code
		}
		applied = true
	}
	c.emit(map[string]any{"ok": true, "reset": true, "applied": applied}, func() {
		c.say("已恢复默认设置")
	})
	return ExitOK
}

func settingsApply(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	if code := applySettings(c); code != ExitOK {
		return code
	}
	c.emit(map[string]any{"ok": true}, func() { c.say("已应用设置并重载内核") })
	return ExitOK
}

// applySettings 把设置落盘并让内核生效（managed 重启 / attach 热重载）。
func applySettings(c *Ctx) int {
	if _, err := engine.EnsureCore(c.Settings); err != nil {
		return c.fail(err)
	}
	engine.ApplyFetchConfig(c.Settings, c.Profiles)
	if err := c.Settings.Save(); err != nil {
		return c.fail(err)
	}
	if err := engine.WriteCoreConfig(c.Settings, c.Profiles); err != nil {
		return c.fail(err)
	}
	if c.Settings.CoreMode == settings.CoreModeAttach {
		if !engine.Reachable(c.Settings, 2*time.Second) {
			if !c.JSON {
				c.say("（控制器不可达，配置已写入）")
			}
			return ExitOK
		}
		if err := engine.Reload(c.Settings); err != nil {
			return c.fail(err)
		}
		return ExitOK
	}
	if !engine.Reachable(c.Settings, 1500*time.Millisecond) {
		if !c.JSON {
			c.say("（内核未运行，配置已写入，下次 core start 生效）")
		}
		return ExitOK
	}
	return restartManaged(c)
}

// ---------- 设置字段元数据 ----------

type settingsField struct {
	name  string
	str   func(*settings.Settings) string
	value func(*settings.Settings) any
	set   func(*settings.Settings, string) error
}

func settingsFields() []settingsField {
	boolField := func(name string, ptr func(*settings.Settings) *bool) settingsField {
		return settingsField{
			name:  name,
			str:   func(s *settings.Settings) string { return boolLabel(*ptr(s)) },
			value: func(s *settings.Settings) any { return *ptr(s) },
			set: func(s *settings.Settings, raw string) error {
				b, err := parseBool(raw)
				if err != nil {
					return err
				}
				*ptr(s) = b
				return nil
			},
		}
	}
	strField := func(name string, ptr func(*settings.Settings) *string) settingsField {
		return settingsField{
			name:  name,
			str:   func(s *settings.Settings) string { return *ptr(s) },
			value: func(s *settings.Settings) any { return *ptr(s) },
			set: func(s *settings.Settings, raw string) error {
				*ptr(s) = raw
				return nil
			},
		}
	}
	intField := func(name string, ptr func(*settings.Settings) *int, min, max int) settingsField {
		return settingsField{
			name:  name,
			str:   func(s *settings.Settings) string { return strconv.Itoa(*ptr(s)) },
			value: func(s *settings.Settings) any { return *ptr(s) },
			set: func(s *settings.Settings, raw string) error {
				n, err := strconv.Atoi(strings.TrimSpace(raw))
				if err != nil {
					return usageError("%s 需要整数", name)
				}
				if n < min || n > max {
					return usageError("%s 必须在 %d..%d 之间", name, min, max)
				}
				*ptr(s) = n
				return nil
			},
		}
	}
	enumField := func(name string, ptr func(*settings.Settings) *string, allowed ...string) settingsField {
		f := strField(name, ptr)
		f.set = func(s *settings.Settings, raw string) error {
			for _, a := range allowed {
				if raw == a {
					*ptr(s) = raw
					return nil
				}
			}
			return usageError("%s 只能是 %s", name, strings.Join(allowed, " / "))
		}
		return f
	}

	return []settingsField{
		enumField("coreMode", func(s *settings.Settings) *string { return &s.CoreMode }, settings.CoreModeManaged, settings.CoreModeAttach),
		strField("corePath", func(s *settings.Settings) *string { return &s.CorePath }),
		strField("controllerAddr", func(s *settings.Settings) *string { return &s.ControllerAddr }),
		strField("secret", func(s *settings.Settings) *string { return &s.Secret }),
		intField("mixedPort", func(s *settings.Settings) *int { return &s.MixedPort }, 1, 65535),
		boolField("allowLan", func(s *settings.Settings) *bool { return &s.AllowLan }),
		enumField("mode", func(s *settings.Settings) *string { return &s.Mode }, "rule", "global", "direct"),
		enumField("logLevel", func(s *settings.Settings) *string { return &s.LogLevel }, "debug", "info", "warning", "error", "silent"),
		boolField("ipv6", func(s *settings.Settings) *bool { return &s.IPv6 }),
		boolField("unifiedDelay", func(s *settings.Settings) *bool { return &s.UnifiedDelay }),
		boolField("tcpConcurrent", func(s *settings.Settings) *bool { return &s.TCPConcurrent }),
		boolField("tun", func(s *settings.Settings) *bool { return &s.Tun }),
		enumField("tunStack", func(s *settings.Settings) *string { return &s.TunStack }, "gvisor", "system", "mixed"),
		strField("testUrl", func(s *settings.Settings) *string { return &s.TestURL }),
		intField("delayTimeout", func(s *settings.Settings) *int { return &s.DelayTimeout }, 500, 15000),
		boolField("autoUpdate", func(s *settings.Settings) *bool { return &s.AutoUpdate }),
		intField("autoUpdateInterval", func(s *settings.Settings) *int { return &s.AutoUpdateInterval }, 5, 60*24*30),
		boolField("proxyFallback", func(s *settings.Settings) *bool { return &s.ProxyFallback }),
		boolField("eventPopup", func(s *settings.Settings) *bool { return &s.EventPopup }),
		boolField("systemProxy", func(s *settings.Settings) *bool { return &s.SystemProxy }),
		strField("systemProxyService", func(s *settings.Settings) *string { return &s.SystemProxyService }),
		boolField("keepCoreOnExit", func(s *settings.Settings) *bool { return &s.KeepCoreOnExit }),
	}
}

func findField(name string) (settingsField, bool) {
	for _, f := range settingsFields() {
		if f.name == name {
			return f, true
		}
	}
	return settingsField{}, false
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on", "开", "开启":
		return true, nil
	case "0", "false", "no", "off", "关", "关闭":
		return false, nil
	}
	return false, usageError("需要布尔值（true/false/on/off/1/0）")
}

// ---------- sysproxy ----------

var sysproxyCmd = command{
	name:    "sysproxy",
	summary: "系统代理开关（on/off/status）",
	usage: `flclash-tui sysproxy status
flclash-tui sysproxy on [--service Wi-Fi]
flclash-tui sysproxy off`,
	sub: []command{
		{name: "status", summary: "查询系统代理状态"},
		{name: "on", summary: "开启系统代理"},
		{name: "off", summary: "关闭系统代理"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("sysproxy"))
			return ExitUsage
		}
		pa, err := argSpec{short: map[string]string{"s": "service"}}.parse(args[1:])
		if err != nil {
			return c.fail(err)
		}
		if svc := pa.get("service", ""); svc != "" {
			c.Settings.SystemProxyService = svc
		}
		switch args[0] {
		case "status":
			on := osshare.CurrentStatus(c.Settings)
			c.emit(map[string]any{
				"ok": true, "enabled": on, "supported": osshare.Supported(),
				"service": c.Settings.SystemProxyService, "port": c.Settings.MixedPort,
			}, func() {
				if !osshare.Supported() {
					c.say("当前平台不支持系统代理设置")
					return
				}
				c.say("系统代理: %s（服务 %s，端口 %d）", boolLabel(on), c.Settings.SystemProxyService, c.Settings.MixedPort)
			})
			if !osshare.Supported() {
				return ExitError
			}
			return ExitOK
		case "on", "off":
			target := args[0] == "on"
			var err error
			if target {
				err = osshare.Enable(c.Settings)
			} else {
				err = osshare.Disable(c.Settings)
			}
			if err != nil {
				return c.fail(err)
			}
			c.Settings.SystemProxy = target
			if err := c.Settings.Save(); err != nil {
				return c.fail(err)
			}
			c.emit(map[string]any{"ok": true, "enabled": target}, func() {
				c.say("系统代理已%s", map[bool]string{true: "开启", false: "关闭"}[target])
			})
			return ExitOK
		}
		fmt.Fprintf(c.Err, "未知的 sysproxy 子命令: %s\n", args[0])
		return ExitUsage
	},
}

// ---------- version ----------

func runVersion(c *Ctx) int {
	info := coreAssetInfo()
	payload := map[string]any{
		"ok":           true,
		"app":          appVersion,
		"coreEmbedded": info.Available,
		"coreVersion":  info.Version,
		"platform":     info.Platform,
		"goRuntime":    info.GoVersion,
		"home":         paths.Home(),
	}
	if v, err := engine.Client(c.Settings).Version(context.Background()); err == nil {
		payload["coreRunning"] = v.Version
	}
	c.emit(payload, func() {
		c.say("flclash-tui %s", appVersion)
		c.say("平台: %s（%s）", info.Platform, info.GoVersion)
		if info.Available {
			c.say("内置内核: mihomo %s", info.Version)
		} else {
			c.say("内置内核: 无")
		}
		if v, ok := payload["coreRunning"]; ok {
			c.say("运行中内核: %s", v)
		}
		c.say("数据目录: %s", paths.Home())
	})
	return ExitOK
}
