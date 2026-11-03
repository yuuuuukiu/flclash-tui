package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"flclash-tui/internal/clashapi"
	"flclash-tui/internal/coremgr"
	"flclash-tui/internal/engine"
	"flclash-tui/internal/paths"
	"flclash-tui/internal/profiles"
	"flclash-tui/internal/settings"
)

// ---------- 页面接口 ----------

type page interface {
	Tab() string
	Update(*App, tea.Msg) tea.Cmd
	View(*App) string
	Shortcuts() string
	OnEnter(*App) tea.Cmd
}

// ---------- 通用异步消息 ----------

type resultMsg struct {
	job  string // 任务名（防重复/显示）
	text string // 成功提示
	err  error
}

type delayOneMsg struct {
	name  string
	delay int
	err   error
}

type delayGroupMsg struct {
	group   string
	results map[string]int
	err     error
}

type proxiesMsg struct{ resp *clashapi.ProxiesResp }

type rulesMsg struct{ resp *clashapi.RulesResp }

type configMsg struct{ cfg *clashapi.GeneralConfig }

type versionMsg struct{ v *clashapi.Version }

type coreExitMsg struct {
	err  error
	code int
}

type configPatchMsg struct{ mode string }

// editorOpenMsg 请求打开全屏配置编辑器。
type editorOpenMsg struct {
	title   string
	hint    string
	initial string
	id      string // 编辑已有订阅时非空（为空表示新建）
	label   string // 新建时的名称
}

// profileSavedMsg 编辑器保存结果。
type profileSavedMsg struct {
	created   bool
	id        string
	label     string
	converted bool // JSON→YAML 转换发生
}

type tickMsg time.Time

type streamClosedMsg struct{ which string }

type doApplyRestartMsg struct{}

// ---------- 吐司类型（通知栈使用） ----------

type toastKind int

const (
	toastInfo toastKind = iota
	toastOK
	toastErr
)

// ---------- 弹窗 ----------

type Modal interface {
	Update(tea.Msg) (Modal, tea.Cmd)
	View() string
}

type InputModal struct {
	Title    string
	Label    string
	Input    textinput.Model
	Submit   func(string) tea.Cmd
	validate func(string) error
}

func NewInputModal(title, label, placeholder, initial string, submit func(string) tea.Cmd) *InputModal {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(initial)
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 60
	return &InputModal{Title: title, Label: label, Input: ti, Submit: submit}
}

func (m *InputModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return nil, nil
		case "enter":
			v := strings.TrimSpace(m.Input.Value())
			if v == "" {
				return m, nil
			}
			cmd := m.Submit(v)
			return nil, cmd
		}
	}
	var cmd tea.Cmd
	m.Input, cmd = m.Input.Update(msg)
	return m, cmd
}

func (m *InputModal) View() string {
	b := strings.Builder{}
	b.WriteString(styleCardTitle.Render(m.Title) + "\n\n")
	b.WriteString(styleMuted.Render(m.Label) + "\n")
	b.WriteString(m.Input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter 确认 · esc 取消"))
	return styleModal.Render(b.String())
}

type ConfirmModal struct {
	Title  string
	Text   string
	Danger bool
	Yes    func() tea.Cmd
}

func NewConfirmModal(title, text string, yes func() tea.Cmd) *ConfirmModal {
	return &ConfirmModal{Title: title, Text: text, Yes: yes, Danger: true}
}

func (m *ConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "n", "N", "q":
		return nil, nil
	case "enter", "y", "Y":
		cmd := m.Yes()
		return nil, cmd
	}
	return m, nil
}

func (m *ConfirmModal) View() string {
	b := strings.Builder{}
	title := m.Title
	if m.Danger {
		title = styleRed.Render("⚠ " + m.Title)
	}
	b.WriteString(title + "\n\n")
	b.WriteString(m.Text + "\n\n")
	b.WriteString(styleMuted.Render("enter/y 确认 · esc/n 取消"))
	return styleModal.Render(b.String())
}

// ---------- App ----------

type App struct {
	Settings *settings.Settings
	Profiles *profiles.Store
	Manager  *coremgr.Manager
	API      *clashapi.Client
	Streams  *clashapi.Manager

	pages   []page
	pageIdx int
	width   int
	height  int

	// 共享实时数据
	Traffic    clashapi.TrafficMsg
	TrafficHis []clashapi.TrafficMsg
	Memory     clashapi.MemoryMsg
	Conns      *clashapi.ConnectionsResp
	Logs       []clashapi.LogMsg
	Version    string

	Mode        string
	SystemProxy bool

	// 右下角事件通知栈
	notifs   []Notification
	notifSeq int

	modal   Modal
	editor  *EditorOverlay
	spinner spinner.Model
	busy    int

	quitting bool
	starting bool
}

