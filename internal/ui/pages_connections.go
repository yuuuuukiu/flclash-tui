package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"flclash-tui/internal/clashapi"
)

type sortMode int

const (
	sortByTime sortMode = iota
	sortByTraffic
)

type connRow struct {
	c      clashapi.Connection
	upRate int64
	dnRate int64
}

func newConnectionsPage() *connectionsPage {
	ti := textinput.New()
	ti.Width = 40
	ti.Placeholder = "主机 / 进程 / 规则"
	return &connectionsPage{filter: ti}
}

func (p *connectionsPage) IsInputActive() bool { return p.filtering }

type connectionsPage struct {
	list      ListView
	filter    textinput.Model
	filtering bool
	query     string
	rows      []connRow
	sort      sortMode
	prev      map[string]clashapi.Connection
	lastTick  time.Time
}

func (p *connectionsPage) Tab() string { return "连接" }
func (p *connectionsPage) Shortcuts() string {
	if p.filtering {
		return "输入关键词过滤 · enter 确认 · esc 取消"
	}
	return "[/]过滤 [x]关闭连接 [X]关闭全部 [s]切换排序  j/k移动 · 自动刷新"
}
func (p *connectionsPage) OnEnter(a *App) tea.Cmd { return nil }

func (p *connectionsPage) Update(a *App, msg tea.Msg) tea.Cmd {
	if p.filtering {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "esc":
				p.filtering = false
				p.query = ""
				p.filter.SetValue("")
				return nil
			case "enter":
				p.filtering = false
				p.query = strings.TrimSpace(p.filter.Value())
				return nil
			}
		}
		var cmd tea.Cmd
		p.filter, cmd = p.filter.Update(msg)
		return cmd
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		p.list.Down()
	case "k", "up":
		p.list.Up()
	case "g":
		p.list.Top()
	case "G":
		p.list.Bottom()
	case "/":
		p.filtering = true
		p.filter.Focus()
		return nil
	case "s":
		if p.sort == sortByTime {
			p.sort = sortByTraffic
		} else {
			p.sort = sortByTime
		}
	case "x":
		if p.list.Cursor < 0 || p.list.Cursor >= len(p.rows) {
			return nil
		}
		return doCloseConn(a, p.rows[p.list.Cursor].c.ID)
	case "X":
		a.modal = NewConfirmModal("关闭全部连接", fmt.Sprintf("确定关闭全部 %d 个连接吗？", len(p.rows)), func() tea.Cmd {
			return a.closeAllConns()
		})
		return nil
	}
	return nil
}

