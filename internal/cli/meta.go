package cli

import (
	"runtime"
	"time"

	"flclash-tui/internal/coreasset"
	"flclash-tui/internal/engine"
)

// appVersion 是应用版本号（与内核版本解耦）。
const appVersion = "1.20.0"

// coreInfo 是给 version 子命令用的内置内核信息。
type coreInfo struct {
	Available bool
	Version   string
	Platform  string
	GoVersion string
}

func coreAssetInfo() coreInfo {
	info := coreasset.InfoOf()
	return coreInfo{
		Available: info.Available,
		Version:   info.Version,
		Platform:  info.Platform,
		GoVersion: runtime.Version(),
	}
}

// restartManaged 让 managed 模式的内核重新加载（配置已由调用方写好）。
//
//   - 后台（daemon）内核：重启该进程，新配置才能真正生效；
//   - TUI 托管 / 外部内核：只能热重载，不抢别人的进程；
//   - 未运行：什么都不做（配置已落盘，下次启动生效）。
func restartManaged(c *Ctx) int {
	if pid, err := readPID(); err == nil && pid > 0 && processAlive(pid) {
		// 重启自己拉起的后台内核：先静默停掉，再在新的配置上启动
		silent := &Ctx{Settings: c.Settings, Profiles: c.Profiles, JSON: true, Quiet: true, Out: c.Out, Err: c.Err}
		if code := stopDaemon(silent, pid); code != ExitOK {
			return code
		}
		if code := startDaemonSilent(c); code != ExitOK {
			return code
		}
		if !c.JSON {
			c.say("内核已重启（新配置已生效）")
		}
		return ExitOK
	}
	if engine.Reachable(c.Settings, 1500*time.Millisecond) {
		if err := engine.HotReload(c.Settings, c.Profiles); err != nil {
			return c.fail(err)
		}
		if !c.JSON {
			c.say("（内核不是由 CLI 后台启动的，已改为热重载）")
		}
		return ExitOK
	}
	if !c.JSON {
		c.say("（内核未运行，配置已写入，下次 core start 生效）")
	}
	return ExitOK
}
