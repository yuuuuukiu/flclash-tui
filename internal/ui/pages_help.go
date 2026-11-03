package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpModal 全局帮助浮层（? 打开，任意键关闭）。
type HelpModal struct{}

type helpEntry struct{ key, desc string }

var globalHelp = []helpEntry{
	{"1-7 / Tab", "切换页面（仪表盘/代理/订阅/连接/规则/日志/设置）"},
	{"m", "循环切换代理模式（规则→全局→直连）"},
	{"?", "打开/关闭本帮助"},
	{"Q / Ctrl+C", "退出"},
	{"j k 或 ↑ ↓", "上下移动"},
	{"g / G", "跳到顶部 / 底部"},
	{"esc", "关闭弹窗 / 取消输入"},
}

var pageHelp = map[string][]helpEntry{
	"仪表盘": {
		{"r", "重启内核（重新生成配置）"},
		{"c", "关闭全部连接"},
		{"f", "清空 FakeIP 缓存"},
	},
	"代理": {
		{"enter", "选择当前节点（Selector 组）"},
		{"t", "测速当前节点"},
		{"T", "并发测速整组"},
		{"s", "按延迟排序开关"},
		{"p", "切换到 Providers 视图（u 更新 / h 健康检查）"},
		{"r", "刷新代理列表"},
		{"J K H L", "节点列表导航"},
	},
	"订阅": {
		{"a", "添加 URL 订阅（自动解析流量信息与名称）"},
		{"n", "手写配置：全屏编辑器，可粘贴 YAML 或 JSON（JSON 自动转 YAML）"},
		{"e", "编辑当前订阅的原始配置（保存后自动重载内核）"},
		{"f", "导入本地 YAML 文件"},
		{"enter / space", "激活订阅（自动重启内核生效）"},
		{"u / U", "更新当前 / 全部订阅"},
		{"d", "删除订阅（需确认）"},
		{"r", "重命名"},
	},
	"连接": {
		{"/", "按主机/进程/规则/链路过滤"},
		{"x", "关闭光标处连接"},
		{"X", "关闭全部连接（需确认）"},
		{"s", "切换排序（时间/流量）"},
	},
	"规则": {
		{"/", "过滤规则"},
		{"r", "重新加载规则"},
	},
	"日志": {
		{"/", "按内容过滤"},
		{"l", "切换日志级别 info→warning→error"},
		{"space / f", "暂停/恢复自动跟随"},
		{"c", "清空日志"},
		{"PgUp / PgDn", "翻页"},
	},
	"设置": {
		{"enter", "编辑文本 / 切换布尔 / 循环选项 / 执行操作"},
		{"s", "保存设置"},
		{"R", "保存并应用（重新生成配置+重启内核）"},
	},
}

func (h *HelpModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return nil, nil
	}
	return h, nil
}

func (h *HelpModal) View() string {
	w := 84
	var b strings.Builder
	b.WriteString(styleCardTitle.Render(" FLClash TUI 帮助") + "\n\n")

	b.WriteString(styleAccent.Render("全局快捷键") + "\n")
	for _, e := range globalHelp {
		b.WriteString("  " + styleHelpKey.Render(PadCJK(e.key, 14)) + styleHelpDesc.Render(e.desc) + "\n")
	}
	b.WriteString("\n")

	// 当前页帮助由 App 注入
	return styleModal.Render(lipgloss.NewStyle().Width(w).Render(b.String()))
}

// HelpModalWithPage 带页面上下文的帮助。
type HelpModalWithPage struct {
	PageName string
	Entries  []helpEntry
}

func (h *HelpModalWithPage) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return nil, nil
	}
	return h, nil
}

func (h *HelpModalWithPage) View() string {
	w := 84
	var b strings.Builder
	b.WriteString(styleCardTitle.Render(" FLClash TUI 帮助") + "\n\n")
	b.WriteString(styleAccent.Render("全局快捷键") + "\n")
	for _, e := range globalHelp {
		b.WriteString("  " + styleHelpKey.Render(PadCJK(e.key, 14)) + styleHelpDesc.Render(e.desc) + "\n")
	}
	if entries, ok := pageHelp[h.PageName]; ok {
		b.WriteString("\n" + styleAccent.Render("当前页 · "+h.PageName) + "\n")
		for _, e := range entries {
			b.WriteString("  " + styleHelpKey.Render(PadCJK(e.key, 14)) + styleHelpDesc.Render(e.desc) + "\n")
		}
	}
	b.WriteString("\n" + styleMuted.Render("任意键返回"))
	return styleModal.Render(lipgloss.NewStyle().Width(w).Render(b.String()))
}
