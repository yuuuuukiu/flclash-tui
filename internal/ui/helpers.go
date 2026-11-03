package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"flclash-tui/internal/clashapi"
)

// ---------- 小工具 ----------

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// bgJob 运行一个自带 20s 超时上下文的后台任务。
func bgJob(f func(ctx context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := ctxTimeout(20 * time.Second)
		defer cancel()
		return f(ctx)
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---------- 流监听命令 ----------

func listenTraffic(ch <-chan clashapi.TrafficMsg) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return streamClosedMsg{"traffic"}
		}
		return v
	}
}

func listenMemory(ch <-chan clashapi.MemoryMsg) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return streamClosedMsg{"memory"}
		}
		return v
	}
}

func listenLogs(ch <-chan clashapi.LogMsg) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return streamClosedMsg{"logs"}
		}
		return v
	}
}

func listenConns(ch <-chan clashapi.ConnectionsResp) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return streamClosedMsg{"connections"}
		}
		return v
	}
}

// listenCoreExit 监听内核进程退出（每次 Start 后调用）。
func (a *App) listenCoreExit() tea.Cmd {
	ch := a.Manager.ExitedCh()
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		<-ch
		return coreExitMsg{err: nil, code: a.Manager.ExitCode()}
	}
}

// ShowHelp 打开帮助浮层。
func (a *App) ShowHelp() tea.Cmd {
	a.modal = &HelpModalWithPage{PageName: a.CurrentPage().Tab()}
	return nil
}

// ---------- 订阅自动更新 ----------

type autoUpdateTickMsg struct{}

func autoUpdateTickerCmd() tea.Cmd {
	return tea.Tick(10*time.Minute, func(time.Time) tea.Msg { return autoUpdateTickMsg{} })
}

// checkAutoUpdates 检查到期订阅并逐个更新（结果经 subUpdatedMsg 汇入主循环）。
func (a *App) checkAutoUpdates() tea.Cmd {
	if !a.Settings.AutoUpdate {
		return nil
	}
	due := a.Profiles.DueUpdate(a.Settings.AutoUpdateInterval)
	if len(due) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, p := range due {
		id := p.ID
		a.BusyStart()
		cmds = append(cmds, func() tea.Msg {
			defer a.BusyEnd()
			ctx, cancel := ctxTimeout(60 * time.Second)
			defer cancel()
			if _, err := a.Profiles.Update(ctx, id); err != nil {
				return resultMsg{job: "自动更新订阅", err: err}
			}
			return subUpdatedMsg{id: id}
		})
	}
	return tea.Batch(cmds...)
}
