package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"flclash-tui/internal/clashapi"
)

var groupTypes = map[string]bool{
	"Selector": true, "URLTest": true, "Fallback": true,
	"LoadBalance": true, "Relay": true, "Smart": true,
}

type proxiesPage struct {
	lastResp     *clashapi.ProxiesResp
	groups       []string
	groupNow     map[string]string
	groupType    map[string]string
	groupList    ListView
	nodeList     ListView
	nodes        []string
	delay        map[string]int
	nodeType     map[string]string
	sortByDelay  bool
	testing      bool
	showProvider bool
	providerList ListView
	providers    []string
	providerMap  map[string]clashapi.ProxyProvider
}

func (p *proxiesPage) Tab() string { return "代理" }
func (p *proxiesPage) Shortcuts() string {
	if p.showProvider {
		return "[p]返回代理 [u]更新Provider [h]健康检查  j/k移动"
	}
	return "[enter]选择节点 [t]测当前 [T]组测速 [s]按延迟排序 [p]Providers [r]刷新  j/k移动 · J/K节点"
}
func (p *proxiesPage) OnEnter(a *App) tea.Cmd { return a.fetchProxies() }

func (p *proxiesPage) Update(a *App, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if p.showProvider {
		return p.updateProviders(a, key)
	}
	switch key.String() {
	case "j", "down":
		p.groupList.Down()
	case "k", "up":
		p.groupList.Up()
	case "g":
		p.groupList.Top()
	case "G":
		p.groupList.Bottom()
	case "J":
		p.nodeList.Down()
	case "K":
		p.nodeList.Up()
	case "L":
		p.nodeList.Bottom()
	case "H":
		p.nodeList.Top()
	case "enter":
		return p.selectNode(a)
	case "t":
		return p.testOne(a)
	case "T":
		return p.testGroup(a)
	case "s":
		p.sortByDelay = !p.sortByDelay
		p.rebuildNodes()
	case "p":
		p.showProvider = true
		return a.fetchProviders()
	case "r":
		return a.fetchProxies()
	}
	return nil
}

func (p *proxiesPage) selectNode(a *App) tea.Cmd {
	if p.groupList.Cursor >= len(p.groups) || p.groupList.Cursor < 0 {
		return nil
	}
	group := p.groups[p.groupList.Cursor]
	gt := p.groupType[group]
	if p.nodeList.Cursor >= len(p.nodes) || p.nodeList.Cursor < 0 {
		return nil
	}
	if gt != "Selector" && gt != "Fallback" {
		return a.ShowToast("组类型 "+gt+" 不支持手动切换", toastInfo)
	}
	name := p.nodes[p.nodeList.Cursor]
	a.BusyStart()
	return func() tea.Msg {
		defer a.BusyEnd()
		ctx, cancel := ctxTimeout(5 * time.Second)
		defer cancel()
		if err := a.API.SelectProxy(ctx, group, name); err != nil {
			return resultMsg{job: "切换节点", err: err}
		}
		return nodeChangedMsg{group: group, name: name}
	}
}

type nodeChangedMsg struct{ group, name string }

func (p *proxiesPage) testOne(a *App) tea.Cmd {
	if p.nodeList.Cursor >= len(p.nodes) || p.nodeList.Cursor < 0 {
		return nil
	}
	name := p.nodes[p.nodeList.Cursor]
	p.testing = true
	a.BusyStart()
	testURL, timeout := a.Settings.TestURL, a.Settings.DelayTimeout
	return func() tea.Msg {
		defer a.BusyEnd()
		ctx, cancel := ctxTimeout(time.Duration(timeout+2000) * time.Millisecond)
		defer cancel()
		d, err := a.API.ProxyDelay(ctx, name, testURL, timeout)
		return delayOneMsg{name: name, delay: d, err: err}
	}
}

func (p *proxiesPage) testGroup(a *App) tea.Cmd {
	if p.groupList.Cursor >= len(p.groups) || p.groupList.Cursor < 0 {
		return nil
	}
	group := p.groups[p.groupList.Cursor]
	p.testing = true
	a.BusyStart()
	testURL, timeout := a.Settings.TestURL, a.Settings.DelayTimeout
	return func() tea.Msg {
		defer a.BusyEnd()
		ctx, cancel := ctxTimeout(time.Duration(timeout+3000) * time.Millisecond)
		defer cancel()
		results, err := a.API.GroupDelay(ctx, group, testURL, timeout)
		return delayGroupMsg{group: group, results: results, err: err}
	}
}

