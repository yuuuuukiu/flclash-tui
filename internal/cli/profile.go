package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"flclash-tui/internal/corecfg"
	"flclash-tui/internal/engine"
	"flclash-tui/internal/profiles"
)

var profileCmd = command{
	name:    "profile",
	summary: "订阅管理（list/add/create/edit/set/show/update/activate/remove）",
	usage: `flclash-tui profile list [--json]
flclash-tui profile add <url> [--label 名称] [--activate]
flclash-tui profile create --label 名称 [--activate] [--file x.yaml|< stdin]
flclash-tui profile edit [id] [--editor vim]     # 用 $EDITOR 编辑
flclash-tui profile set  [id] [--file x.yaml]    # 从 stdin 读入，不启动编辑器
flclash-tui profile show [id] [--raw]
flclash-tui profile update [id|--all]
flclash-tui profile activate [id]
flclash-tui profile rename <id> <新名称>
flclash-tui profile remove <id> [--yes]`,
	sub: []command{
		{name: "list", summary: "列出订阅"},
		{name: "add", summary: "添加 URL 订阅"},
		{name: "create", summary: "新建手写配置"},
		{name: "edit", summary: "用 $EDITOR 编辑配置"},
		{name: "set", summary: "从 stdin 覆盖配置"},
		{name: "show", summary: "输出订阅内容"},
		{name: "update", summary: "更新订阅"},
		{name: "activate", summary: "激活订阅"},
		{name: "rename", summary: "重命名订阅"},
		{name: "remove", summary: "删除订阅"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("profile"))
			return ExitUsage
		}
		switch args[0] {
		case "list", "ls":
			return profileList(c, args[1:])
		case "add":
			return profileAdd(c, args[1:])
		case "create", "new":
			return profileCreate(c, args[1:])
		case "edit":
			return profileEdit(c, args[1:])
		case "set":
			return profileSet(c, args[1:])
		case "show", "cat":
			return profileShow(c, args[1:])
		case "update", "up":
			return profileUpdate(c, args[1:])
		case "activate", "use":
			return profileActivate(c, args[1:])
		case "rename", "mv":
			return profileRename(c, args[1:])
		case "remove", "rm", "delete":
			return profileRemove(c, args[1:])
		}
		fmt.Fprintf(c.Err, "未知的 profile 子命令: %s\n", args[0])
		return ExitUsage
	},
}

// profileView 是 list 的 JSON 结构。
type profileView struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Label      string            `json:"label"`
	Active     bool              `json:"active"`
	URL        string            `json:"url,omitempty"`
	UpdatedAt  time.Time         `json:"updatedAt,omitzero"`
	AutoUpdate bool              `json:"autoUpdate"`
	SubInfo    *profiles.SubInfo `json:"subscriptionInfo,omitempty"`
}

func toView(p profiles.Profile) profileView {
	return profileView{
		ID: p.ID, Type: p.Type, Label: p.Label, Active: p.Active, URL: p.URL,
		UpdatedAt: p.UpdatedAt, AutoUpdate: p.AutoUpdate, SubInfo: p.SubInfo,
	}
}

func profileList(c *Ctx, argv []string) int {
	if _, err := parseNoOpts(argv); err != nil {
		return c.fail(err)
	}
	list := c.Profiles.List()
	views := make([]profileView, 0, len(list))
	for _, p := range list {
		views = append(views, toView(p))
	}
	c.emit(map[string]any{"ok": true, "profiles": views, "count": len(views)}, func() {
		if len(views) == 0 {
			c.say("还没有订阅。用 profile add <url> 或 profile create 新建。")
			return
		}
		rows := make([][]string, 0, len(views))
		for _, v := range views {
			mark := " "
			if v.Active {
				mark = "*"
			}
			rows = append(rows, []string{mark, v.ID, v.Label, v.Type, subInfoLabel(v.SubInfo), relTimeLabel(v.UpdatedAt)})
		}
		printTable(c.Out, []string{"", "ID", "名称", "类型", "流量", "更新于"}, rows)
	})
	return ExitOK
}

