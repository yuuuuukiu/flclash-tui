package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"flclash-tui/internal/clashapi"
)

type rulesPage struct {
	list      ListView
	all       []clashapi.Rule
	rows      []clashapi.Rule
	filter    textinput.Model
	filtering bool
	query     string
	loaded    bool
}

func newRulesPage() *rulesPage {
	ti := textinput.New()
	ti.Width = 40
	ti.Placeholder = "输入关键词过滤"
	return &rulesPage{filter: ti}
}

func (p *rulesPage) Tab() string { return "规则" }
func (p *rulesPage) Shortcuts() string {
	if p.filtering {
		return "输入关键词过滤 · enter 确认 · esc 取消"
	}
	return "[/]过滤 [r]刷新  j/k移动 · g/G首尾"
}
func (p *rulesPage) OnEnter(a *App) tea.Cmd {
	// 每次进入都重新拉取（内核重载后规则可能已变化）
	return a.fetchRules()
}

func (p *rulesPage) IsInputActive() bool { return p.filtering }

func (p *rulesPage) applyRules(resp *clashapi.RulesResp) tea.Cmd {
	if resp != nil {
		p.all = resp.Rules
		p.loaded = true
		p.refilter()
	}
	return nil
}

func (p *rulesPage) Update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case rulesMsg:
		if m.resp != nil {
			p.all = m.resp.Rules
			p.loaded = true
			p.refilter()
		}
		return nil
	}
	if p.filtering {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "esc":
				p.filtering = false
				p.query = ""
				p.filter.SetValue("")
				p.refilter()
				return nil
			case "enter":
				p.filtering = false
				p.query = strings.TrimSpace(p.filter.Value())
				p.refilter()
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
	case "r":
		return a.fetchRules()
	}
	return nil
}

func (p *rulesPage) refilter() {
	p.rows = p.all
	if p.query != "" {
		q := strings.ToLower(p.query)
		out := make([]clashapi.Rule, 0, len(p.all))
		for _, r := range p.all {
			if strings.Contains(strings.ToLower(r.Payload), q) ||
				strings.Contains(strings.ToLower(r.Type), q) ||
				strings.Contains(strings.ToLower(r.Proxy), q) {
				out = append(out, r)
			}
		}
		p.rows = out
	}
	p.list.Len = len(p.rows)
	p.list.Clamp()
}

func (p *rulesPage) View(a *App) string {
	w := a.width - 4
	if w < 40 {
		w = 40
	}
	h := a.height - 7
	if h < 3 {
		h = 3
	}
	p.list.Height = h - 2
	head := fmt.Sprintf(" 规则 (%d)", len(p.rows))
	if p.loaded && len(p.all) > 0 && len(p.rows) != len(p.all) {
		head += styleMuted.Render(fmt.Sprintf(" / 共 %d 条", len(p.all)))
	}
	if p.query != "" {
		head += styleYellow.Render("  过滤: " + p.query)
	}
	if len(p.rows) == 0 {
		if !p.loaded {
			return styleCard.Width(w).Height(h).Render(head + "\n" + styleMuted.Render(" 加载中… (r 手动刷新)"))
		}
		return styleCard.Width(w).Height(h).Render(head + "\n" + styleMuted.Render(" 无规则"))
	}

	colType := 16
	colProxy := w / 5
	colPayload := w - colType - colProxy - 6

	var b strings.Builder
	b.WriteString(styleMuted.Render(
		PadCJK("  类型", colType)+PadCJK("匹配内容", colPayload)+PadCJK("出口", colProxy)) + "\n")
	from, to := p.list.Window()
	for i := from; i < to; i++ {
		r := p.rows[i]
		cur := "  "
		if i == p.list.Cursor {
			cur = "▶ "
		}
		line := PadCJK(cur+TruncateCJK(r.Type, colType-2), colType) +
			PadCJK(TruncateCJK(r.Payload, colPayload), colPayload) +
			styleAccent.Render(TruncateCJK(r.Proxy, colProxy))
		if i == p.list.Cursor {
			line = styleSelItem.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return styleCard.Width(w).Height(h).Render(head + "\n" + strings.TrimSuffix(b.String(), "\n"))
}
