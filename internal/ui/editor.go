package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// EditorOverlay 全屏配置编辑器：直接手写/粘贴 YAML 或 JSON。
// ctrl+s 保存（保存动作返回 tea.Cmd，由 App 层路由）；esc 由 App 层处理关闭。
type EditorOverlay struct {
	Title  string
	Hint   string
	ta     textarea.Model
	OnSave func(content string) tea.Cmd
}

func NewEditor(title, hint, initial string, onSave func(string) tea.Cmd) *EditorOverlay {
	ta := textarea.New()
	ta.Placeholder = "在此粘贴或编写配置内容 —— 支持 YAML，也可以直接粘贴 JSON（保存时自动转为 YAML 存储）…"
	ta.SetValue(initial)
	ta.ShowLineNumbers = true
	ta.CharLimit = 4 << 20 // 4MB，足够大型订阅
	ta.Focus()
	return &EditorOverlay{Title: title, Hint: hint, ta: ta, OnSave: onSave}
}

// BlinkCmd 让光标开始闪烁。
func (e *EditorOverlay) BlinkCmd() tea.Cmd { return textarea.Blink }

func (e *EditorOverlay) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	return cmd
}

// Save 触发保存回调（在 Update 主循环内同步调用，回调只允许返回 Cmd）。
func (e *EditorOverlay) Save() tea.Cmd {
	if e.OnSave != nil {
		return e.OnSave(e.ta.Value())
	}
	return nil
}

func (e *EditorOverlay) View(a *App) string {
	w := a.width - 4
	if w < 40 {
		w = 40
	}
	h := a.height - 4
	if h < 6 {
		h = 6
	}
	e.ta.SetWidth(w - 2)
	e.ta.SetHeight(h - 4)

	var b strings.Builder
	head := styleCardTitle.Render(" " + e.Title)
	if e.Hint != "" {
		head += "   " + styleMuted.Render(e.Hint)
	}
	b.WriteString(head + "\n")
	b.WriteString(e.ta.View() + "\n")
	b.WriteString(styleHelpKey.Render(" ctrl+s ") + styleHelpDesc.Render("保存并应用") +
		styleHelpKey.Render("  esc ") + styleHelpDesc.Render("取消") +
		styleMuted.Render("   支持 YAML / JSON"))
	return styleCard.Width(w).Height(h).Render(b.String())
}

// SaveProfileContent 保存动作的通用实现：规范化内容并返回结果消息。
// id 为空表示新建（label 作为名称），否则为编辑指定订阅。
func SaveProfileContent(a *App, id, label string) func(string) tea.Cmd {
	created := id == ""
	return func(content string) tea.Cmd {
		return func() tea.Msg {
			if created {
				np, _, err := a.Profiles.AddFromContent(label, []byte(content))
				if err != nil {
					return resultMsg{job: "保存配置", err: err}
				}
				return profileSavedMsg{created: true, id: np.ID, label: np.Label, converted: wasJSON(content)}
			}
			if _, err := a.Profiles.SetContent(id, []byte(content)); err != nil {
				return resultMsg{job: "保存配置", err: err}
			}
			return profileSavedMsg{created: false, id: id, label: label, converted: wasJSON(content)}
		}
	}
}

// wasJSON 判断保存时是否发生了 JSON→YAML 转换。
func wasJSON(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), "{")
}
