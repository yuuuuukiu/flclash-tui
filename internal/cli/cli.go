// Package cli 提供 flclash-tui 的全部非交互式子命令。
//
// 设计约定：
//   - 每个子命令都对应 TUI 里的一个动作，行为一致（共用 internal/engine）；
//   - 默认输出面向人阅读的文本，加 --json 输出机器可读的 JSON；
//   - 退出码：0 成功 / 1 运行时错误 / 2 用法错误 / 3 控制器不可达。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"flclash-tui/internal/engine"
	"flclash-tui/internal/paths"
	"flclash-tui/internal/profiles"
	"flclash-tui/internal/settings"
)

const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitUnreachable = 3
)

// Ctx 是一次 CLI 调用的共享上下文。
type Ctx struct {
	Settings *settings.Settings
	Profiles *profiles.Store

	JSON  bool
	Quiet bool
	Out   io.Writer
	Err   io.Writer
}

func (c *Ctx) say(format string, a ...any) {
	if c.Quiet {
		return
	}
	fmt.Fprintf(c.Out, format+"\n", a...)
}

// emit 输出结构化结果：JSON 模式打印 JSON，否则调用 text 渲染。
func (c *Ctx) emit(v any, text func()) {
	if c.JSON {
		enc := json.NewEncoder(c.Out)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v)
		return
	}
	text()
}

// fail 打印错误并按类型返回退出码。
func (c *Ctx) fail(err error) int {
	msg := strings.TrimSpace(strings.TrimPrefix(err.Error(), "用法错误: "))
	if c.JSON {
		enc := json.NewEncoder(c.Out)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(map[string]any{"ok": false, "error": msg})
	} else {
		fmt.Fprintln(c.Err, "错误:", msg)
	}
	if errors.Is(err, errUsage) {
		return ExitUsage
	}
	return ExitError
}

var errUsage = errors.New("用法错误")

// usageError 返回一个用法错误（退出码 2）。
func usageError(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, a...))
}

// ---------- 命令注册 ----------

type command struct {
	name    string
	summary string
	usage   string
	run     func(*Ctx, []string) int
	sub     []command
}

func (c command) find(name string) (command, bool) {
	for _, s := range c.sub {
		if s.name == name {
			return s, true
		}
	}
	return command{}, false
}

var root = command{
	name: "flclash-tui",
}

// init 注册全部子命令。延迟赋值是为了避免各命令的 run 函数（引用自身帮助）
// 与变量初始化形成循环依赖。
func init() {
	root.sub = []command{coreCmd, profileCmd, proxyCmd, connCmd, ruleCmd, logCmd, configCmd, settingsCmd, sysproxyCmd, versionCmd, helpCmd}
}

// mustCommand 按名字取子命令（供各命令打印自身帮助）。
func mustCommand(name string) command {
	if c, ok := root.find(name); ok {
		return c
	}
	return command{name: name}
}

// Run 执行 CLI，返回进程退出码。
func Run(args []string) int {
	ctx := &Ctx{Out: os.Stdout, Err: os.Stderr}

	// 先摘出全局开关，其余交给子命令解析
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json" || a == "-j":
			ctx.JSON = true
		case a == "--quiet" || a == "-q":
			ctx.Quiet = true
		case a == "--home":
			if i+1 >= len(args) {
				fmt.Fprintln(ctx.Err, "错误: --home 需要一个目录参数")
				return ExitUsage
			}
			i++
			os.Setenv("FLCLASH_TUI_HOME", args[i])
		case strings.HasPrefix(a, "--home="):
			os.Setenv("FLCLASH_TUI_HOME", strings.TrimPrefix(a, "--home="))
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 {
		printRootHelp(ctx.Out)
		return ExitUsage
	}

	cmd, ok := root.find(rest[0])
	if !ok {
		fmt.Fprintf(ctx.Err, "未知子命令: %s\n\n", rest[0])
		printRootHelp(ctx.Err)
		return ExitUsage
	}

	s, ps, err := engine.Load()
	if err != nil {
		fmt.Fprintln(ctx.Err, "初始化失败:", err.Error())
		return ExitError
	}
	ctx.Settings, ctx.Profiles = s, ps
	engine.ApplyFetchConfig(s, ps)

	code := cmd.run(ctx, rest[1:])
	return code
}

// ---------- 通用输出工具 ----------

// kvList 打印对齐的键值对。
func kvList(w io.Writer, rows [][2]string) {
	width := 0
	for _, r := range rows {
		if n := displayWidth(r[0]); n > width {
			width = n
		}
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%s  %s\n", padRight(r[0], width), r[1])
	}
}

