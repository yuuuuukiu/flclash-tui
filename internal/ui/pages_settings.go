package ui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"flclash-tui/internal/coreasset"
	"flclash-tui/internal/osshare"
	"flclash-tui/internal/paths"
	"flclash-tui/internal/settings"
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldBool
	fieldChoice
	fieldAction
)

type field struct {
	key     string
	label   string
	kind    fieldKind
	choices []string
	hint    string
	get     func(a *App) string
	toggle  func(a *App)
	set     func(a *App, v string) error
	action  func(a *App) tea.Cmd
}

type settingsPage struct {
	list    ListView
	fields  []field
	changed bool
}

func newSettingsPage() *settingsPage {
	p := &settingsPage{}
	p.fields = []field{
		{key: "corePath", label: "mihomo 内核路径", kind: fieldText,
			hint: "mihomo 可执行文件绝对路径（留空则自动使用内置内核）",
			get:  func(a *App) string { return orDash(a.Settings.CorePath) },
			set:  func(a *App, v string) error { a.Settings.CorePath = v; return nil }},
		{key: "coreMode", label: "内核模式", kind: fieldChoice, choices: []string{settings.CoreModeManaged, settings.CoreModeAttach},
			hint: "managed=TUI托管内核 attach=连接已有控制器",
			get:  func(a *App) string { return a.Settings.CoreMode },
			set:  func(a *App, v string) error { a.Settings.CoreMode = v; return nil }},
		{key: "controller", label: "控制器地址", kind: fieldText,
			hint: "external controller 监听地址",
			get:  func(a *App) string { return a.Settings.ControllerAddr },
			set:  func(a *App, v string) error { a.Settings.ControllerAddr = v; return nil }},
		{key: "secret", label: "Secret", kind: fieldText,
			hint: "控制器密钥",
			get:  func(a *App) string { return orDash(a.Settings.Secret) },
			set:  func(a *App, v string) error { a.Settings.Secret = v; return nil }},
		{key: "mixedPort", label: "混合端口", kind: fieldText,
			hint: "HTTP/SOCKS 混合代理端口",
			get:  func(a *App) string { return strconv.Itoa(a.Settings.MixedPort) },
			set: func(a *App, v string) error {
				n, err := strconv.Atoi(v)
				if err != nil || n <= 0 || n > 65535 {
					return errInvalidPort
				}
				a.Settings.MixedPort = n
				return nil
			}},
		{key: "mode", label: "代理模式", kind: fieldChoice, choices: []string{"rule", "global", "direct"},
			get: func(a *App) string { return a.Settings.Mode },
			set: func(a *App, v string) error { a.Settings.Mode = v; return nil }},
		{key: "logLevel", label: "日志等级", kind: fieldChoice, choices: []string{"debug", "info", "warning", "error", "silent"},
			get: func(a *App) string { return a.Settings.LogLevel },
			set: func(a *App, v string) error { a.Settings.LogLevel = v; return nil }},
		{key: "allowLan", label: "允许局域网", kind: fieldBool,
			get:    func(a *App) string { return boolStr(a.Settings.AllowLan) },
			toggle: func(a *App) { a.Settings.AllowLan = !a.Settings.AllowLan }},
		{key: "ipv6", label: "IPv6", kind: fieldBool,
			get:    func(a *App) string { return boolStr(a.Settings.IPv6) },
			toggle: func(a *App) { a.Settings.IPv6 = !a.Settings.IPv6 }},
		{key: "unifiedDelay", label: "统一延迟", kind: fieldBool,
			get:    func(a *App) string { return boolStr(a.Settings.UnifiedDelay) },
			toggle: func(a *App) { a.Settings.UnifiedDelay = !a.Settings.UnifiedDelay }},
		{key: "tcpConcurrent", label: "TCP 并发", kind: fieldBool,
			get:    func(a *App) string { return boolStr(a.Settings.TCPConcurrent) },
			toggle: func(a *App) { a.Settings.TCPConcurrent = !a.Settings.TCPConcurrent }},
		{key: "tun", label: "TUN 模式", kind: fieldBool,
			hint:   "需要 root/管理员权限",
			get:    func(a *App) string { return boolStr(a.Settings.Tun) },
			toggle: func(a *App) { a.Settings.Tun = !a.Settings.Tun }},
		{key: "tunStack", label: "TUN 栈", kind: fieldChoice, choices: []string{"gvisor", "system", "mixed"},
			get: func(a *App) string { return a.Settings.TunStack },
			set: func(a *App, v string) error { a.Settings.TunStack = v; return nil }},
		{key: "testURL", label: "测速 URL", kind: fieldText,
			get: func(a *App) string { return a.Settings.TestURL },
			set: func(a *App, v string) error { a.Settings.TestURL = v; return nil }},
		{key: "timeout", label: "测速超时(ms)", kind: fieldText,
			get: func(a *App) string { return strconv.Itoa(a.Settings.DelayTimeout) },
			set: func(a *App, v string) error {
				n, err := strconv.Atoi(v)
				if err != nil || n < 500 || n > 15000 {
					return errInvalidTimeout
				}
				a.Settings.DelayTimeout = n
				return nil
			}},
		{key: "autoUpdate", label: "订阅自动更新", kind: fieldBool,
			get:    func(a *App) string { return boolStr(a.Settings.AutoUpdate) },
			toggle: func(a *App) { a.Settings.AutoUpdate = !a.Settings.AutoUpdate }},
		{key: "autoUpdateInt", label: "自动更新间隔(分钟)", kind: fieldText,
			get: func(a *App) string { return strconv.Itoa(a.Settings.AutoUpdateInterval) },
			set: func(a *App, v string) error {
				n, err := strconv.Atoi(v)
				if err != nil || n < 5 {
					return errInvalidInterval
				}
				a.Settings.AutoUpdateInterval = n
				return nil
			}},
		{key: "proxyFallback", label: "订阅失败自动走代理", kind: fieldBool,
			hint:   "拉取/更新失败时经专用代理端口重试（应用后生效）",
			get:    func(a *App) string { return boolStr(a.Settings.ProxyFallback) },
			toggle: func(a *App) { a.Settings.ProxyFallback = !a.Settings.ProxyFallback }},
		{key: "eventPopup", label: "事件弹窗(右下角)", kind: fieldBool,
			hint:   "操作结果与内核错误通知",
			get:    func(a *App) string { return boolStr(a.Settings.EventPopup) },
			toggle: func(a *App) { a.Settings.EventPopup = !a.Settings.EventPopup }},
		{key: "sysProxy", label: "系统代理", kind: fieldAction,
			hint: "开关系统 HTTP/SOCKS 代理 (macOS)",
			get:  func(a *App) string { return boolStr(a.Settings.SystemProxy) },
			action: func(a *App) tea.Cmd {
				a.BusyStart()
				target := !a.Settings.SystemProxy
				return func() tea.Msg {
					defer a.BusyEnd()
					var err error
					if target {
						err = osshare.Enable(a.Settings)
					} else {
						err = osshare.Disable(a.Settings)
					}
					if err != nil {
						return resultMsg{job: "系统代理", err: err}
					}
					return sysProxyChangedMsg{on: target}
				}
			}},
		{key: "sysProxyService", label: "系统代理网络服务", kind: fieldText,
			hint: "networksetup 服务名，如 Wi-Fi / Ethernet",
			get:  func(a *App) string { return a.Settings.SystemProxyService },
			set:  func(a *App, v string) error { a.Settings.SystemProxyService = v; return nil }},

		{key: "sep", label: "─── 操作 ───", kind: fieldAction, get: func(a *App) string { return "" }},
		{key: "actApply", label: "应用设置并重启内核", kind: fieldAction, hint: "保存到磁盘 + 重新生成配置 + 重启",
			action: func(a *App) tea.Cmd { return a.saveAndApply() }},
		{key: "actCoreExtract", label: "重新释放内置内核", kind: fieldAction,
			hint: "把打包进本程序的 mihomo 解压到数据目录（修复损坏/换平台）",
			action: func(a *App) tea.Cmd {
				a.BusyStart()
				return func() tea.Msg {
					defer a.BusyEnd()
					p, err := coreasset.Ensure(paths.CoreBinFile())
					if err != nil {
						return resultMsg{job: "释放内核", err: err}
					}
					a.Settings.CorePath = p
					if err := a.Settings.Save(); err != nil {
						return resultMsg{job: "释放内核", err: err}
					}
					return resultMsg{job: "释放内核", text: "内置内核已就绪: " + p}
				}
			}},
		{key: "actSave", label: "仅保存设置", kind: fieldAction,
			action: func(a *App) tea.Cmd { return a.saveOnly() }},
		{key: "actGeo", label: "更新 GeoIP/GeoSite 数据库", kind: fieldAction,
			action: func(a *App) tea.Cmd {
				a.BusyStart()
				return func() tea.Msg {
					defer a.BusyEnd()
					ctx, cancel := ctxTimeout(120 * time.Second)
					defer cancel()
					if err := a.API.UpdateGeo(ctx); err != nil {
						return resultMsg{job: "更新Geo", err: err}
					}
					return resultMsg{job: "更新Geo", text: "Geo 数据库已更新"}
				}
			}},
		{key: "actFakeip", label: "清空 FakeIP 缓存", kind: fieldAction,
			action: func(a *App) tea.Cmd {
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
			}},
		{key: "actUpgrade", label: "在线升级内核版本", kind: fieldAction, hint: "调用 mihomo 自带升级（对二进制有写权限时）",
			action: func(a *App) tea.Cmd {
				a.BusyStart()
				return func() tea.Msg {
					defer a.BusyEnd()
					ctx, cancel := ctxTimeout(300 * time.Second)
					defer cancel()
					if err := a.API.UpgradeCore(ctx); err != nil {
						return resultMsg{job: "升级内核", err: err}
					}
					return resultMsg{job: "升级内核", text: "内核已升级，建议重启"}
				}
			}},
	}
	p.list.Len = len(p.fields)
	return p
}

