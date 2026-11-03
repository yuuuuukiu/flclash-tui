package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"flclash-tui/internal/clashapi"
	"flclash-tui/internal/engine"
)

var connCmd = command{
	name:    "conn",
	summary: "连接管理（list/close/close-all）",
	usage: `flclash-tui conn list [--filter 关键词] [--sort time|traffic] [--limit 50] [--json]
flclash-tui conn close <连接ID>
flclash-tui conn close-all`,
	sub: []command{
		{name: "list", summary: "列出连接"},
		{name: "close", summary: "关闭单个连接"},
		{name: "close-all", summary: "关闭全部连接"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("conn"))
			return ExitUsage
		}
		switch args[0] {
		case "list", "ls":
			return connList(c, args[1:])
		case "close", "kill":
			return connClose(c, args[1:])
		case "close-all", "closeall":
			return connCloseAll(c, args[1:])
		}
		fmt.Fprintf(c.Err, "未知的 conn 子命令: %s\n", args[0])
		return ExitUsage
	},
}

func connList(c *Ctx, argv []string) int {
	spec := argSpec{short: map[string]string{"l": "limit", "f": "filter", "s": "sort"}}
	pa, err := spec.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	limit, err := pa.parseInt("limit", 0)
	if err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := engine.Client(c.Settings).GetConnections(ctx)
	if err != nil {
		return c.fail(fmt.Errorf("控制器不可达（%s）: %w", c.Settings.ControllerAddr, err))
	}

	conns := append([]clashapi.Connection(nil), resp.Connections...)
	if q := strings.ToLower(pa.get("filter", "")); q != "" {
		filtered := conns[:0]
		for _, cn := range conns {
			if strings.Contains(strings.ToLower(connHost(cn)), q) ||
				strings.Contains(strings.ToLower(cn.Metadata.Process), q) ||
				strings.Contains(strings.ToLower(cn.Rule+cn.RulePayload), q) ||
				strings.Contains(strings.ToLower(strings.Join(cn.Chains, ">")), q) {
				filtered = append(filtered, cn)
			}
		}
		conns = filtered
	}
	switch pa.get("sort", "time") {
	case "traffic":
		sort.SliceStable(conns, func(i, j int) bool {
			return conns[i].Upload+conns[i].Download > conns[j].Upload+conns[j].Download
		})
	default:
		sort.SliceStable(conns, func(i, j int) bool { return conns[i].Start.After(conns[j].Start) })
	}
	if limit > 0 && len(conns) > limit {
		conns = conns[:limit]
	}

	type connView struct {
		ID       string   `json:"id"`
		Host     string   `json:"host"`
		Port     string   `json:"port"`
		Network  string   `json:"network"`
		Rule     string   `json:"rule"`
		Payload  string   `json:"rulePayload"`
		Chains   []string `json:"chains"`
		Upload   int64    `json:"upload"`
		Download int64    `json:"download"`
		Start    string   `json:"start"`
		Process  string   `json:"process,omitempty"`
		SourceIP string   `json:"sourceIP,omitempty"`
	}
	views := make([]connView, 0, len(conns))
	for _, cn := range conns {
		views = append(views, connView{
			ID: cn.ID, Host: connHost(cn), Port: cn.Metadata.DestinationPort, Network: cn.Metadata.Network,
			Rule: cn.Rule, Payload: cn.RulePayload, Chains: cn.Chains,
			Upload: cn.Upload, Download: cn.Download, Start: cn.Start.Format(time.RFC3339),
			Process: cn.Metadata.Process, SourceIP: cn.Metadata.SourceIP,
		})
	}
	c.emit(map[string]any{
		"ok": true, "count": len(views),
		"uploadTotal": resp.UploadTotal, "downloadTotal": resp.DownloadTotal, "memory": resp.Memory,
		"connections": views,
	}, func() {
		c.say("连接 %d 个  累计 ↑%s ↓%s  内存 %s", len(views),
			humanBytes(resp.UploadTotal), humanBytes(resp.DownloadTotal), humanBytes(resp.Memory))
		if len(views) == 0 {
			return
		}
		rows := make([][]string, 0, len(views))
		for _, v := range views {
			chain := reverseChain(v.Chains)
			rows = append(rows, []string{
				v.Host + ":" + v.Port, v.Network, v.Rule, chain,
				"↑" + humanBytes(v.Upload), "↓" + humanBytes(v.Download), v.Process,
			})
		}
		printTable(c.Out, []string{"主机", "网络", "规则", "链路", "上行", "下行", "进程"}, rows)
	})
	return ExitOK
}

func connHost(cn clashapi.Connection) string {
	if cn.Metadata.Host != "" {
		return cn.Metadata.Host
	}
	return cn.Metadata.DestinationIP
}

func reverseChain(chains []string) string {
	rev := make([]string, len(chains))
	for i, s := range chains {
		rev[len(chains)-1-i] = s
	}
	return strings.Join(rev, "→")
}

func connClose(c *Ctx, argv []string) int {
	pa, err := parseNoOpts(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) == 0 {
		return c.fail(usageError("需要一个连接 ID（用 conn list --json 获取）"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Client(c.Settings).CloseConnection(ctx, pa.pos[0]); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true, "id": pa.pos[0]}, func() {
		c.say("已关闭连接 %s", pa.pos[0])
	})
	return ExitOK
}

func connCloseAll(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Client(c.Settings).CloseAllConnections(ctx); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true}, func() { c.say("已关闭全部连接") })
	return ExitOK
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
