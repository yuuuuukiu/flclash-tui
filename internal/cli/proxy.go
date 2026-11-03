package cli

import (
	"context"
	"fmt"
	"sort"
	"time"

	"flclash-tui/internal/clashapi"
	"flclash-tui/internal/engine"
)

var proxyCmd = command{
	name:    "proxy",
	summary: "代理与策略组（list/groups/select/delay/provider）",
	usage: `flclash-tui proxy groups [--json]
flclash-tui proxy list <组名|--all> [--json]
flclash-tui proxy select <组名> <节点名>
flclash-tui proxy delay [节点名] [--group 组名] [--url u] [--timeout ms]
flclash-tui proxy provider list|update|check [名称]`,
	sub: []command{
		{name: "groups", summary: "列出策略组"},
		{name: "list", summary: "列出组内节点"},
		{name: "select", summary: "切换节点"},
		{name: "delay", summary: "测速"},
		{name: "provider", summary: "Provider 管理"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("proxy"))
			return ExitUsage
		}
		switch args[0] {
		case "groups", "ls":
			return proxyGroups(c, args[1:])
		case "list", "nodes":
			return proxyList(c, args[1:])
		case "select", "use":
			return proxySelect(c, args[1:])
		case "delay", "test":
			return proxyDelay(c, args[1:])
		case "provider", "providers":
			return proxyProvider(c, args[1:])
		}
		fmt.Fprintf(c.Err, "未知的 proxy 子命令: %s\n", args[0])
		return ExitUsage
	},
}

var groupTypeSet = map[string]bool{
	"Selector": true, "URLTest": true, "Fallback": true,
	"LoadBalance": true, "Relay": true, "Smart": true,
}

type groupView struct {
	Name  string   `json:"name"`
	Type  string   `json:"type"`
	Now   string   `json:"now"`
	Nodes []string `json:"nodes"`
	Delay int      `json:"delay"`
}

// fetchProxies 拉取 /proxies，控制器不可达时给出明确退出码。
func fetchProxies(c *Ctx) (*clashapi.ProxiesResp, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := engine.Client(c.Settings).GetProxies(ctx)
	if err != nil {
		return nil, c.fail(fmt.Errorf("控制器不可达（%s）: %w", c.Settings.ControllerAddr, err))
	}
	return resp, ExitOK
}