func NewApp(s *settings.Settings, ps *profiles.Store, m *coremgr.Manager) *App {
	sp := spinner.New()
	sp.Spinner = spinner.Meter
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	app := &App{
		Settings: s,
		Profiles: ps,
		Manager:  m,
		Streams:  clashapi.NewManager(s.APIBase(), s.Secret),
		spinner:  sp,
	}
	app.API = clashapi.New(app.Settings.APIBase(), app.Settings.Secret)
	app.API.TestURL = app.Settings.TestURL
	app.Logs = make([]clashapi.LogMsg, 0, 256)
	app.pages = []page{
		&dashboardPage{},
		&proxiesPage{},
		&profilesPage{},
		newConnectionsPage(),
		newRulesPage(),
		newLogsPage(),
		newSettingsPage(),
	}
	app.SystemProxy = app.Settings.SystemProxy
	app.applyFetchConfig()
	return app
}

func (a *App) CurrentPage() page { return a.pages[a.pageIdx] }

// inputActive 判断当前是否有文本输入正在接收按键（此时 q 不应退出）。
func (a *App) inputActive() bool {
	if a.modal != nil {
		return true
	}
	if ip, ok := a.CurrentPage().(interface{ IsInputActive() bool }); ok {
		return ip.IsInputActive()
	}
	return false
}

func (a *App) SetPage(i int) tea.Cmd {
	if i < 0 || i >= len(a.pages) || i == a.pageIdx {
		return nil
	}
	a.pageIdx = i
	p := a.pages[i]
	if cp, ok := p.(interface{ NeedRefresh() bool }); ok && cp.NeedRefresh() {
		// 有条件刷新由各页面自己处理
		_ = cp
	}
	return p.OnEnter(a)
}

// BusyStart / BusyEnd 跟踪后台任务数量用于显示 spinner。
func (a *App) BusyStart() { a.busy++ }
func (a *App) BusyEnd() {
	if a.busy > 0 {
		a.busy--
	}
}

// ShowToast 推送一条右下角事件通知（历史接口名，内部为通知栈）。
func (a *App) ShowToast(text string, kind toastKind) tea.Cmd {
	a.pushNotif(text, kind)
	return nil
}

// applyFetchConfig 把设置中的拉取回退配置下发给订阅存储。
func (a *App) applyFetchConfig() {
	a.Profiles.SetFetchConfig(profiles.FetchConfig{
		Enabled:      a.Settings.ProxyFallback,
		FallbackPort: a.Settings.MixedPort + 1,
		MixedPort:    a.Settings.MixedPort,
	})
}

// ---------- 启动流程 ----------

// WriteCoreConfig 由活动订阅+设置生成 config.yaml（与 CLI 共用同一套管线）。
func (a *App) WriteCoreConfig() error {
	return engine.WriteCoreConfig(a.Settings, a.Profiles)
}

// StartCore 启动内核并启动流（managed 模式）。
func (a *App) StartCore() tea.Cmd {
	a.BusyStart()
	a.starting = true
	return func() tea.Msg {
		err := a.Manager.Start(a.Settings)
		a.BusyEnd()
		a.starting = false
		if err != nil {
			return resultMsg{job: "core", err: err}
		}
		return doApplyRestartMsg{}
	}
}

// ReconnectStreams 重置 API 客户端并重启流（channel 复用，监听命令无需重建）。
func (a *App) ReconnectStreams() tea.Cmd {
	a.API = clashapi.New(a.Settings.APIBase(), a.Settings.Secret)
	a.API.TestURL = a.Settings.TestURL
	a.Streams.Reconfigure(a.Settings.APIBase(), a.Settings.Secret)
	a.Streams.Restart()
	return a.FetchAll()
}

// FetchAll 拉取初始数据。
func (a *App) FetchAll() tea.Cmd {
	return tea.Batch(a.fetchVersion(), a.fetchProxies(), a.fetchRules(), a.fetchConfigs())
}

func (a *App) fetchVersion() tea.Cmd {
	a.BusyStart()
	return bgJob(func(ctx context.Context) tea.Msg {
		defer a.BusyEnd()
		v, err := a.API.Version(ctx)
		if err != nil {
			return versionMsg{}
		}
		return versionMsg{v}
	})
}

