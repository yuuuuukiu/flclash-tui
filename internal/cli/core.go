package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"flclash-tui/internal/coreasset"
	"flclash-tui/internal/coremgr"
	"flclash-tui/internal/engine"
	"flclash-tui/internal/paths"
	"flclash-tui/internal/settings"
)

var coreCmd = command{
	name:    "core",
	summary: "内核控制（start/stop/restart/status/logs/path）",
	usage: `flclash-tui core start [--daemon] [--wait 10s]
flclash-tui core stop
flclash-tui core restart [--daemon]
flclash-tui core status
flclash-tui core logs [-n 100] [-f]
flclash-tui core path [--extract]
flclash-tui core upgrade`,
	sub: []command{
		{name: "start", summary: "启动内核（--daemon 后台运行）"},
		{name: "stop", summary: "停止内核"},
		{name: "restart", summary: "重启内核"},
		{name: "status", summary: "查看内核状态"},
		{name: "logs", summary: "查看内核日志"},
		{name: "path", summary: "显示内置/使用的内核路径"},
		{name: "upgrade", summary: "在线升级内核（mihomo 自带）"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("core"))
			return ExitUsage
		}
		switch args[0] {
		case "start":
			return coreStart(c, args[1:])
		case "stop":
			return coreStop(c, args[1:])
		case "restart":
			return coreRestart(c, args[1:])
		case "status":
			return coreStatus(c, args[1:])
		case "logs":
			return coreLogs(c, args[1:])
		case "path":
			return corePath(c, args[1:])
		case "upgrade":
			return coreUpgrade(c, args[1:])
		case "_serve":
			return coreServe(c, args[1:])
		}
		fmt.Fprintf(c.Err, "未知的 core 子命令: %s\n", args[0])
		return ExitUsage
	},
}

// ---------- start / stop / restart ----------

func coreStart(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"daemon": false}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	if c.Settings.CoreMode != settings.CoreModeManaged {
		return c.fail(fmt.Errorf("当前为 attach 模式，内核由外部管理；如需托管请先设置 coreMode=managed"))
	}
	if _, err := engine.EnsureCore(c.Settings); err != nil {
		return c.fail(err)
	}
	if c.Settings.CorePath == "" {
		return c.fail(fmt.Errorf("未找到 mihomo 内核（当前平台未内置，且未配置 corePath）"))
	}
	if coremgr.PortInUse(c.Settings.ControllerAddr) {
		return c.fail(fmt.Errorf("%s 已被占用（可能已有内核在运行）；如需接管请先 core stop", c.Settings.ControllerAddr))
	}

	if pa.boolOf("daemon") {
		if err := engine.WriteCoreConfig(c.Settings, c.Profiles); err != nil {
			return c.fail(err)
		}
		return startDaemon(c)
	}

	m := coremgr.New()
	if err := engine.StartCore(c.Settings, m); err != nil {
		return c.fail(err)
	}
	// 前台运行：等信号后退出
	if !c.JSON {
		c.say("内核已启动 pid=%d 控制器=%s", m.PID(), c.Settings.ControllerAddr)
	}
	c.emit(map[string]any{"ok": true, "pid": m.PID(), "controller": c.Settings.ControllerAddr}, func() {
		c.say("按 Ctrl-C 停止内核…")
	})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	m.Stop()
	c.say("内核已停止")
	return ExitOK
}

// startDaemon 以子进程方式启动内核并写 pid 文件。
func startDaemon(c *Ctx) int {
	return startDaemonWith(c, true)
}

// startDaemonSilent 同 startDaemon，但不打印启动提示（供重启流程复用）。
func startDaemonSilent(c *Ctx) int {
	return startDaemonWith(c, false)
}