func subInfoLabel(info *profiles.SubInfo) string {
	if info == nil || info.Total <= 0 {
		return "—"
	}
	used := info.Upload + info.Download
	return profiles.FormatBytes(used) + "/" + profiles.FormatBytes(info.Total)
}

func relTimeLabel(t time.Time) string {
	if t.IsZero() {
		return "从未"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}

// ---------- add ----------

func profileAdd(c *Ctx, argv []string) int {
	spec := argSpec{
		boolFlags: map[string]bool{"activate": false},
		alias:     map[string]string{"a": "activate"},
		short:     map[string]string{"l": "label"},
	}
	pa, err := spec.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) == 0 {
		return c.fail(usageError("需要一个订阅 URL"))
	}
	rawURL := pa.pos[0]
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p, _, err := c.Profiles.AddFromURL(ctx, rawURL)
	if err != nil {
		return c.fail(err)
	}
	if label := pa.get("label", ""); label != "" {
		if err := c.Profiles.Rename(p.ID, label); err != nil {
			return c.fail(err)
		}
		p.Label = label
	}
	activated := false
	if pa.boolOf("activate") {
		if err := c.Profiles.SetActive(p.ID); err != nil {
			return c.fail(err)
		}
		activated = true
		if code := applyAfterChange(c); code != ExitOK {
			return code
		}
	}
	got, _ := c.Profiles.Get(p.ID)
	c.emit(map[string]any{"ok": true, "profile": toView(got), "activated": activated, "viaProxy": p.ViaProxy}, func() {
		c.say("已添加订阅「%s」 id=%s", got.Label, got.ID)
		if p.ViaProxy {
			c.say("（经代理拉取）")
		}
		if !activated {
			c.say("用 profile activate %s 激活，或 --activate 一步到位", got.ID)
		}
	})
	return ExitOK
}

// ---------- create / set ----------

func profileCreate(c *Ctx, argv []string) int {
	spec := argSpec{
		boolFlags: map[string]bool{"activate": false},
		alias:     map[string]string{"a": "activate"},
		short:     map[string]string{"l": "label", "f": "file"},
	}
	pa, err := spec.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	label := pa.get("label", "自定义配置")
	content, err := readContent(pa.get("file", ""))
	if err != nil {
		return c.fail(err)
	}
	p, normalized, err := c.Profiles.AddFromContent(label, content)
	if err != nil {
		return c.fail(err)
	}
	activated := false
	if pa.boolOf("activate") {
		if err := c.Profiles.SetActive(p.ID); err != nil {
			return c.fail(err)
		}
		activated = true
		if code := applyAfterChange(c); code != ExitOK {
			return code
		}
	}
	c.emit(map[string]any{"ok": true, "profile": toView(*p), "activated": activated, "bytes": len(normalized)}, func() {
		c.say("已创建配置「%s」 id=%s（%d 字节）", p.Label, p.ID, len(normalized))
		if !activated {
			c.say("用 profile activate %s 激活", p.ID)
		}
	})
	return ExitOK
}

