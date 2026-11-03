package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"flclash-tui/internal/profiles"
)

type profilesPage struct {
	list     ListView
	snapshot []profiles.Profile
}

func (p *profilesPage) Tab() string { return "订阅" }
func (p *profilesPage) Shortcuts() string {
	return "[a]添加订阅 [n]手写配置 [e]编辑源 [f]导入文件 [enter]激活 [u]更新 [U]全部更新 [d]删除 [r]重命名  j/k移动"
}
func (p *profilesPage) OnEnter(a *App) tea.Cmd { return nil }

func (p *profilesPage) Update(a *App, msg tea.Msg) tea.Cmd {
	// 每次消息同步快照（有锁保护，开销可忽略）
	p.snapshot = a.Profiles.List()
	p.list.Len = len(p.snapshot)
	p.list.Clamp()

	switch m := msg.(type) {
	case subAddedMsg:
		p.snapshot = a.Profiles.List()
		p.list.Len = len(p.snapshot)
		p.list.Cursor = len(p.snapshot) - 1
		note := ""
		if m.viaProxy {
			note = "（经代理拉取）"
		}
		return tea.Batch(a.ApplyAndRestart(), a.ShowToast("✅ 已添加「"+m.label+"」"+note, toastOK))
	case subDeletedMsg:
		return a.ShowToast("✅ 订阅已删除", toastOK)
	case subRenamedMsg:
		return a.ShowToast("✅ 已重命名", toastOK)
	case profileSavedMsg:
		// 编辑器保存完成：关闭编辑器、定位光标，编辑激活订阅时自动应用
		a.editor = nil
		p.snapshot = a.Profiles.List()
		p.list.Len = len(p.snapshot)
		tip := ""
		if m.converted {
			tip = "（JSON 已转为 YAML）"
		}
		if m.created {
			p.list.Cursor = len(p.snapshot) - 1
			p.list.Clamp()
			return a.ShowToast("✅ 已创建「"+m.label+"」"+tip+" · 回车激活生效", toastOK)
		}
		p.list.Clamp()
		if it, ok := a.Profiles.Active(); ok && it.ID == m.id {
			return a.ApplyAndRestart()
		}
		return a.ShowToast("✅ 配置已保存"+tip+"（当前激活订阅未受影响）", toastOK)
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
	case "a", "A":
		a.modal = NewInputModal("添加订阅", "输入订阅链接 (Clash YAML)", "https://example.com/subscribe?token=…", "", func(v string) tea.Cmd {
			a.BusyStart()
			return func() tea.Msg {
				defer a.BusyEnd()
				ctx, cancel := ctxTimeout(60 * time.Second)
				defer cancel()
				np, _, err := a.Profiles.AddFromURL(ctx, v)
				if err != nil {
					return resultMsg{job: "添加订阅", err: err}
				}
				return subAddedMsg{label: np.Label, viaProxy: np.ViaProxy}
			}
		})
		return nil
	case "n", "N":
		// 手写配置：先取名称，再进入全屏编辑器（可粘贴 YAML/JSON）
		a.modal = NewInputModal("手写配置", "配置名称", "我的配置", "", func(v string) tea.Cmd {
			return func() tea.Msg {
				return editorOpenMsg{
					title: "新建 · " + v,
					hint:  "可直接粘贴 JSON，保存时自动转为 YAML",
					label: v,
				}
			}
		})
		return nil
	case "e", "E":
		item, ok := p.current(a)
		if !ok {
			return nil
		}
		id, label, file := item.ID, item.Label, item.File()
		return func() tea.Msg {
			data, err := os.ReadFile(file)
			if err != nil {
				return resultMsg{job: "打开编辑器", err: err}
			}
			return editorOpenMsg{
				title:   "编辑 · " + label,
				hint:    "保存后自动应用",
				initial: string(data),
				id:      id,
			}
		}
	case "f", "F":
		a.modal = NewInputModal("导入本地配置", "输入 YAML 文件绝对路径", "/path/to/config.yaml", "", func(v string) tea.Cmd {
			a.BusyStart()
			return func() tea.Msg {
				defer a.BusyEnd()
				np, err := a.Profiles.AddFromFile(v, "")
				if err != nil {
					return resultMsg{job: "导入文件", err: err}
				}
				return subAddedMsg{label: np.Label}
			}
		})
		return nil
	case "u":
		return p.updateOne(a)
	case "U":
		return p.updateAll(a)
	case "d", "D":
		return p.deleteOne(a)
	case "r":
		return p.renameOne(a)
	case "enter", " ":
		return p.activateOne(a)
	}
	return nil
}

type subAddedMsg struct {
	label    string
	viaProxy bool
}
type subUpdatedMsg struct {
	id       string
	viaProxy bool
}
type subDeletedMsg struct{}
type subRenamedMsg struct{}

func (p *profilesPage) current(a *App) (profiles.Profile, bool) {
	if p.list.Cursor < 0 || p.list.Cursor >= len(p.snapshot) {
		return profiles.Profile{}, false
	}
	return p.snapshot[p.list.Cursor], true
}