func (a *App) fetchProxies() tea.Cmd {
	a.BusyStart()
	return bgJob(func(ctx context.Context) tea.Msg {
		defer a.BusyEnd()
		r, err := a.API.GetProxies(ctx)
		if err != nil {
			return resultMsg{job: "proxies", err: err}
		}
		return proxiesMsg{r}
	})
}

func (a *App) fetchRules() tea.Cmd {
	a.BusyStart()
	return bgJob(func(ctx context.Context) tea.Msg {
		defer a.BusyEnd()
		r, err := a.API.GetRules(ctx)
		if err != nil {
			return resultMsg{job: "rules", err: err}
		}
		return rulesMsg{r}
	})
}

func (a *App) fetchConfigs() tea.Cmd {
	a.BusyStart()
	return bgJob(func(ctx context.Context) tea.Msg {
		defer a.BusyEnd()
		r, err := a.API.GetConfigs(ctx)
		if err != nil {
			return resultMsg{job: "configs", err: err}
		}
		return configMsg{r}
	})
}

// CycleMode 循环切换 rule -> global -> direct。
func (a *App) CycleMode() tea.Cmd {
	next := map[string]string{"rule": "global", "global": "direct", "direct": "rule"}[a.Mode]
	if next == "" {
		next = "rule"
	}
	return a.SetMode(next)
}

func (a *App) SetMode(mode string) tea.Cmd {
	a.BusyStart()
	return func() tea.Msg {
		defer a.BusyEnd()
		ctx, cancel := ctxTimeout(5 * time.Second)
		defer cancel()
		err := a.API.PatchConfigs(ctx, map[string]any{"mode": mode})
		if err != nil {
			return resultMsg{job: "mode", err: err}
		}
		return configPatchMsg{mode: mode}
	}
}

// ApplyAndRestart 应用设置：写配置 → 重启内核(managed) 或热重载(attach/外部内核) → 重连。
func (a *App) ApplyAndRestart() tea.Cmd {
	a.BusyStart()
	return func() tea.Msg {
		defer a.BusyEnd()
		if err := a.WriteCoreConfig(); err != nil {
			return resultMsg{job: "apply", err: err}
		}
		if a.Settings.CoreMode == settings.CoreModeManaged {
			switch a.Manager.State() {
			case coremgr.StateRunning:
				if err := a.Manager.Restart(a.Settings); err != nil {
					return resultMsg{job: "apply", err: err}
				}
			case coremgr.StateStopped:
				// 内核不是本 TUI 托管的（例如 CLI 后台启动）：原地热重载，不抢进程
				if engine.Reachable(a.Settings, 1500*time.Millisecond) {
					if err := engine.Reload(a.Settings); err != nil {
						return resultMsg{job: "apply", err: err}
					}
				} else if err := a.Manager.Start(a.Settings); err != nil {
					return resultMsg{job: "apply", err: err}
				}
			}
		} else {
			ctx, cancel := ctxTimeout(15 * time.Second)
			defer cancel()
			if err := a.API.ReloadConfig(ctx, paths.CoreConfigFile(), true); err != nil {
				return resultMsg{job: "apply", err: err}
			}
		}
		return doApplyRestartMsg{}
	}
}

