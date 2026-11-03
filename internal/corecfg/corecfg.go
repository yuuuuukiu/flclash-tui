// Package corecfg 复刻 FLClash 的配置生成逻辑（lib/common/task.dart _makeRealProfileTask）：
// 订阅原始 YAML + 应用设置 -> 合并 patch -> 最终喂给 mihomo 的 config.yaml。
package corecfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"flclash-tui/internal/settings"
)

// Normalize 把手写的配置文本（YAML 或 JSON）规范化为 YAML。
// JSON 是 YAML 的子集，正常情况下直接可解析；yaml.Node 往返可以保留注释与键序。
// 返回值 converted 表示输入是 JSON（用于提示用户）。
func Normalize(content []byte) (out []byte, converted bool, err error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return nil, false, fmt.Errorf("内容为空")
	}
	var node yaml.Node
	if err := yaml.Unmarshal(trimmed, &node); err != nil {
		// 回退到纯 JSON 解析（兼容 YAML 解析器不接受的写法，如制表符缩进）
		var m map[string]any
		if jerr := json.Unmarshal(trimmed, &m); jerr != nil {
			return nil, false, fmt.Errorf("既不是合法 YAML 也不是合法 JSON（%v）", err)
		}
		out, err = yaml.Marshal(m)
		return out, true, err
	}
	if len(node.Content) == 0 {
		return nil, false, fmt.Errorf("空文档")
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("配置根节点必须是映射（如 {proxies: ...}），当前为 %s", nodeKindName(root.Kind))
	}
	// 清除 JSON 来源的表现样式（flow / 引号），输出地道的块状 YAML；
	// 引号只保留给语义需要的标量（emitter 会按 tag 自动重新加引号）
	clearJSONPresentation(root)
	out, err = yaml.Marshal(&node)
	return out, trimmed[0] == '{', err
}

func clearJSONPresentation(n *yaml.Node) {
	if n == nil {
		return
	}
	n.Style &^= yaml.FlowStyle | yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle
	for _, c := range n.Content {
		clearJSONPresentation(c)
	}
}

func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "文档"
	case yaml.SequenceNode:
		return "序列（[]）"
	case yaml.ScalarNode:
		return "标量"
	case yaml.AliasNode:
		return "别名"
	}
	return "未知"
}