func (p *proxiesPage) applyProxies(resp *clashapi.ProxiesResp) tea.Cmd {
	if resp == nil {
		return nil
	}
	p.lastResp = resp
	if p.delay == nil {
		p.delay = map[string]int{}
	}
	p.groupNow = map[string]string{}
	p.groupType = map[string]string{}
	p.nodeType = map[string]string{}
	p.groups = nil
	for name, pr := range resp.Proxies {
		if len(pr.History) > 0 && pr.History[len(pr.History)-1].Delay > 0 {
			p.delay[name] = pr.History[len(pr.History)-1].Delay
		}
	}
	for name, pr := range resp.Proxies {
		if groupTypes[pr.Type] {
			p.groups = append(p.groups, name)
			p.groupType[name] = pr.Type
			p.groupNow[name] = pr.Now
		}
		p.nodeType[name] = pr.Type
	}
	sort.Strings(p.groups)
	// GLOBAL 组排到最后（对齐 FLClash 的展示习惯）
	for i, g := range p.groups {
		if g == "GLOBAL" && i != len(p.groups)-1 {
			p.groups = append(p.groups[:i], p.groups[i+1:]...)
			p.groups = append(p.groups, "GLOBAL")
			break
		}
	}
	if p.groupList.Cursor >= len(p.groups) {
		p.groupList.Cursor = 0
	}
	p.rebuildNodes()
	return nil
}

func (p *proxiesPage) rebuildNodes() {
	if p.lastResp == nil || p.groupList.Cursor >= len(p.groups) || p.groupList.Cursor < 0 {
		p.nodes = nil
		return
	}
	group := p.groups[p.groupList.Cursor]
	pr, ok := p.lastResp.Proxies[group]
	if !ok {
		p.nodes = nil
		return
	}
	prev := ""
	if p.nodeList.Cursor < len(p.nodes) {
		prev = p.nodes[p.nodeList.Cursor]
	}
	p.nodes = append([]string(nil), pr.All...)
	if p.sortByDelay {
		sort.SliceStable(p.nodes, func(i, j int) bool {
			di, oki := p.delay[p.nodes[i]]
			dj, okj := p.delay[p.nodes[j]]
			// -1 表示超时，排在最后
			si, sj := delayScore(di, oki), delayScore(dj, okj)
			if si != sj {
				return si < sj
			}
			if si < maxDelayScore {
				return di < dj
			}
			return p.nodes[i] < p.nodes[j]
		})
	}
	// 尽量保持光标在原节点上
	if prev != "" {
		for i, n := range p.nodes {
			if n == prev {
				p.nodeList.Cursor = i
				break
			}
		}
	}
}

const (
	maxDelayScore    = 3
	delayScoreNoTest = 1
	delayScoreOK     = 0
	delayScoreFail   = 2
)

func delayScore(d int, ok bool) int {
	if !ok || d == 0 {
		return delayScoreNoTest
	}
	if d < 0 {
		return delayScoreFail
	}
	return delayScoreOK
}

func (p *proxiesPage) applyDelayOne(msg delayOneMsg) {
	p.testing = false
	if p.delay == nil {
		p.delay = map[string]int{}
	}
	if msg.err != nil {
		p.delay[msg.name] = -1
	} else {
		p.delay[msg.name] = msg.delay
	}
}

func (p *proxiesPage) applyDelayGroup(msg delayGroupMsg) {
	p.testing = false
	if msg.err != nil {
		return
	}
	if p.delay == nil {
		p.delay = map[string]int{}
	}
	for name, d := range msg.results {
		p.delay[name] = d
	}
	if p.sortByDelay {
		p.rebuildNodes()
	}
}

// ---------- Provider 视图 ----------

type fetchProvidersMsg struct{ resp *clashapi.ProvidersResp }

func (a *App) fetchProviders() tea.Cmd {
	a.BusyStart()
	return func() tea.Msg {
		defer a.BusyEnd()
		ctx, cancel := ctxTimeout(10 * time.Second)
		defer cancel()
		r, err := a.API.GetProviders(ctx)
		if err != nil {
			return resultMsg{job: "providers", err: err}
		}
		return fetchProvidersMsg{r}
	}
}

func (p *proxiesPage) updateProviders(a *App, key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "p":
		p.showProvider = false
		return nil
	case "j", "down":
		p.providerList.Down()
	case "k", "up":
		p.providerList.Up()
	case "u":
		if p.providerList.Cursor < len(p.providers) {
			name := p.providers[p.providerList.Cursor]
			a.BusyStart()
			return func() tea.Msg {
				defer a.BusyEnd()
				ctx, cancel := ctxTimeout(60 * time.Second)
				defer cancel()
				if err := a.API.UpdateProvider(ctx, name); err != nil {
					return resultMsg{job: "更新 " + name, err: err}
				}
				return resultMsg{job: "更新 " + name, text: "Provider 已更新"}
			}
		}
	case "h":
		if p.providerList.Cursor < len(p.providers) {
			name := p.providers[p.providerList.Cursor]
			a.BusyStart()
			return func() tea.Msg {
				defer a.BusyEnd()
				ctx, cancel := ctxTimeout(60 * time.Second)
				defer cancel()
				if err := a.API.HealthCheckProvider(ctx, name); err != nil {
					return resultMsg{job: "健康检查 " + name, err: err}
				}
				return resultMsg{job: "健康检查 " + name, text: "健康检查完成"}
			}
		}
	}
	return nil
}

