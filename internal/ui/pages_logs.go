package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"flclash-tui/internal/clashapi"
)

type logsPage struct {
	vp        viewport.Model
	filtering bool
	query     string
	filter    textinput.Model
	paused    bool
	follow    bool
	level     int // 0=info 1=warning 2=error
	lines     []string
	dirty     bool
}

var levelNames = []string{"info", "warning", "error"}

func newLogsPage() *logsPage {
	ti := textinput.New()
	ti.Width = 40
	ti.Placeholder = "输入关键词过滤"
	return &logsPage{follow: true, filter: ti}
}

func (p *logsPage) Tab() string { return "日志" }
func (p *logsPage) Shortcuts() string {
	if p.filtering {
		return "输入关键词过滤 · enter 确认 · esc 取消"
	}
	s := "[/]过滤 [l]级别:" + levelNames[p.level] + " [c]清空 [space]暂停/继续 [f]跟随:" + followLabel(p.follow) + "  j/k/PgUp/PgDn滚动"
	return s
}
func (p *logsPage) OnEnter(a *App) tea.Cmd { return nil }

func followLabel(b bool) string {
	if b {
		return "开"
	}
	return "关"
}

func (p *logsPage) IsInputActive() bool { return p.filtering }

func (p *logsPage) Update(a *App, msg tea.Msg) tea.Cmd {
	if p.filtering {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "esc":
				p.filtering = false
				p.query = ""
				p.filter.SetValue("")
				p.rebuild(a)
				return nil
			case "enter":
				p.filtering = false
				p.query = strings.TrimSpace(p.filter.Value())
				p.rebuild(a)
				return nil
			}
		}
		var cmd tea.Cmd
		p.filter, cmd = p.filter.Update(msg)
		return cmd
	}

	if _, ok := msg.(tickMsg); ok {
		// 把 App 层累积的新日志刷进来
		p.rebuild(a)
		return nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		p.vp.LineDown(1)
		p.follow = false
	case "k", "up":
		p.vp.LineUp(1)
	case "G", "end":
		p.vp.GotoBottom()
		p.follow = true
	case "g", "home":
		p.vp.GotoTop()
	case "pgup":
		p.vp.HalfPageUp()
	case "pgdown":
		p.vp.HalfPageDown()
	case "/":
		p.filtering = true
		p.filter.Focus()
	case "l":
		p.level = (p.level + 1) % len(levelNames)
		p.rebuild(a)
	case "c":
		a.Logs = nil
		p.rebuild(a)
	case " ":
		p.follow = !p.follow
	case "f":
		p.follow = !p.follow
	}
	return nil
}

func (p *logsPage) rebuild(a *App) {
	lines := make([]string, 0, len(a.Logs))
	for _, l := range a.Logs {
		// 级别过滤：0=info 全部显示；1=warning 起；2=仅 error
		switch p.level {
		case 1:
			if l.Type == "info" || l.Type == "debug" {
				continue
			}
		case 2:
			if l.Type != "error" {
				continue
			}
		}
		if p.query != "" && !strings.Contains(strings.ToLower(l.Payload), strings.ToLower(p.query)) {
			continue
		}
		lines = append(lines, formatLog(l))
	}
	p.lines = lines
	content := strings.Join(lines, "\n")
	p.vp.SetContent(content)
	if p.follow {
		p.vp.GotoBottom()
	}
}

func formatLog(l clashapi.LogMsg) string {
	var st lipgloss.Style
	switch l.Type {
	case "warning":
		st = styleYellow
	case "error":
		st = styleRed
	case "debug":
		st = styleMuted
	default:
		st = lipgloss.NewStyle().Foreground(colorFg)
	}
	return st.Render(PadCJK("["+l.Type+"]", 12)) + l.Payload
}

func (p *logsPage) View(a *App) string {
	w := a.width - 4
	if w < 40 {
		w = 40
	}
	h := a.height - 7
	if h < 3 {
		h = 3
	}
	p.vp.Width = w - 2
	p.vp.Height = h - 2
	head := fmt.Sprintf(" 日志 (%d 行)", len(p.lines))
	if p.query != "" {
		head += styleYellow.Render("  过滤: " + p.query)
	}
	if p.follow {
		head += styleGreen.Render("  · 跟随")
	} else {
		head += styleMuted.Render("  · 已暂停跟随")
	}
	var inputLine string
	if p.filtering {
		inputLine = "\n " + p.filter.View()
	}
	return styleCard.Width(w).Height(h).Render(head+"\n"+p.vp.View()) + inputLine
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