func startDaemonWith(c *Ctx, announce bool) int {
	exe, err := os.Executable()
	if err != nil {
		return c.fail(err)
	}
	logFile, err := os.OpenFile(paths.CoreLogFile(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return c.fail(err)
	}
	cmd := exec.Command(exe, "core", "_serve")
	cmd.Env = append(os.Environ(), "FLCLASH_TUI_HOME="+paths.Home())
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = daemonSysProcAttr()
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return c.fail(err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	logFile.Close()
	_ = os.WriteFile(paths.CorePIDFile(), []byte(strconv.Itoa(pid)), 0o644)

	// 等控制器就绪（最多 15s）
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if engine.Reachable(c.Settings, 800*time.Millisecond) {
			c.emit(map[string]any{"ok": true, "pid": pid, "controller": c.Settings.ControllerAddr}, func() {
				if announce {
					c.say("内核已后台启动 pid=%d 控制器=%s", pid, c.Settings.ControllerAddr)
				}
			})
			return ExitOK
		}
		if !processAlive(pid) {
			os.Remove(paths.CorePIDFile())
			tail := strings.Join(readTail(paths.CoreLogFile(), 12), "\n")
			return c.fail(fmt.Errorf("内核进程已退出，日志尾部：\n%s", tail))
		}
		time.Sleep(250 * time.Millisecond)
	}
	return c.fail(fmt.Errorf("内核已启动(pid=%d)但控制器未就绪，查看日志: %s", pid, paths.CoreLogFile()))
}

// coreServe 是 daemon 的实际内核进程：exec 内核并透传信号。
func coreServe(c *Ctx, argv []string) int {
	if _, err := engine.EnsureCore(c.Settings); err != nil {
		fmt.Fprintln(c.Err, err.Error())
		return ExitError
	}
	m := coremgr.New()
	if err := engine.StartCore(c.Settings, m); err != nil {
		fmt.Fprintln(c.Err, err.Error())
		return ExitError
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	m.Stop()
	return ExitOK
}

func coreStop(c *Ctx, argv []string) int {
	pid, err := readPID()
	if err != nil || pid <= 0 {
		// 没有 pid 文件：可能是 TUI 托管或 attach 模式
		if engine.Reachable(c.Settings, 800*time.Millisecond) {
			return c.fail(fmt.Errorf("未找到 pid 文件，但控制器仍在响应；该内核不是由 CLI 后台启动的，请用 TUI 管理或 core status 确认"))
		}
		c.emit(map[string]any{"ok": true, "stopped": false}, func() {
			c.say("没有正在运行的内核")
		})
		return ExitOK
	}
	return stopDaemon(c, pid)
}

// stopDaemon 结束指定 pid 的后台内核并清理 pid 文件。
func stopDaemon(c *Ctx, pid int) int {
	if !processAlive(pid) {
		os.Remove(paths.CorePIDFile())
		c.emit(map[string]any{"ok": true, "stopped": false}, func() {
			c.say("没有正在运行的内核（清理了残留 pid 文件）")
		})
		return ExitOK
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return c.fail(err)
	}
	_ = proc.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			os.Remove(paths.CorePIDFile())
			c.emit(map[string]any{"ok": true, "stopped": true, "pid": pid}, func() {
				c.say("内核已停止 pid=%d", pid)
			})
			return ExitOK
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = proc.Kill()
	os.Remove(paths.CorePIDFile())
	c.emit(map[string]any{"ok": true, "stopped": true, "pid": pid, "forced": true}, func() {
		c.say("内核未在 5s 内退出，已强制结束 pid=%d", pid)
	})
	return ExitOK
}

func coreRestart(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	if pid, err := readPID(); err == nil && pid > 0 && processAlive(pid) {
		if code := stopDaemon(c, pid); code != ExitOK {
			return code
		}
	} else if engine.Reachable(c.Settings, 800*time.Millisecond) {
		// 非本工具启动的内核（TUI 托管 / attach）：不能杀，改为热重载
		if err := engine.HotReload(c.Settings, c.Profiles); err != nil && !errors.Is(err, engine.ErrNotRunning) {
			return c.fail(err)
		}
		c.emit(map[string]any{"ok": true, "restarted": false, "reloaded": true}, func() {
			c.say("内核不是由 CLI 后台启动的，已改为热重载配置")
		})
		return ExitOK
	}
	return coreStart(c, []string{"--daemon"})
}

// ---------- status / logs / path ----------

func coreStatus(c *Ctx, argv []string) int {
	type status struct {
		OK         bool   `json:"ok"`
		Running    bool   `json:"running"`
		PID        int    `json:"pid,omitempty"`
		Managed    string `json:"managed"`
		Controller string `json:"controller"`
		Reachable  bool   `json:"reachable"`
		Version    string `json:"version,omitempty"`
		CorePath   string `json:"corePath,omitempty"`
		Uptime     string `json:"uptime,omitempty"`
		Mode       string `json:"mode,omitempty"`
	}
	st := status{Managed: c.Settings.CoreMode, Controller: c.Settings.ControllerAddr, CorePath: c.Settings.CorePath}
	if pid, err := readPID(); err == nil && pid > 0 && processAlive(pid) {
		st.PID = pid
		st.Running = true
	}
	if v, err := engine.Client(c.Settings).Version(context.Background()); err == nil {
		st.Reachable = true
		st.Running = true
		st.Version = v.Version
	}
	if cfg, err := engine.Client(c.Settings).GetConfigs(context.Background()); err == nil {
		st.Mode = cfg.Mode
	}
	if st.Running {
		st.OK = true
	}
	code := ExitOK
	if !st.Running {
		code = ExitUnreachable
	}
	c.emit(st, func() {
		if st.Running {
			c.say("内核: 运行中")
		} else {
			c.say("内核: 已停止")
		}
		if st.PID > 0 {
			c.say("PID: %d", st.PID)
		}
		c.say("模式: %s", st.Managed)
		c.say("控制器: %s（%s）", st.Controller, reachLabel(st.Reachable))
		if st.Version != "" {
			c.say("版本: %s", st.Version)
		}
		if st.Mode != "" {
			c.say("代理模式: %s", st.Mode)
		}
		if st.CorePath != "" {
			c.say("内核路径: %s", st.CorePath)
		}
	})
	return code
}

func reachLabel(ok bool) string {
	if ok {
		return "可连接"
	}
	return "不可达"
}

func coreLogs(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"follow": false},
		alias: map[string]string{"f": "follow", "n": "lines"}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	n, err := pa.parseInt("lines", 100)
	if err != nil {
		return c.fail(err)
	}
	path := paths.CoreLogFile()
	if pa.boolOf("follow") {
		return followFile(c, path, n)
	}
	lines, err := readTailErr(path, n)
	if err != nil {
		return c.fail(err)
	}
	if c.JSON {
		c.emit(map[string]any{"ok": true, "path": path, "lines": lines}, func() {})
		return ExitOK
	}
	for _, l := range lines {
		fmt.Fprintln(c.Out, l)
	}
	return ExitOK
}

func corePath(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"extract": false}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	info := coreasset.InfoOf()
	res := map[string]any{
		"ok":        true,
		"platform":  info.Platform,
		"embedded":  info.Available,
		"version":   info.Version,
		"sha256":    info.SHA256,
		"extracted": paths.CoreBinFile(),
		"inUse":     c.Settings.CorePath,
	}
	if pa.boolOf("extract") {
		p, err := coreasset.Ensure(paths.CoreBinFile())
		if err != nil {
			return c.fail(err)
		}
		res["inUse"] = p
		c.Settings.CorePath = p
		if err := c.Settings.Save(); err != nil {
			return c.fail(err)
		}
	}
	c.emit(res, func() {
		c.say("平台: %s", info.Platform)
		if info.Available {
			c.say("内置内核: 是（%s）", info.Version)
		} else {
			c.say("内置内核: 否（请把 mihomo 放到 PATH 或在设置里指定路径）")
		}
		c.say("释放路径: %s", paths.CoreBinFile())
		if p, ok := res["inUse"].(string); ok && p != "" {
			c.say("当前使用: %s", p)
		}
	})
	return ExitOK
}

func coreUpgrade(c *Ctx, argv []string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := engine.Client(c.Settings).UpgradeCore(ctx); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true}, func() { c.say("内核已升级，建议重启内核") })
	return ExitOK
}

// ---------- pid / 进程工具 ----------

func readPID() (int, error) {
	data, err := os.ReadFile(paths.CorePIDFile())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// processAlive 判断进程是否存活（信号 0 探测）。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