// displayWidth 粗略计算显示宽度（CJK 记 2）。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWideRune(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0x303E,
		r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6:
		return true
	}
	return false
}

// printTable 打印简单表格（首行为表头）。
func printTable(w io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = displayWidth(h)
	}
	for _, r := range rows {
		for i := range header {
			if i < len(r) {
				if n := displayWidth(r[i]); n > widths[i] {
					widths[i] = n
				}
			}
		}
	}
	line := func(cells []string) {
		var b strings.Builder
		for i := range header {
			v := ""
			if i < len(cells) {
				v = cells[i]
			}
			b.WriteString(padRight(v, widths[i]))
			if i != len(header)-1 {
				b.WriteString("  ")
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	line(header)
	for _, r := range rows {
		line(r)
	}
}

func padRight(s string, width int) string {
	if n := displayWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// pickProfile 解析 id 参数：为空时取激活订阅，否则按 ID 前缀/名称匹配。
func pickProfile(ps *profiles.Store, ref string) (profiles.Profile, error) {
	list := ps.List()
	if strings.TrimSpace(ref) == "" {
		if p, ok := ps.Active(); ok {
			return p, nil
		}
		return profiles.Profile{}, errors.New("没有激活的订阅，请指定订阅 ID")
	}
	ref = strings.TrimSpace(ref)
	// 1) 精确 ID
	if p, ok := ps.Get(ref); ok {
		return p, nil
	}
	// 2) ID 前缀
	var byPrefix []profiles.Profile
	for _, p := range list {
		if strings.HasPrefix(p.ID, ref) {
			byPrefix = append(byPrefix, p)
		}
	}
	if len(byPrefix) == 1 {
		return byPrefix[0], nil
	}
	if len(byPrefix) > 1 {
		return profiles.Profile{}, fmt.Errorf("ID 前缀 %q 匹配到 %d 个订阅，请写得更具体", ref, len(byPrefix))
	}
	// 3) 名称
	var byLabel []profiles.Profile
	for _, p := range list {
		if p.Label == ref {
			byLabel = append(byLabel, p)
		}
	}
	if len(byLabel) == 1 {
		return byLabel[0], nil
	}
	if len(byLabel) > 1 {
		return profiles.Profile{}, fmt.Errorf("名称 %q 对应多个订阅，请改用 ID", ref)
	}
	return profiles.Profile{}, fmt.Errorf("找不到订阅: %s", ref)
}

// ---------- 根帮助 ----------

func printRootHelp(w io.Writer) {
	fmt.Fprintf(w, `FLClash TUI —— mihomo 终端客户端（内置内核）

用法:
  flclash-tui                         启动交互式 TUI
  flclash-tui <子命令> [参数] [--json] 非交互式调用

全局参数:
  --json, -j     以 JSON 输出结果（便于脚本处理）
  --quiet, -q    静默模式（只输出结果与错误）
  --home <dir>   指定数据目录（等价于 FLCLASH_TUI_HOME）

子命令:
`)
	for _, c := range root.sub {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, `
示例:
  flclash-tui profile add 'https://example.com/sub?token=x' --activate
  flclash-tui profile create --label 我的配置 < my.yaml
  flclash-tui proxy select '🚀 节点选择' 香港01
  flclash-tui core start --daemon && flclash-tui core status
  flclash-tui conn list --json | jq '.connections[].metadata.host'

数据目录: %s
`, paths.Home())
}

var helpCmd = command{
	name:    "help",
	summary: "显示帮助",
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printRootHelp(c.Out)
			return ExitOK
		}
		cmd, ok := root.find(args[0])
		if !ok {
			fmt.Fprintf(c.Err, "未知子命令: %s\n", args[0])
			return ExitUsage
		}
		printCommandHelp(c.Out, cmd)
		return ExitOK
	},
}

func printCommandHelp(w io.Writer, cmd command) {
	fmt.Fprintf(w, "%s —— %s\n\n", cmd.name, cmd.summary)
	if cmd.usage != "" {
		fmt.Fprintf(w, "用法:\n  %s\n\n", strings.ReplaceAll(cmd.usage, "\n", "\n  "))
	}
	if len(cmd.sub) > 0 {
		fmt.Fprintln(w, "子命令:")
		for _, s := range cmd.sub {
			fmt.Fprintf(w, "  %-12s %s\n", s.name, s.summary)
		}
	}
}

var versionCmd = command{
	name:    "version",
	summary: "显示版本信息",
	run: func(c *Ctx, args []string) int {
		return runVersion(c)
	},
}