// applySnapshot 由连接流快照计算速率并重建行。由 app 层每秒调用。
func (p *connectionsPage) applySnapshot(a *App, resp *clashapi.ConnectionsResp) {
	if resp == nil {
		return
	}
	now := time.Now()
	dt := 1.0
	if !p.lastTick.IsZero() {
		dt = now.Sub(p.lastTick).Seconds()
		if dt < 0.2 {
			dt = 0.2
		}
	}
	p.lastTick = now

	prev := p.prev
	p.prev = map[string]clashapi.Connection{}
	rows := make([]connRow, 0, len(resp.Connections))
	for _, c := range resp.Connections {
		p.prev[c.ID] = c
		var du, dd int64
		if old, ok := prev[c.ID]; ok {
			du = int64(float64(c.Upload-old.Upload) / dt)
			dd = int64(float64(c.Download-old.Download) / dt)
		}
		rows = append(rows, connRow{c: c, upRate: du, dnRate: dd})
	}
	if p.sort == sortByTraffic {
		sort.SliceStable(rows, func(i, j int) bool {
			si := rows[i].c.Upload + rows[i].c.Download
			sj := rows[j].c.Upload + rows[j].c.Download
			if si != sj {
				return si > sj
			}
			return rows[i].c.Start.Before(rows[j].c.Start)
		})
	} else {
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i].c.Start.After(rows[j].c.Start)
		})
	}
	// 过滤
	if p.query != "" {
		q := strings.ToLower(p.query)
		filtered := rows[:0]
		for _, r := range rows {
			if strings.Contains(strings.ToLower(connHost(r.c)), q) ||
				strings.Contains(strings.ToLower(r.c.Metadata.Process), q) ||
				strings.Contains(strings.ToLower(r.c.Rule+r.c.RulePayload), q) ||
				strings.Contains(strings.ToLower(strings.Join(r.c.Chains, ">")), q) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	// 保持光标
	oldID := ""
	if p.list.Cursor < len(p.rows) && p.list.Cursor >= 0 {
		oldID = p.rows[p.list.Cursor].c.ID
	}
	p.rows = rows
	p.list.Len = len(rows)
	if oldID != "" {
		for i, r := range rows {
			if r.c.ID == oldID {
				p.list.Cursor = i
				break
			}
		}
	}
	p.list.Clamp()
}

func connHost(c clashapi.Connection) string {
	if c.Metadata.Host != "" {
		return c.Metadata.Host
	}
	return c.Metadata.DestinationIP
}

func (p *connectionsPage) View(a *App) string {
	w := a.width - 4
	if w < 60 {
		w = 60
	}
	h := a.height - 7
	if h < 3 {
		h = 3
	}
	p.list.Height = h - 2

	// 头部统计
	head := fmt.Sprintf(" 连接 (%d)", len(p.rows))
	if a.Conns != nil {
		head += styleMuted.Render(fmt.Sprintf("  ↑%s ↓%s · 内存 %s",
			FormatBytes(a.Conns.UploadTotal), FormatBytes(a.Conns.DownloadTotal), FormatBytes(a.Memory.Inuse)))
	}
	if p.query != "" {
		head += styleYellow.Render(fmt.Sprintf("  过滤: %s", p.query))
	}
	if len(p.rows) == 0 {
		return styleCard.Width(w).Height(h).Render(head + "\n" + styleMuted.Render(" 暂无连接（产生流量后出现）"))
	}

	// 列宽
	colHost := w / 3
	colRule := w / 4
	colChain := w / 5
	colSpeed := 22
	restW := w - colHost - colRule - colChain - colSpeed - 8
	if restW < 6 {
		restW = 6
	}

	var b strings.Builder
	b.WriteString(styleMuted.Render(
		PadCJK("  主机", colHost)+PadCJK("规则", colRule)+PadCJK("链路", colChain)+
			PadCJK("↑速度 ↓流量/↑流量", colSpeed)+PadCJK("进程", restW)) + "\n")

	from, to := p.list.Window()
	for i := from; i < to; i++ {
		r := p.rows[i]
		c := r.c
		cur := "  "
		if i == p.list.Cursor {
			cur = "▶ "
		}
		host := connHost(c) + ":" + c.Metadata.DestinationPort
		if c.Metadata.Network != "" {
			host = strings.ToUpper(c.Metadata.Network[:1]) + " " + host
		}
		rule := c.Rule
		if c.RulePayload != "" {
			rule += "(" + c.RulePayload + ")"
		}
		chain := ""
		if len(c.Chains) > 0 {
			// chains 是从目标到入口的顺序，显示为 入口→…→出口
			rev := make([]string, len(c.Chains))
			for i2, s := range c.Chains {
				rev[len(c.Chains)-1-i2] = s
			}
			chain = strings.Join(rev, "→")
		}
		speed := stylePurple.Render(FormatSpeed(r.upRate)) + " " + styleAccent.Render(FormatSpeed(r.dnRate))
		totals := styleMuted.Render(fmt.Sprintf("↑%s ↓%s", FormatBytes(c.Upload), FormatBytes(c.Download)))
		line := PadCJK(cur+TruncateCJK(host, colHost-2), colHost) +
			PadCJK(TruncateCJK(rule, colRule), colRule) +
			PadCJK(TruncateCJK(chain, colChain), colChain) +
			" " + speed + "  " + totals
		if c.Metadata.Process != "" {
			line += styleMuted.Render(" " + TruncateCJK(c.Metadata.Process, restW))
		}
		if i == p.list.Cursor {
			line = styleSelItem.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return styleCard.Width(w).Height(h).Render(head + "\n" + strings.TrimSuffix(b.String(), "\n"))
}
