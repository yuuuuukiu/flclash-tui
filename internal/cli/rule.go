package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"flclash-tui/internal/engine"
)

var ruleCmd = command{
	name:    "rule",
	summary: "规则（list/update）",
	usage: `flclash-tui rule list [--filter 关键词] [--json]
flclash-tui rule update <rule-provider 名称>`,
	sub: []command{
		{name: "list", summary: "列出规则"},
		{name: "update", summary: "更新 rule provider"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("rule"))
			return ExitUsage
		}
		switch args[0] {
		case "list", "ls":
			return ruleList(c, args[1:])
		case "update":
			return ruleUpdate(c, args[1:])
		}
		fmt.Fprintf(c.Err, "未知的 rule 子命令: %s\n", args[0])
		return ExitUsage
	},
}

func ruleList(c *Ctx, argv []string) int {
	pa, err := argSpec{short: map[string]string{"f": "filter"}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := engine.Client(c.Settings).GetRules(ctx)
	if err != nil {
		return c.fail(fmt.Errorf("控制器不可达（%s）: %w", c.Settings.ControllerAddr, err))
	}
	rules := resp.Rules
	if q := strings.ToLower(pa.get("filter", "")); q != "" {
		filtered := rules[:0]
		for _, r := range rules {
			if strings.Contains(strings.ToLower(r.Payload), q) ||
				strings.Contains(strings.ToLower(r.Type), q) ||
				strings.Contains(strings.ToLower(r.Proxy), q) {
				filtered = append(filtered, r)
			}
		}
		rules = filtered
	}
	type ruleView struct {
		Type    string `json:"type"`
		Payload string `json:"payload"`
		Proxy   string `json:"proxy"`
	}
	views := make([]ruleView, 0, len(rules))
	for _, r := range rules {
		views = append(views, ruleView{Type: r.Type, Payload: r.Payload, Proxy: r.Proxy})
	}
	c.emit(map[string]any{"ok": true, "count": len(views), "rules": views}, func() {
		if len(views) == 0 {
			c.say("没有匹配的规则")
			return
		}
		rows := make([][]string, 0, len(views))
		for _, v := range views {
			rows = append(rows, []string{v.Type, v.Payload, v.Proxy})
		}
		printTable(c.Out, []string{"类型", "匹配内容", "出口"}, rows)
	})
	return ExitOK
}

func ruleUpdate(c *Ctx, argv []string) int {
	pa, err := parseNoOpts(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) == 0 {
		return c.fail(usageError("需要一个 rule provider 名称"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := engine.Client(c.Settings).UpdateRuleProvider(ctx, pa.pos[0]); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true, "name": pa.pos[0]}, func() {
		c.say("已更新 rule provider %s", pa.pos[0])
	})
	return ExitOK
}