func profileSet(c *Ctx, argv []string) int {
	pa, err := argSpec{short: map[string]string{"f": "file"}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	ref := ""
	if len(pa.pos) > 0 {
		ref = pa.pos[0]
	}
	p, err := pickProfile(c.Profiles, ref)
	if err != nil {
		return c.fail(err)
	}
	content, err := readContent(pa.get("file", ""))
	if err != nil {
		return c.fail(err)
	}
	normalized, err := c.Profiles.SetContent(p.ID, content)
	if err != nil {
		return c.fail(err)
	}
	applied := false
	if p.Active {
		if code := applyAfterChange(c); code != ExitOK {
			return code
		}
		applied = true
	}
	c.emit(map[string]any{"ok": true, "id": p.ID, "bytes": len(normalized), "applied": applied}, func() {
		c.say("已更新「%s」（%d 字节）", p.Label, len(normalized))
		if applied {
			c.say("已重新生成配置并重载内核")
		} else {
			c.say("该订阅未激活，内核未重载")
		}
	})
	return ExitOK
}

// ---------- edit（拉起 $EDITOR） ----------

func profileEdit(c *Ctx, argv []string) int {
	spec := argSpec{short: map[string]string{"e": "editor"}}
	pa, err := spec.parse(argv)
	if err != nil {
		return c.fail(err)
	}

	// 没有 ID 也没有激活订阅时，退化为新建
	ref := ""
	if len(pa.pos) > 0 {
		ref = pa.pos[0]
	}
	var target profiles.Profile
	creating := false
	if _, ok := c.Profiles.Active(); !ok && ref == "" {
		creating = true
	} else {
		target, err = pickProfile(c.Profiles, ref)
		if err != nil {
			return c.fail(err)
		}
	}

	if !isInteractive() {
		return c.fail(fmt.Errorf("当前 stdin 不是终端，无法启动编辑器；请改用 profile set 从 stdin 读入，或加 --editor"))
	}

	editor, err := resolveEditor(pa.get("editor", ""))
	if err != nil {
		return c.fail(err)
	}

	initial := ""
	if !creating {
		data, err := os.ReadFile(target.File())
		if err != nil {
			return c.fail(err)
		}
		initial = string(data)
	} else {
		initial = defaultSkeleton
	}

	tmp, err := os.CreateTemp("", "flclash-*.yaml")
	if err != nil {
		return c.fail(err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(initial); err != nil {
		tmp.Close()
		return c.fail(err)
	}
	tmp.Close()

	if err := runEditor(editor, tmpPath); err != nil {
		if errors.Is(err, errEditorAborted) {
			c.emit(map[string]any{"ok": true, "cancelled": true}, func() { c.say("已取消（未修改任何内容）") })
			return ExitOK
		}
		return c.fail(err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return c.fail(err)
	}
	if string(edited) == initial {
		c.emit(map[string]any{"ok": true, "changed": false, "id": target.ID}, func() { c.say("内容未变化，未做任何修改") })
		return ExitOK
	}

	label := target.Label
	if creating {
		label = pa.get("label", "自定义配置")
	}
	normalized, converted, err := corecfg.Normalize(edited)
	if err != nil {
		keep := writeRejected(tmpPath, edited)
		return c.fail(fmt.Errorf("%w（内容已保留在 %s）", err, keep))
	}

	if creating {
		p, _, err := c.Profiles.AddFromContent(label, normalized)
		if err != nil {
			return c.fail(err)
		}
		c.emit(map[string]any{"ok": true, "created": true, "id": p.ID, "converted": converted}, func() {
			c.say("已创建「%s」 id=%s", p.Label, p.ID)
			c.say("用 profile activate %s 激活", p.ID)
		})
		return ExitOK
	}

	if _, err := c.Profiles.SetContent(target.ID, normalized); err != nil {
		return c.fail(err)
	}
	applied := false
	if target.Active {
		if code := applyAfterChange(c); code != ExitOK {
			return code
		}
		applied = true
	}
	c.emit(map[string]any{"ok": true, "id": target.ID, "applied": applied, "converted": converted}, func() {
		c.say("已保存「%s」", target.Label)
		if converted {
			c.say("（JSON 已规范化为 YAML）")
		}
		if applied {
			c.say("已重新生成配置并重载内核")
		}
	})
	return ExitOK
}

var errEditorAborted = errors.New("编辑器非正常退出")

// defaultSkeleton 是新建配置时的初始模板（含注释，便于直接上手）。
const defaultSkeleton = `# FLClash TUI 手写配置（YAML 或 JSON 均可，保存后自动规范化）
proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
`

// resolveEditor 按 --editor → $FLCLASH_TUI_EDITOR → $VISUAL → $EDITOR → vi 的顺序选择编辑器。
func resolveEditor(explicit string) (string, error) {
	candidates := []string{explicit, os.Getenv("FLCLASH_TUI_EDITOR"), os.Getenv("VISUAL"), os.Getenv("EDITOR")}
	for _, cand := range candidates {
		cand = strings.TrimSpace(cand)
		if cand == "" {
			continue
		}
		fields := strings.Fields(cand)
		if len(fields) == 0 {
			continue
		}
		if _, err := exec.LookPath(fields[0]); err != nil {
			if explicit != "" {
				return "", fmt.Errorf("找不到编辑器 %q", fields[0])
			}
			continue
		}
		return cand, nil
	}
	for _, fallback := range []string{"vi", "vim", "nano", "notepad"} {
		if _, err := exec.LookPath(fallback); err == nil {
			return fallback, nil
		}
	}
	return "", fmt.Errorf("未找到可用编辑器，请设置 $EDITOR 或使用 --editor")
}

// runEditor 在终端里前台运行编辑器（继承 stdin/stdout/stderr 以保留 tty）。
func runEditor(editor, path string) error {
	fields := strings.Fields(editor)
	args := append(fields[1:], path)
	cmd := exec.Command(fields[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// vim 的 :cq / :q! 之类的非零退出码视为放弃修改
			return fmt.Errorf("%w（退出码 %d）", errEditorAborted, exitErr.ExitCode())
		}
		return err
	}
	return nil
}

// writeRejected 保存校验失败的内容，避免用户白写。
func writeRejected(orig string, content []byte) string {
	dest := strings.TrimSuffix(orig, ".yaml") + ".rejected.yaml"
	if err := os.WriteFile(dest, content, 0o600); err != nil {
		return orig
	}
	return dest
}

// isInteractive 判断 stdin 是否为终端（/dev/null 等字符设备不算）。
func isInteractive() bool {
	return term.IsTerminal(os.Stdin.Fd())
}

// readContent 从 --file 或 stdin 读取配置文本。
func readContent(path string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	if isInteractive() {
		return nil, usageError("需要配置内容：请用 --file 指定文件，或通过 stdin 管道传入")
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 64*1024*1024))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, usageError("stdin 内容为空")
	}
	return data, nil
}

// ---------- show / update / activate / rename / remove ----------

func profileShow(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"raw": false}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	ref := ""
	if len(pa.pos) > 0 {
		ref = pa.pos[0]
	}
	p, err := pickProfile(c.Profiles, ref)
	if err != nil {
		return c.fail(err)
	}
	data, err := os.ReadFile(p.File())
	if err != nil {
		return c.fail(err)
	}
	if c.JSON {
		c.emit(map[string]any{"ok": true, "profile": toView(p), "content": string(data)}, func() {})
		return ExitOK
	}
	if pa.boolOf("raw") {
		fmt.Fprint(c.Out, string(data))
		return ExitOK
	}
	fmt.Fprintf(c.Out, "# %s (id=%s, %s, %s)\n", p.Label, p.ID, p.Type, activeLabel(p.Active))
	fmt.Fprint(c.Out, string(data))
	return ExitOK
}

func activeLabel(active bool) string {
	if active {
		return "激活中"
	}
	return "未激活"
}

func profileUpdate(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"all": false}, alias: map[string]string{"a": "all"}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	var targets []profiles.Profile
	if pa.boolOf("all") {
		for _, p := range c.Profiles.List() {
			if p.Type == profiles.TypeURL {
				targets = append(targets, p)
			}
		}
	} else {
		ref := ""
		if len(pa.pos) > 0 {
			ref = pa.pos[0]
		}
		p, err := pickProfile(c.Profiles, ref)
		if err != nil {
			return c.fail(err)
		}
		targets = append(targets, p)
	}
	if len(targets) == 0 {
		return c.fail(fmt.Errorf("没有可更新的 URL 订阅"))
	}

	type result struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		OK       bool   `json:"ok"`
		Error    string `json:"error,omitempty"`
		ViaProxy bool   `json:"viaProxy"`
	}
	var results []result
	activeUpdated := false
	exitCode := ExitOK
	for _, t := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		np, err := c.Profiles.Update(ctx, t.ID)
		cancel()
		r := result{ID: t.ID, Label: t.Label, OK: err == nil}
		if err != nil {
			r.Error = err.Error()
			exitCode = ExitError
		} else {
			r.ViaProxy = np.ViaProxy
			if t.Active {
				activeUpdated = true
			}
		}
		results = append(results, r)
		if !c.JSON {
			if r.OK {
				note := ""
				if r.ViaProxy {
					note = "（经代理）"
				}
				c.say("✅ 已更新「%s」%s", r.Label, note)
			} else {
				c.say("❌ 更新「%s」失败: %s", r.Label, r.Error)
			}
		}
	}
	applied := false
	if activeUpdated && exitCode == ExitOK {
		if code := applyAfterChange(c); code != ExitOK {
			return code
		}
		applied = true
	}
	c.emit(map[string]any{"ok": exitCode == ExitOK, "results": results, "applied": applied}, func() {})
	return exitCode
}