var (
	errInvalidPort     = lipglossError("端口必须是 1-65535 的数字")
	errInvalidTimeout  = lipglossError("超时必须是 500-15000 毫秒")
	errInvalidInterval = lipglossError("间隔至少 5 分钟")
)

func lipglossError(s string) error { return &simpleErr{s} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func boolStr(b bool) string {
	if b {
		return "开启"
	}
	return "关闭"
}

func (p *settingsPage) Tab() string { return "设置" }
func (p *settingsPage) Shortcuts() string {
	return "[enter]编辑/切换/执行 [s]保存 [R]应用并重启  j/k移动"
}
func (p *settingsPage) OnEnter(a *App) tea.Cmd { return nil }

type sysProxyChangedMsg struct{ on bool }

func (p *settingsPage) Update(a *App, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case sysProxyChangedMsg:
		a.Settings.SystemProxy = m.on
		a.SystemProxy = m.on
		_ = a.Settings.Save()
		if m.on {
			return a.ShowToast("✅ 系统代理已开启", toastOK)
		}
		return a.ShowToast("✅ 系统代理已关闭", toastOK)
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
	case "enter", " ":
		if p.list.Cursor < 0 || p.list.Cursor >= len(p.fields) {
			return nil
		}
		f := p.fields[p.list.Cursor]
		switch f.kind {
		case fieldBool:
			f.toggle(a)
			p.changed = true
			return nil
		case fieldChoice:
			cur := f.get(a)
			idx := 0
			for i, c := range f.choices {
				if c == cur {
					idx = i
					break
				}
			}
			f.set(a, f.choices[(idx+1)%len(f.choices)])
			p.changed = true
			return nil
		case fieldText:
			cur := f.get(a)
			if cur == "—" {
				cur = ""
			}
			a.modal = NewInputModal("编辑 · "+f.label, f.hint, "", cur, func(v string) tea.Cmd {
				if err := f.set(a, v); err != nil {
					return a.ShowToast("❌ "+err.Error(), toastErr)
				}
				p.changed = true
				return a.ShowToast("已修改，记得保存", toastInfo)
			})
			return nil
		case fieldAction:
			if f.action != nil {
				return f.action(a)
			}
		}
	case "s", "S":
		return a.saveOnly()
	case "R":
		return a.saveAndApply()
	}
	return nil
}

