package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"flclash-tui/internal/cli"
	"flclash-tui/internal/coreasset"
	"flclash-tui/internal/coremgr"
	"flclash-tui/internal/engine"
	"flclash-tui/internal/paths"
	"flclash-tui/internal/profiles"
	"flclash-tui/internal/settings"
	"flclash-tui/internal/ui"
)

func main() {
	// 有参数 = 非交互式调用；无参数 = 启动 TUI
	if len(os.Args) > 1 {
		os.Exit(cli.Run(os.Args[1:]))
	}
	runTUI()
}

func runTUI() {
	if err := paths.EnsureHome(); err != nil {
		fmt.Fprintln(os.Stderr, "初始化目录失败:", err)
		os.Exit(1)
	}
	s, err := settings.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取设置失败:", err)
		os.Exit(1)
	}
	// 首次运行：优先释放内置内核，其次在常见路径寻找 mihomo
	if s.CoreMode == settings.CoreModeManaged && s.CorePath == "" {
		if p, err := engine.EnsureCore(s); err == nil && p != "" {
			s.CorePath = p
			_ = s.Save()
		} else if p := findCore(); p != "" {
			s.CorePath = p
			_ = s.Save()
		}
	}
	ps := profiles.NewStore()
	m := coremgr.New()

	app := ui.NewApp(s, ps, m)

	if s.CoreMode == settings.CoreModeManaged {
		if s.CorePath == "" {
			fmt.Fprintln(os.Stderr, "未找到 mihomo 内核。请：\n"+
				"  1) 从 https://github.com/MetaCubeX/mihomo/releases 下载对应平台二进制\n"+
				"  2) 启动 TUI 后进入 [7]设置 设置内核路径")
			fmt.Fprintln(os.Stderr, "（已生成配置目录: "+paths.Home()+"）")
		} else {
			if err := app.WriteCoreConfig(); err != nil {
				fmt.Fprintln(os.Stderr, "生成配置失败:", err)
			}
			if coremgr.PortInUse(s.ControllerAddr) {
				fmt.Fprintf(os.Stderr, "警告: %s 已被占用（可能有内核在运行）\n", s.ControllerAddr)
			}
		}
	}

	p := tea.NewProgram(app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI 运行出错:", err)
	}
	// 退出清理：默认停止托管的内核，避免孤儿进程
	if s.CoreMode == settings.CoreModeManaged && !s.KeepCoreOnExit {
		m.Stop()
	}
}

// findCore 在常见位置寻找 mihomo 内核（内置内核不可用时的兜底）。
func findCore() string {
	candidates := []string{
		paths.CoreBinFile(), // 内置内核释放位置
		"/opt/homebrew/bin/mihomo",
		"/usr/local/bin/mihomo",
		"/usr/bin/mihomo",
		filepath.Join(paths.Home(), "mihomo"),
		"mihomo", // PATH
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

var _ = coreasset.Version