// Generate 把订阅原始配置（可为 nil，表示无订阅空跑）与设置合并为最终内核配置。
func Generate(raw map[string]any, s *settings.Settings) (map[string]any, error) {
	if raw == nil {
		raw = map[string]any{}
	}
	out := map[string]any{}
	for k, v := range raw {
		out[k] = v
	}

	// ---- external controller（对齐 FLClash：只暴露 REST，禁用其他形态）----
	out["external-controller"] = s.ControllerAddr
	out["secret"] = s.Secret
	out["external-controller-tls"] = ""
	out["external-controller-unix"] = ""
	out["external-controller-pipe"] = ""
	out["external-ui"] = ""
	out["external-ui-url"] = ""

	// ---- 入站端口 ----
	out["mixed-port"] = s.MixedPort
	out["port"] = 0
	out["socks-port"] = 0
	out["redir-port"] = 0
	out["tproxy-port"] = 0
	out["allow-lan"] = s.AllowLan

	// ---- 常规 ----
	out["mode"] = s.Mode
	out["log-level"] = s.LogLevel
	out["ipv6"] = s.IPv6
	out["unified-delay"] = s.UnifiedDelay
	out["tcp-concurrent"] = s.TCPConcurrent
	out["find-process-mode"] = "strict"
	out["global-ua"] = "clash.meta"

	// ---- profile：选择状态由 TUI 自己管理 ----
	profile, _ := out["profile"].(map[string]any)
	if profile == nil {
		profile = map[string]any{}
	}
	profile["store-selected"] = false
	out["profile"] = profile

	// ---- TUN（对齐 FLClash：dns-hijack / auto-route / auto-detect-interface 强制覆写）----
	tun, _ := out["tun"].(map[string]any)
	if tun == nil {
		tun = map[string]any{}
	}
	tun["enable"] = s.Tun
	tun["stack"] = s.TunStack
	tun["auto-route"] = true
	tun["auto-detect-interface"] = true
	tun["dns-hijack"] = []any{"any:53"}
	out["tun"] = tun

	// ---- DNS 基线（对齐 FLClash：内核永远不能没有 DNS）----
	dnsEnabled := false
	if m, ok := out["dns"].(map[string]any); ok {
		if b, ok := m["enable"].(bool); ok {
			dnsEnabled = b
		}
	}
	if !dnsEnabled {
		out["dns"] = map[string]any{
			"enable":         true,
			"enhanced-mode":  "fake-ip",
			"fake-ip-range":  "198.18.0.1/16",
			"fake-ip-filter": []any{"*.lan", "*.local", "*.localdomain"},
			"nameserver":     []any{"https://doh.pub/dns-query", "https://dns.alidns.com/dns-query"},
			"respect-rules":  false,
		}
	}

	// ---- 无订阅时补一个最小拓扑，保证内核可启动 ----
	if _, ok := out["proxies"]; !ok {
		if _, ok := out["proxy-providers"]; !ok {
			out["proxies"] = []any{}
		}
	}
	if _, ok := out["rules"]; !ok {
		out["rules"] = []any{"MATCH,DIRECT"}
	}

	// ---- 订阅拉取回退专用监听：绑定到当前主出口（组），代理内请求强制走它 ----
	if s.ProxyFallback && s.MixedPort < 65535 {
		if target := pickMainOutbound(out); target != "" {
			listeners, _ := out["listeners"].([]any)
			kept := listeners[:0]
			for _, l := range listeners {
				if m, ok := l.(map[string]any); ok && m["name"] == fallbackListenerName {
					continue
				}
				kept = append(kept, l)
			}
			out["listeners"] = append(kept, map[string]any{
				"name":  fallbackListenerName,
				"type":  "mixed",
				"port":  s.MixedPort + 1,
				"proxy": target,
			})
		}
	}

	return out, nil
}

const fallbackListenerName = "flt-sub-fallback"

// pickMainOutbound 找出"当前正在使用的代理"对应的组/节点名：
// 优先 MATCH 规则的出口（非 DIRECT/REJECT），否则第一个 Selector 组，再否则第一个组/代理。
func pickMainOutbound(cfg map[string]any) string {
	if rules, ok := cfg["rules"].([]any); ok {
		for i := len(rules) - 1; i >= 0; i-- {
			r, ok := rules[i].(string)
			if !ok || !strings.HasPrefix(r, "MATCH,") {
				continue
			}
			name := strings.TrimSpace(strings.TrimPrefix(r, "MATCH,"))
			if name != "" && name != "DIRECT" && name != "REJECT" {
				return name
			}
			break
		}
	}
	if groups, ok := cfg["proxy-groups"].([]any); ok {
		var firstAny string
		for _, g := range groups {
			m, ok := g.(map[string]any)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			if firstAny == "" {
				firstAny = name
			}
			if t, _ := m["type"].(string); t == "select" || t == "" {
				return name
			}
		}
		if firstAny != "" {
			return firstAny
		}
	}
	if proxies, ok := cfg["proxies"].([]any); ok && len(proxies) > 0 {
		if m, ok := proxies[0].(map[string]any); ok {
			if n, _ := m["name"].(string); n != "" {
				return n
			}
		}
	}
	return ""
}

// ParseYAML 解析订阅 YAML 为 map。
func ParseYAML(data []byte) (map[string]any, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("订阅 YAML 解析失败: %w", err)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return raw, nil
}

// Marshal 把最终配置序列化为 YAML。
func Marshal(cfg map[string]any) ([]byte, error) {
	return yaml.Marshal(cfg)
}

// Validate 做基本结构校验（proxies / proxy-groups / rules 类型检查由内核完成，
// 这里只挡住明显不可用的输入）。
func Validate(raw map[string]any) error {
	if len(raw) == 0 {
		return fmt.Errorf("空配置")
	}
	if _, ok := raw["proxies"]; ok {
		if _, ok := raw["proxies"].([]any); !ok {
			return fmt.Errorf("proxies 字段类型错误")
		}
	}
	return nil
}