func (a *App) saveOnly() tea.Cmd {
	if err := a.Settings.Save(); err != nil {
		return a.ShowToast("❌ 保存失败: "+err.Error(), toastErr)
	}
	if sp, ok := a.CurrentPage().(*settingsPage); ok {
		sp.changed = false
	}
	return a.ShowToast("✅ 设置已保存", toastOK)
}

func (a *App) saveAndApply() tea.Cmd {
	if err := a.Settings.Save(); err != nil {
		return a.ShowToast("❌ 保存失败: "+err.Error(), toastErr)
	}
	if sp, ok := a.CurrentPage().(*settingsPage); ok {
		sp.changed = false
	}
	return a.ApplyAndRestart()
}

func (p *settingsPage) View(a *App) string {
	w := a.width - 4
	if w < 40 {
		w = 40
	}
	h := a.height - 7
	if h < 3 {
		h = 3
	}
	p.list.Height = h - 2
	var b strings.Builder
	from, to := p.list.Window()
	for i := from; i < to; i++ {
		f := p.fields[i]
		cur := "  "
		if i == p.list.Cursor {
			cur = "▶ "
		}
		var line string
		switch f.kind {
		case fieldAction:
			line = styleYellow.Render(PadCJK(cur+f.label, w-4))
		case fieldBool:
			line = PadCJK(cur+f.label, w/2) + boolSettingLabel(f.get(a))
		case fieldChoice:
			line = PadCJK(cur+f.label, w/2) + styleAccent.Render(f.get(a))
		default:
			line = PadCJK(cur+f.label, w/2) + styleValue.Render(TruncateCJK(f.get(a), w-w/2-2))
		}
		if i == p.list.Cursor && f.kind != fieldAction {
			line = styleSelItem.Render(line)
		}
		b.WriteString(line + "\n")
	}
	head := styleCardTitle.Render(" 设置 ")
	if p.changed {
		head += styleYellow.Render("· 有未保存修改")
	}
	return styleCard.Width(w).Height(h).Render(head + "\n" + strings.TrimSuffix(b.String(), "\n"))
}

func boolSettingLabel(s string) string {
	if s == "开启" {
		return styleGreen.Render("● 开启")
	}
	return styleMuted.Render("○ 关闭")
}