func collectGroups(resp *clashapi.ProxiesResp) []groupView {
	out := make([]groupView, 0, 8)
	for name, p := range resp.Proxies {
		if !groupTypeSet[p.Type] {
			continue
		}
		delay := 0
		if len(p.History) > 0 {
			delay = p.History[len(p.History)-1].Delay
		}
		out = append(out, groupView{Name: name, Type: p.Type, Now: p.Now, Nodes: p.All, Delay: delay})
	}
	sort.Slice(out, func(i, j int) bool {
		// GLOBAL 排最后，其余按名称
		if (out[i].Name == "GLOBAL") != (out[j].Name == "GLOBAL") {
			return out[j].Name == "GLOBAL"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func proxyGroups(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	resp, code := fetchProxies(c)
	if resp == nil {
		return code
	}
	groups := collectGroups(resp)
	c.emit(map[string]any{"ok": true, "groups": groups, "count": len(groups)}, func() {
		if len(groups) == 0 {
			c.say("没有策略组")
			return
		}
		rows := make([][]string, 0, len(groups))
		for _, g := range groups {
			rows = append(rows, []string{g.Name, g.Type, g.Now, fmt.Sprintf("%d", len(g.Nodes)), delayLabel(g.Delay)})
		}
		printTable(c.Out, []string{"组名", "类型", "当前节点", "节点数", "延迟"}, rows)
	})
	return ExitOK
}

func delayLabel(ms int) string {
	if ms <= 0 {
		return "—"
	}
	return fmt.Sprintf("%dms", ms)
}

func proxyList(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"all": false}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	resp, code := fetchProxies(c)
	if resp == nil {
		return code
	}
	if pa.boolOf("all") {
		type nodeView struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Delay int    `json:"delay"`
		}
		nodes := make([]nodeView, 0, len(resp.Proxies))
		for name, p := range resp.Proxies {
			if groupTypeSet[p.Type] {
				continue
			}
			d := 0
			if len(p.History) > 0 {
				d = p.History[len(p.History)-1].Delay
			}
			nodes = append(nodes, nodeView{Name: name, Type: p.Type, Delay: d})
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		c.emit(map[string]any{"ok": true, "proxies": nodes, "count": len(nodes)}, func() {
			rows := make([][]string, 0, len(nodes))
			for _, n := range nodes {
				rows = append(rows, []string{n.Name, n.Type, delayLabel(n.Delay)})
			}
			printTable(c.Out, []string{"节点", "类型", "延迟"}, rows)
		})
		return ExitOK
	}

	if len(pa.pos) == 0 {
		return c.fail(usageError("需要一个组名（或用 --all 列出全部节点）"))
	}
	group := pa.pos[0]
	p, ok := resp.Proxies[group]
	if !ok {
		return c.fail(fmt.Errorf("找不到策略组: %s", group))
	}
	type nodeView struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Delay   int    `json:"delay"`
		Current bool   `json:"current"`
	}
	nodes := make([]nodeView, 0, len(p.All))
	for _, n := range p.All {
		d := 0
		nt := ""
		if np, ok := resp.Proxies[n]; ok {
			nt = np.Type
			if len(np.History) > 0 {
				d = np.History[len(np.History)-1].Delay
			}
		}
		nodes = append(nodes, nodeView{Name: n, Type: nt, Delay: d, Current: n == p.Now})
	}
	c.emit(map[string]any{"ok": true, "group": group, "type": p.Type, "now": p.Now, "nodes": nodes}, func() {
		c.say("组: %s (%s) 当前: %s", group, p.Type, p.Now)
		rows := make([][]string, 0, len(nodes))
		for _, n := range nodes {
			mark := " "
			if n.Current {
				mark = "*"
			}
			rows = append(rows, []string{mark, n.Name, n.Type, delayLabel(n.Delay)})
		}
		printTable(c.Out, []string{"", "节点", "类型", "延迟"}, rows)
	})
	return ExitOK
}

func proxySelect(c *Ctx, argv []string) int {
	pa, err := parseNoOpts(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) < 2 {
		return c.fail(usageError("用法: proxy select <组名> <节点名>"))
	}
	group := pa.pos[0]
	name := pa.pos[1]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Client(c.Settings).SelectProxy(ctx, group, name); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true, "group": group, "name": name}, func() {
		c.say("已切换 %s → %s", group, name)
	})
	return ExitOK
}

func proxyDelay(c *Ctx, argv []string) int {
	spec := argSpec{
		boolFlags: map[string]bool{"all": false},
		short:     map[string]string{"u": "url", "t": "timeout", "g": "group"},
	}
	pa, err := spec.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	testURL := pa.get("url", c.Settings.TestURL)
	timeout, err := pa.parseInt("timeout", c.Settings.DelayTimeout)
	if err != nil {
		return c.fail(err)
	}
	client := engine.Client(c.Settings)
	client.TestURL = testURL
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+5000)*time.Millisecond)
	defer cancel()

	// 组测速
	if group := pa.get("group", ""); group != "" {
		results, err := client.GroupDelay(ctx, group, testURL, timeout)
		if err != nil {
			return c.fail(err)
		}
		type item struct {
			Name  string `json:"name"`
			Delay int    `json:"delay"`
		}
		items := make([]item, 0, len(results))
		for n, d := range results {
			items = append(items, item{Name: n, Delay: d})
		}
		sort.Slice(items, func(i, j int) bool {
			if (items[i].Delay <= 0) != (items[j].Delay <= 0) {
				return items[j].Delay <= 0
			}
			if items[i].Delay != items[j].Delay {
				return items[i].Delay < items[j].Delay
			}
			return items[i].Name < items[j].Name
		})
		c.emit(map[string]any{"ok": true, "group": group, "results": items}, func() {
			rows := make([][]string, 0, len(items))
			for _, it := range items {
				rows = append(rows, []string{it.Name, delayLabel(it.Delay)})
			}
			printTable(c.Out, []string{"节点", "延迟"}, rows)
		})
		return ExitOK
	}

	if len(pa.pos) == 0 {
		return c.fail(usageError("需要节点名，或用 --group 组名 做整组测速"))
	}
	name := pa.pos[0]
	d, err := client.ProxyDelay(ctx, name, testURL, timeout)
	if err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true, "name": name, "delay": d}, func() {
		c.say("%s: %dms", name, d)
	})
	return ExitOK
}