func (p *proxiesPage) applyProviders(resp *clashapi.ProvidersResp) {
	if resp == nil {
		return
	}
	p.providers = nil
	p.providerMap = map[string]clashapi.ProxyProvider{}
	for name, pr := range resp.Providers {
		if pr.VehicleType == "Compatible" {
			continue
		}
		p.providers = append(p.providers, name)
		p.providerMap[name] = pr
	}
	sort.Strings(p.providers)
}

// ---------- View ----------

func (p *proxiesPage) View(a *App) string {
	if p.showProvider {
		return p.viewProviders(a)
	}
	w := a.width - 4
	if w < 40 {
		w = 40
	}
	leftW := 30
	if w/3 < leftW {
		leftW = w / 3
	}
	rightW := w - leftW - 1
	bodyH := a.height - 7
	if bodyH < 3 {
		bodyH = 3
	}
	p.groupList.Len = len(p.groups)
	p.nodeList.Len = len(p.nodes)
	p.groupList.Height = bodyH - 2
	p.nodeList.Height = bodyH - 2

	left := p.viewGroups(leftW, bodyH)
	right := p.viewNodes(rightW, bodyH)
	footer := ""
	if p.testing {
		footer = a.spinner.View() + " 测速中…"
	} else if p.sortByDelay {
		footer = styleMuted.Render("已按延迟排序")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n " + footer
}

func (p *proxiesPage) viewGroups(w, h int) string {
	if len(p.groups) == 0 {
		return card(w, "策略组", styleMuted.Render(" 无数据 · 内核未运行或无订阅"))
	}
	var b strings.Builder
	from, to := p.groupList.Window()
	for i := from; i < to; i++ {
		name := p.groups[i]
		cur := "  "
		if i == p.groupList.Cursor {
			cur = "▶ "
		}
		now := p.groupNow[name]
		d := p.delay[now]
		row := styleAccent.Render(PadCJK(cur+TruncateCJK(name, w-14), w-12)) + " " + RenderDelay(d)
		if i == p.groupList.Cursor {
			row = styleSelItem.Render(row)
		}
		b.WriteString(row + "\n")
	}
	title := styleCardTitle.Render(fmt.Sprintf(" 策略组 (%d)", len(p.groups)))
	content := title + "\n" + strings.TrimSuffix(b.String(), "\n")
	return styleCard.Width(w).Height(h).Render(content)
}

func (p *proxiesPage) viewNodes(w, h int) string {
	if p.groupList.Cursor >= len(p.groups) || len(p.groups) == 0 {
		return styleCard.Width(w).Height(h).Render(styleCardTitle.Render(" 节点"))
	}
	group := p.groups[p.groupList.Cursor]
	gt := p.groupType[group]
	now := p.groupNow[group]
	head := fmt.Sprintf(" 节点 · %s (%d) · %s", TruncateCJK(group, max(10, w-18)), len(p.nodes), gt)
	var b strings.Builder
	if len(p.nodes) == 0 {
		b.WriteString(styleMuted.Render(" 无节点"))
	} else {
		from, to := p.nodeList.Window()
		for i := from; i < to; i++ {
			name := p.nodes[i]
			cur := "  "
			if name == now {
				cur = "● "
			}
			if i == p.nodeList.Cursor {
				cur = "▶ "
			}
			nt := PadCJK(p.nodeType[name], 13)
			line := PadCJK(cur+TruncateCJK(name, w-30), w-26) + styleMuted.Render(nt) + " " + RenderDelay(p.delay[name])
			if i == p.nodeList.Cursor {
				line = styleSelItem.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}
	return styleCard.Width(w).Height(h).Render(head + "\n" + strings.TrimSuffix(b.String(), "\n"))
}

func (p *proxiesPage) viewProviders(a *App) string {
	w := a.width - 4
	if w < 40 {
		w = 40
	}
	h := a.height - 7
	if h < 3 {
		h = 3
	}
	if len(p.providers) == 0 {
		return card(w, "Providers", styleMuted.Render(" 没有外部 Provider"))
	}
	p.providerList.Len = len(p.providers)
	p.providerList.Height = h - 2
	var b strings.Builder
	from, to := p.providerList.Window()
	for i := from; i < to; i++ {
		name := p.providers[i]
		pr := p.providerMap[name]
		cur := "  "
		if i == p.providerList.Cursor {
			cur = "▶ "
		}
		info := fmt.Sprintf("%d 节点", len(pr.Proxies))
		if pr.SubscriptionInfo != nil && pr.SubscriptionInfo.Total > 0 {
			used := pr.SubscriptionInfo.Upload + pr.SubscriptionInfo.Download
			info += fmt.Sprintf(" · %s/%s", FormatBytes(used), FormatBytes(pr.SubscriptionInfo.Total))
		}
		line := PadCJK(cur+TruncateCJK(name, w/2), w/2) +
			styleMuted.Render(PadCJK(pr.VehicleType, 10)) +
			styleMuted.Render(info)
		if i == p.providerList.Cursor {
			line = styleSelItem.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return styleCard.Width(w).Height(h).Render(
		styleCardTitle.Render(fmt.Sprintf(" Providers (%d)", len(p.providers))) + "\n" + b.String())
}