// ---------- Init / Update / View ----------

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
		a.spinner.Tick,
		autoUpdateTickerCmd(),
		listenTraffic(a.Streams.Traffic),
		listenMemory(a.Streams.Memory),
		listenLogs(a.Streams.Logs),
		listenConns(a.Streams.Conns),
		a.FetchAll(),
	}
	// 监听内核退出
	cmds = append(cmds, a.listenCoreExit())
	// managed 模式：启动内核（成功后经 doApplyRestartMsg 重连流）
	if a.Settings.CoreMode == settings.CoreModeManaged && a.Settings.CorePath != "" {
		cmds = append(cmds, a.StartCore())
	}
	// attach 模式启动流
	if a.Settings.CoreMode == settings.CoreModeAttach {
		a.Streams.Start()
	}
	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			a.quitting = true
			return a, tea.Quit
		}
		// 配置编辑器全接管
		if a.editor != nil {
			switch msg.String() {
			case "esc":
				a.editor = nil
				return a, a.ShowToast("编辑已取消", toastInfo)
			case "ctrl+s":
				return a, a.editor.Save()
			}
			return a, a.editor.Update(msg)
		}
		if a.modal != nil {
			m, cmd := a.modal.Update(msg)
			a.modal = m
			return a, cmd
		}
		// bubbletea 会把一次读到的连续可打印字符合并成单个多 rune KeyMsg，
		// 导航键（j/k/g/G/数字…）需要拆成单键依次处理；弹窗/编辑器路径不受影响。
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			return a, tea.Batch(cmds...)
		}
		switch msg.String() {
		case "?":
			return a, a.ShowHelp()
		case "tab":
			return a, a.SetPage((a.pageIdx + 1) % len(a.pages))
		case "shift+tab":
			return a, a.SetPage((a.pageIdx - 1 + len(a.pages)) % len(a.pages))
		case "1", "2", "3", "4", "5", "6", "7":
			return a, a.SetPage(int(msg.Runes[0] - '1'))
		case "m":
			return a, a.CycleMode()
		case "q":
			// 文本输入激活时 q 是普通字符
			if !a.inputActive() {
				a.quitting = true
				return a, tea.Quit
			}
		case "Q":
			a.quitting = true
			return a, tea.Quit
		}
		return a, a.CurrentPage().Update(a, msg)

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return a, a.CurrentPage().Update(a, tea.KeyMsg{Type: tea.KeyUp})
		case tea.MouseButtonWheelDown:
			return a, a.CurrentPage().Update(a, tea.KeyMsg{Type: tea.KeyDown})
		}

	case tickMsg:
		// 通知过期 + 状态刷新；同时给当前页一个 1s 心跳（日志重建/快照刷新等）
		a.expireNotifs(time.Now())
		return a, tea.Batch(tickCmd(), a.spinner.Tick, a.CurrentPage().Update(a, msg))

	case spinner.TickMsg:
		a.spinner, _ = a.spinner.Update(msg)
		return a, nil

	case clashapi.TrafficMsg:
		a.Traffic = msg
		a.TrafficHis = append(a.TrafficHis, msg)
		if len(a.TrafficHis) > 180 {
			a.TrafficHis = a.TrafficHis[1:]
		}
		return a, listenTraffic(a.Streams.Traffic)

	case clashapi.MemoryMsg:
		a.Memory = msg
		return a, listenMemory(a.Streams.Memory)

	case clashapi.LogMsg:
		a.Logs = append(a.Logs, msg)
		if len(a.Logs) > 2000 {
			a.Logs = a.Logs[len(a.Logs)-2000:]
		}
		// 内核重要错误日志镜像到右下角事件栈
		if msg.Type == "error" {
			a.pushNotif("内核错误: "+msg.Payload, toastErr)
		}
		return a, listenLogs(a.Streams.Logs)

	case clashapi.ConnectionsResp:
		a.Conns = &msg
		if cp, ok := a.CurrentPage().(*connectionsPage); ok {
			cp.applySnapshot(a, &msg)
		}
		return a, listenConns(a.Streams.Conns)

	case coreExitMsg:
		if a.Manager.StoppedIntentionally() {
			// 主动重启/停止，不告警
			return a, nil
		}
		tail := strings.Join(a.Manager.ReadLogTail(6), "\n")
		return a, a.ShowToast(fmt.Sprintf("内核进程退出(code=%d) %s", msg.code, tail), toastErr)

	case versionMsg:
		if msg.v != nil {
			a.Version = msg.v.Version
		}
		return a, nil

	case proxiesMsg:
		if cp, ok := a.CurrentPage().(*proxiesPage); ok {
			return a, cp.applyProxies(msg.resp)
		}
		return a, nil

	case fetchProvidersMsg:
		if cp, ok := a.CurrentPage().(*proxiesPage); ok {
			cp.applyProviders(msg.resp)
		}
		return a, nil

	case nodeChangedMsg:
		if cp, ok := a.CurrentPage().(*proxiesPage); ok {
			cp.groupNow[msg.group] = msg.name
		}
		return a, a.ShowToast(fmt.Sprintf("已切换 %s → %s", msg.group, msg.name), toastOK)

	case editorOpenMsg:
		a.editor = NewEditor(msg.title, msg.hint, msg.initial, SaveProfileContent(a, msg.id, msg.label))
		return a, a.editor.BlinkCmd()

	case subUpdatedMsg:
		// 自动/手动更新完成后：若更新的是激活订阅则重新生成配置并重启（不依赖当前页面）
		note := ""
		if it, ok := a.Profiles.Get(msg.id); ok && it.ViaProxy {
			note = "（经代理拉取）"
		}
		if it, ok := a.Profiles.Active(); ok && it.ID == msg.id {
			return a, tea.Batch(a.ApplyAndRestart(), a.ShowToast("✅ 订阅已更新"+note, toastOK))
		}
		return a, a.ShowToast("✅ 订阅已更新"+note+"（激活订阅未受影响）", toastOK)

	case rulesMsg:
		if cp, ok := a.CurrentPage().(*rulesPage); ok {
			return a, cp.applyRules(msg.resp)
		}
		return a, nil

	case configMsg:
		if msg.cfg != nil {
			a.Mode = msg.cfg.Mode
		}
		return a, nil

	case configPatchMsg:
		a.Mode = msg.mode
		return a, a.ShowToast("模式已切换: "+modeLabel(msg.mode), toastOK)

	case resultMsg:
		if msg.err != nil {
			if msg.job == "proxies" || msg.job == "configs" || msg.job == "rules" || msg.job == "providers" {
				// 静默的初始拉取失败
				return a, nil
			}
			return a, a.ShowToast("❌ "+msg.job+": "+msg.err.Error(), toastErr)
		}
		if msg.text == "" {
			return a, nil
		}
		return a, a.ShowToast("✅ "+msg.text, toastOK)

	case delayOneMsg:
		if cp, ok := a.CurrentPage().(*proxiesPage); ok {
			cp.applyDelayOne(msg)
		}
		return a, nil

	case delayGroupMsg:
		if cp, ok := a.CurrentPage().(*proxiesPage); ok {
			cp.applyDelayGroup(msg)
		}
		return a, nil

	case autoUpdateTickMsg:
		return a, tea.Batch(a.checkAutoUpdates(), autoUpdateTickerCmd())

	case doApplyRestartMsg:
		cmds := []tea.Cmd{a.ReconnectStreams(), a.ShowToast("已应用并重启内核", toastOK), a.listenCoreExit()}
		return a, tea.Batch(cmds...)
	}
	return a, a.CurrentPage().Update(a, msg)
}