func profileActivate(c *Ctx, argv []string) int {
	pa, err := parseNoOpts(argv)
	if err != nil {
		return c.fail(err)
	}
	ref := ""
	if len(pa.pos) > 0 {
		ref = pa.pos[0]
	}
	p, err := pickProfile(c.Profiles, ref)
	if err != nil {
		return c.fail(err)
	}
	if err := c.Profiles.SetActive(p.ID); err != nil {
		return c.fail(err)
	}
	if code := applyAfterChange(c); code != ExitOK {
		return code
	}
	c.emit(map[string]any{"ok": true, "id": p.ID, "label": p.Label}, func() {
		c.say("已激活「%s」，内核已重载", p.Label)
	})
	return ExitOK
}

func profileRename(c *Ctx, argv []string) int {
	pa, err := parseNoOpts(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) < 2 {
		return c.fail(usageError("用法: profile rename <id> <新名称>"))
	}
	p, err := pickProfile(c.Profiles, pa.pos[0])
	if err != nil {
		return c.fail(err)
	}
	label := strings.Join(pa.pos[1:], " ")
	if err := c.Profiles.Rename(p.ID, label); err != nil {
		return c.fail(err)
	}
	c.emit(map[string]any{"ok": true, "id": p.ID, "label": label}, func() {
		c.say("已重命名为「%s」", label)
	})
	return ExitOK
}