func (p *profilesPage) updateOne(a *App) tea.Cmd {
	item, ok := p.current(a)
	if !ok {
		return nil
	}
	if item.Type != profiles.TypeURL {
		return a.ShowToast("本地文件订阅无法在线更新", toastInfo)
	}
	a.BusyStart()
	return func() tea.Msg {
		defer a.BusyEnd()
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		np, err := a.Profiles.Update(ctx, item.ID)
		if err != nil {
			return resultMsg{job: "更新订阅", err: err}
		}
		return subUpdatedMsg{id: item.ID, viaProxy: np.ViaProxy}
	}
}

func (p *profilesPage) updateAll(a *App) tea.Cmd {
	var cmds []tea.Cmd
	for _, it := range a.Profiles.List() {
		if it.Type == profiles.TypeURL {
			id := it.ID
			a.BusyStart()
			cmds = append(cmds, func() tea.Msg {
				defer a.BusyEnd()
				ctx, cancel := ctxTimeout(60 * time.Second)
				defer cancel()
				np, err := a.Profiles.Update(ctx, id)
				if err != nil {
					return resultMsg{job: "更新订阅", err: err}
				}
				return subUpdatedMsg{id: id, viaProxy: np.ViaProxy}
			})
		}
	}
	if len(cmds) == 0 {
		return a.ShowToast("没有可更新的订阅", toastInfo)
	}
	return tea.Batch(cmds...)
}

func (p *profilesPage) deleteOne(a *App) tea.Cmd {
	item, ok := p.current(a)
	if !ok {
		return nil
	}
	a.modal = NewConfirmModal("删除订阅", fmt.Sprintf("确定删除「%s」吗？此操作不可撤销。", item.Label), func() tea.Cmd {
		return func() tea.Msg {
			if err := a.Profiles.Delete(item.ID); err != nil {
				return resultMsg{job: "删除订阅", err: err}
			}
			return subDeletedMsg{}
		}
	})
	return nil
}

func (p *profilesPage) renameOne(a *App) tea.Cmd {
	item, ok := p.current(a)
	if !ok {
		return nil
	}
	a.modal = NewInputModal("重命名订阅", "新名称", "", item.Label, func(v string) tea.Cmd {
		return func() tea.Msg {
			if err := a.Profiles.Rename(item.ID, v); err != nil {
				return resultMsg{job: "重命名", err: err}
			}
			return subRenamedMsg{}
		}
	})
	return nil
}

func (p *profilesPage) activateOne(a *App) tea.Cmd {
	item, ok := p.current(a)
	if !ok {
		return nil
	}
	if item.Active {
		return a.ShowToast("该订阅已是激活状态", toastInfo)
	}
	if err := a.Profiles.SetActive(item.ID); err != nil {
		return a.ShowToast("❌ 激活失败: "+err.Error(), toastErr)
	}
	p.snapshot = a.Profiles.List()
	return a.ApplyAndRestart()
}

func (p *profilesPage) View(a *App) string {
	p.snapshot = a.Profiles.List()
	p.list.Len = len(p.snapshot)
	p.list.Clamp()

	w := a.width - 4
	if w < 40 {
		w = 40
	}
	h := a.height - 7
	if h < 3 {
		h = 3
	}
	if len(p.snapshot) == 0 {
		return card(w, "订阅",
			styleMuted.Render(" 还没有订阅"),
			"",
			styleHelpKey.Render(" a ")+styleHelpDesc.Render("添加 URL 订阅"),
			styleHelpKey.Render(" f ")+styleHelpDesc.Render("导入本地 YAML 文件"),
		)
	}
	p.list.Height = h - 2
	var b strings.Builder
	from, to := p.list.Window()
	for i := from; i < to; i++ {
		it := p.snapshot[i]
		cur := "  "
		if i == p.list.Cursor {
			cur = "▶ "
		}
		mark := styleMuted.Render("[文件]")
		if it.Type == profiles.TypeURL {
			mark = stylePurple.Render("[订阅]")
		}
		activeMark := "   "
		if it.Active {
			activeMark = styleGreen.Render("●  ")
		}
		name := TruncateCJK(it.Label, max(10, w/2-10))
		line := activeMark + PadCJK(cur+name, w/2-6) + mark
		var right string
		if it.SubInfo != nil && it.SubInfo.Total > 0 {
			used := it.SubInfo.Upload + it.SubInfo.Download
			right = fmt.Sprintf("%s/%s 到期:%s", FormatBytes(used), FormatBytes(it.SubInfo.Total),
				shortExpire(it.SubInfo.Expire))
		} else if !it.UpdatedAt.IsZero() {
			right = "更新于 " + relTime(it.UpdatedAt)
		} else {
			right = "未更新"
		}
		line += styleMuted.Render(right)
		if it.Active {
			line += styleGreen.Render("  使用中")
		}
		if i == p.list.Cursor {
			line = styleSelItem.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return styleCard.Width(w).Height(h).Render(
		styleCardTitle.Render(fmt.Sprintf(" 订阅 (%d)", len(p.snapshot))) + "\n" + strings.TrimSuffix(b.String(), "\n"))
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d天前", int(d.Hours()/24))
	}
}

func shortExpire(unix int64) string {
	if unix <= 0 {
		return "长期"
	}
	days := int(time.Until(time.Unix(unix, 0)).Hours() / 24)
	if days < 0 {
		return "已过期"
	}
	return fmt.Sprintf("%d天", days)
}