func proxyProvider(c *Ctx, argv []string) int {
	if len(argv) == 0 {
		return c.fail(usageError("用法: proxy provider list|update|check [名称]"))
	}
	action := argv[0]
	pa, err := parseNoOpts(argv[1:])
	if err != nil {
		return c.fail(err)
	}
	client := engine.Client(c.Settings)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	switch action {
	case "list", "ls":
		resp, err := client.GetProviders(ctx)
		if err != nil {
			return c.fail(err)
		}
		names := make([]string, 0, len(resp.Providers))
		for n, p := range resp.Providers {
			if p.VehicleType == "Compatible" {
				continue
			}
			names = append(names, n)
		}
		sort.Strings(names)
		type provView struct {
			Name        string   `json:"name"`
			VehicleType string   `json:"vehicleType"`
			NodeCount   int      `json:"nodeCount"`
			UpdatedAt   string   `json:"updatedAt"`
			TestURL     string   `json:"testUrl"`
			Nodes       []string `json:"nodes,omitempty"`
		}
		views := make([]provView, 0, len(names))
		for _, n := range names {
			p := resp.Providers[n]
			v := provView{Name: n, VehicleType: p.VehicleType, NodeCount: len(p.Proxies), UpdatedAt: p.UpdatedAt, TestURL: p.TestUrl}
			for _, node := range p.Proxies {
				v.Nodes = append(v.Nodes, node.Name)
			}
			views = append(views, v)
		}
		c.emit(map[string]any{"ok": true, "providers": views}, func() {
			if len(views) == 0 {
				c.say("没有外部 Provider")
				return
			}
			rows := make([][]string, 0, len(views))
			for _, v := range views {
				rows = append(rows, []string{v.Name, v.VehicleType, fmt.Sprintf("%d", v.NodeCount), v.UpdatedAt})
			}
			printTable(c.Out, []string{"名称", "类型", "节点数", "更新时间"}, rows)
		})
		return ExitOK
	case "update", "up":
		if len(pa.pos) == 0 {
			return c.fail(usageError("需要一个 Provider 名称"))
		}
		if err := client.UpdateProvider(ctx, pa.pos[0]); err != nil {
			return c.fail(err)
		}
		c.emit(map[string]any{"ok": true, "name": pa.pos[0]}, func() {
			c.say("已更新 Provider %s", pa.pos[0])
		})
		return ExitOK
	case "check", "healthcheck":
		if len(pa.pos) == 0 {
			return c.fail(usageError("需要一个 Provider 名称"))
		}
		if err := client.HealthCheckProvider(ctx, pa.pos[0]); err != nil {
			return c.fail(err)
		}
		c.emit(map[string]any{"ok": true, "name": pa.pos[0]}, func() {
			c.say("已完成健康检查 %s", pa.pos[0])
		})
		return ExitOK
	}
	return c.fail(usageError("未知的 provider 动作: %s", action))
}