func profileRemove(c *Ctx, argv []string) int {
	pa, err := argSpec{boolFlags: map[string]bool{"yes": false}, alias: map[string]string{"y": "yes"}}.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	if len(pa.pos) == 0 {
		return c.fail(usageError("需要一个订阅 ID"))
	}
	p, err := pickProfile(c.Profiles, pa.pos[0])
	if err != nil {
		return c.fail(err)
	}
	if !pa.boolOf("yes") && isInteractive() {
		fmt.Fprintf(c.Err, "确定删除「%s」吗？[y/N] ", p.Label)
		var ans string
		_, _ = fmt.Fscanln(os.Stdin, &ans)
		if !strings.EqualFold(strings.TrimSpace(ans), "y") {
			c.say("已取消")
			return ExitOK
		}
	}
	wasActive := p.Active
	if err := c.Profiles.Delete(p.ID); err != nil {
		return c.fail(err)
	}
	applied := false
	if wasActive {
		if code := applyAfterChange(c); code != ExitOK {
			return code
		}
		applied = true
	}
	c.emit(map[string]any{"ok": true, "id": p.ID, "applied": applied}, func() {
		c.say("已删除「%s」", p.Label)
	})
	return ExitOK
}

// applyAfterChange 在配置变化后让内核重新加载。
// managed 模式下用 PUT /configs 原地热重载（不重启进程、不断连接），
// 这样「后台运行的内核」不会被 CLI 调用意外带走；控制器不可达时只写配置。
func applyAfterChange(c *Ctx) int {
	err := engine.HotReload(c.Settings, c.Profiles)
	if errors.Is(err, engine.ErrNotRunning) {
		if !c.JSON {
			c.say("（内核未运行，配置已写入，下次 core start 生效）")
		}
		return ExitOK
	}
	if err != nil {
		return c.fail(err)
	}
	if !c.JSON {
		c.say("（内核已热重载新配置）")
	}
	return ExitOK
}