func (a *App) View() string {
	if a.quitting {
		return "再见 👋\n"
	}
	var b strings.Builder

	// 顶栏
	tabs := make([]string, 0, len(a.pages))
	for i, p := range a.pages {
		label := fmt.Sprintf("%d%s", i+1, p.Tab())
		if i == a.pageIdx {
			tabs = append(tabs, styleTabActive.Render(" "+label+" "))
		} else {
			tabs = append(tabs, styleTab.Render(label))
		}
	}
	busy := ""
	if a.busy > 0 || a.starting {
		busy = a.spinner.View() + " "
	}
	status := styleMuted.Render(busy + modeLabel(a.Mode) + " · " + coreStateLabel(a))
	title := "FLClash TUI"
	if a.Version != "" {
		title += " " + styleMuted.Render("("+a.Version+")")
	}
	top := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", strings.Join(tabs, ""), "   ", status)
	b.WriteString(top + "\n")

	// 内容
	bodyH := a.height - 4 // 顶栏1 + 状态栏2 + 余量
	if bodyH < 1 {
		bodyH = 1
	}
	var body string
	switch {
	case a.editor != nil:
		// 全屏配置编辑器
		body = a.editor.View(a)
	case a.modal != nil:
		// 模态浮层：居中覆盖内容区
		body = lipgloss.Place(a.width, bodyH, lipgloss.Center, lipgloss.Center, a.modal.View())
	default:
		body = a.CurrentPage().View(a)
	}
	// 右下角事件通知栈覆盖在内容之上
	body = overlayBottomRight(body, a.notifLines(), a.width, bodyH)
	b.WriteString(body + "\n")

	// 状态栏
	left := " " + a.CurrentPage().Shortcuts()
	right := ""
	if a.Traffic.Down > 0 || a.Traffic.Up > 0 || a.Conns != nil {
		right = fmt.Sprintf("↓%s ↑%s ", FormatSpeed(a.Traffic.Down), FormatSpeed(a.Traffic.Up))
	}
	if a.Settings.SystemProxy && a.SystemProxy {
		right += "系统代理 "
	}
	barW := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	bar := styleStatusBar.Render(PadCJK(left, max(1, barW)) + right)
	b.WriteString(bar)

	return b.String()
}

func modeLabel(mode string) string {
	switch mode {
	case "rule":
		return "规则"
	case "global":
		return "全局"
	case "direct":
		return "直连"
	case "":
		return "—"
	}
	return mode
}

func coreStateLabel(a *App) string {
	switch a.Manager.State() {
	case coremgr.StateRunning:
		return "内核运行中"
	case coremgr.StateStarting:
		return "内核启动中"
	default:
		if a.Settings.CoreMode == settings.CoreModeAttach {
			if a.Version != "" {
				return "已连接外部控制器"
			}
			return "等待外部控制器"
		}
		return "内核已停止"
	}
}
