package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"flclash-tui/internal/coremgr"
	"flclash-tui/internal/osshare"
	"flclash-tui/internal/profiles"
	"flclash-tui/internal/settings"
)

type dashboardPage struct {
	needReload bool
}

func (d *dashboardPage) Tab() string { return "仪表盘" }
func (d *dashboardPage) Shortcuts() string {
	return "[m]切换模式 [r]重启内核 [c]关闭全部连接 [f]清空FakeIP  全局: 1-7换页 · ?帮助 · Q退出"
}
func (d *dashboardPage) OnEnter(a *App) tea.Cmd { return a.fetchConfigs() }

func (d *dashboardPage) Update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			if a.Settings.CoreMode == settings.CoreModeManaged {
				return a.ApplyAndRestart()
			}
			return a.ShowToast("attach 模式下无法控制内核进程", toastInfo)
		case "c":
			return a.closeAllConns()
		case "f":
			a.BusyStart()
			return func() tea.Msg {
				defer a.BusyEnd()
				ctx, cancel := ctxTimeout(5 * time.Second)
				defer cancel()
				if err := a.API.FlushFakeIP(ctx); err != nil {
					return resultMsg{job: "清空FakeIP", err: err}
				}
				return resultMsg{job: "清空FakeIP", text: "FakeIP 缓存已清空"}
			}
		}
	}
	return nil
}

func (a *App) closeAllConns() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := ctxTimeout(5 * time.Second)
		defer cancel()
		if err := a.API.CloseAllConnections(ctx); err != nil {
			return resultMsg{job: "关闭连接", err: err}
		}
		return resultMsg{job: "关闭连接", text: "已关闭全部连接"}
	}
}

func (d *dashboardPage) View(a *App) string {
	w := a.width - 4
	if w < 40 {
		w = 40
	}

	// ---- 状态卡 ----
	stateTxt := coreStateLabel(a)
	stateStyle := styleGreen
	if a.Manager.State() == coremgr.StateStarting {
		stateStyle = styleYellow
	} else if a.Manager.State() == coremgr.StateStopped {
		stateStyle = styleRed
	}
	uptime := "—"
	if a.Manager.State() == coremgr.StateRunning {
		uptime = HumanDuration(a.Manager.Uptime())
	}
	ver := a.Version
	if ver == "" {
		ver = "—"
	}
	status := lipgloss.JoinHorizontal(lipgloss.Top,
		card(w/2-1, "状态",
			kv("内核", stateStyle.Render(stateTxt)),
			kv("版本", styleValue.Render(ver)),
			kv("运行时长", styleValue.Render(uptime)),
			kv("控制器", styleValue.Render(a.Settings.ControllerAddr)),
		),
		card(w/2-1, "代理",
			kv("模式", styleAccent.Render(modeLabel(a.Mode))),
			kv("混合端口", styleValue.Render(fmt.Sprint(a.Settings.MixedPort))),
			kv("系统代理", sysProxyLabel(a)),
			kv("TUN", boolLabel(a.Settings.Tun)),
		),
	)

	// ---- 流量卡 ----
	downHist := make([]int64, 0, len(a.TrafficHis))
	upHist := make([]int64, 0, len(a.TrafficHis))
	for _, t := range a.TrafficHis {
		downHist = append(downHist, t.Down)
		upHist = append(upHist, t.Up)
	}
	chartW := w - 2
	downLine := Sparkline(downHist, chartW, 0, lipgloss.Color("39"))
	upLine := Sparkline(upHist, chartW, 0, lipgloss.Color("141"))

	dl, ul := "0 B/s", "0 B/s"
	var totalUp, totalDown int64
	if a.Conns != nil {
		totalUp, totalDown = a.Conns.UploadTotal, a.Conns.DownloadTotal
	}
	if a.Manager.State() == coremgr.StateRunning || a.Conns != nil {
		dl = FormatSpeed(a.Traffic.Down)
		ul = FormatSpeed(a.Traffic.Up)
	}

	mem := "—"
	if a.Memory.Inuse > 0 {
		mem = FormatBytes(a.Memory.Inuse)
	}
	trafficCard := card(w, "实时流量",
		lipgloss.JoinHorizontal(lipgloss.Top,
			styleAccent.Render(" ↓ ")+styleValue.Render(PadCJK(dl, 16))+"\n"+
				stylePurple.Render(" ↑ ")+styleValue.Render(ul),
			lipgloss.JoinVertical(lipgloss.Left,
				styleMuted.Render("内存: "+mem),
				styleMuted.Render("连接: "+fmt.Sprint(connCount(a))),
			),
		),
		"↓ "+downLine,
		"↑ "+upLine,
		styleMuted.Render(fmt.Sprintf("本次运行累计 ↑%s ↓%s", FormatBytes(totalUp), FormatBytes(totalDown))),
	)

	// ---- 订阅卡 ----
	var subLines []string
	if p, ok := a.Profiles.Active(); ok {
		subLines = append(subLines, kv("当前订阅", styleValue.Render(p.Label)))
		if p.SubInfo != nil && p.SubInfo.Total > 0 {
			used := p.SubInfo.Upload + p.SubInfo.Download
			subLines = append(subLines,
				kv("流量", fmt.Sprintf("%s / %s", FormatBytes(used), FormatBytes(p.SubInfo.Total))),
				"   "+ProgressBar(used, p.SubInfo.Total, w/2),
			)
		}
		if p.SubInfo != nil && p.SubInfo.Expire > 0 {
			subLines = append(subLines, kv("到期", styleValue.Render(profiles.FormatExpire(p.SubInfo.Expire))))
		}
	} else {
		subLines = append(subLines, styleMuted.Render("尚未添加订阅，按 [3] 进入订阅页添加"))
	}
	subCard := card(w, "订阅", subLines...)

	return lipgloss.JoinVertical(lipgloss.Left, status, trafficCard, subCard)
}

func card(width int, title string, lines ...string) string {
	inner := []string{styleCardTitle.Render(title), ""}
	inner = append(inner, lines...)
	content := lipgloss.NewStyle().Width(width - 2).Render(strings.Join(inner, "\n"))
	return styleCard.Width(width).Render(content)
}

func kv(k, v string) string {
	return styleMuted.Render(PadCJK(" "+k, 14)) + v
}

func boolLabel(b bool) string {
	if b {
		return styleGreen.Render("开启")
	}
	return styleMuted.Render("关闭")
}

func sysProxyLabel(a *App) string {
	if a.SystemProxy {
		return styleGreen.Render("开启")
	}
	if !osshare.Supported() {
		return styleMuted.Render("不支持")
	}
	return styleMuted.Render("关闭")
}

func connCount(a *App) int {
	if a.Conns == nil {
		return 0
	}
	return len(a.Conns.Connections)
}

// ---------- 供其他页面使用的通用动作 ----------

func doCloseConn(a *App, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := ctxTimeout(5 * time.Second)
		defer cancel()
		if err := a.API.CloseConnection(ctx, id); err != nil {
			return resultMsg{job: "关闭连接", err: err}
		}
		return resultMsg{job: "closeconn", text: ""}
	}
}
