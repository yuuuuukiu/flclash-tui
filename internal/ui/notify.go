package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ---------- 右下角事件通知栈 ----------

// Notification 一条事件（操作结果 / 内核重要错误日志）。
type Notification struct {
	seq  int
	text string
	kind toastKind
	at   time.Time
}

const (
	notifMaxCount = 6
	notifTTLShort = 5 * time.Second  // 操作结果
	notifTTLLong  = 10 * time.Second // 错误
)

var notifStyles = map[toastKind]lipgloss.Style{
	toastInfo: styleToastInfo,
	toastOK:   styleToastOK,
	toastErr:  styleToastErr,
}

var notifIcons = map[toastKind]string{
	toastInfo: "ℹ",
	toastOK:   "✓",
	toastErr:  "✗",
}

// pushNotif 追加一条事件（右下角堆叠）；EventPopup 关闭时不显示。
// 连续相同文本只刷新时间不重复堆叠。
func (a *App) pushNotif(text string, kind toastKind) {
	if !a.Settings.EventPopup {
		return
	}
	a.notifSeq++
	// 文本自带的表情前缀由胶囊图标统一表达，避免重复
	text = strings.TrimSpace(text)
	for _, p := range []string{"✅", "❌", "ℹ"} {
		if strings.HasPrefix(text, p) {
			text = strings.TrimSpace(strings.TrimPrefix(text, p))
			break
		}
	}
	n := Notification{seq: a.notifSeq, text: text, kind: kind, at: time.Now()}
	if len(a.notifs) > 0 {
		last := &a.notifs[len(a.notifs)-1]
		if last.text == text {
			last.at = n.at
			return
		}
	}
	a.notifs = append(a.notifs, n)
	if len(a.notifs) > notifMaxCount {
		a.notifs = a.notifs[len(a.notifs)-notifMaxCount:]
	}
}

// expireNotifs 按 TTL 清理过期事件。
func (a *App) expireNotifs(now time.Time) {
	out := a.notifs[:0]
	for _, n := range a.notifs {
		ttl := notifTTLShort
		if n.kind == toastErr {
			ttl = notifTTLLong
		}
		if now.Sub(n.at) < ttl {
			out = append(out, n)
		}
	}
	a.notifs = out
}

// notifMaxWidth 单条通知最大显示宽度。
func notifMaxWidth(total int) int {
	w := total * 2 / 3
	if w > 72 {
		w = 72
	}
	if w < 24 {
		w = 24
	}
	return w
}

// notifLines 渲染通知为右对齐的胶囊行（自下而上堆叠，最新在最下）。
func (a *App) notifLines() []string {
	if len(a.notifs) == 0 {
		return nil
	}
	maxW := notifMaxWidth(a.width)
	lines := make([]string, 0, len(a.notifs))
	for _, n := range a.notifs {
		text := TruncateCJK(n.text, maxW-4)
		lines = append(lines, notifStyles[n.kind].Render(" "+notifIcons[n.kind]+" "+text+" "))
	}
	return lines
}

// overlayBottomRight 把通知胶囊覆盖到内容区右下角（自下而上堆叠）。
func overlayBottomRight(body string, pills []string, width, height int) string {
	if len(pills) == 0 || height <= 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	n := len(pills)
	for i, pill := range pills {
		row := height - n + i
		if row < 0 {
			continue
		}
		pw := lipgloss.Width(pill)
		avail := width - pw
		if avail < 0 {
			avail = 0
		}
		left := lipgloss.NewStyle().MaxWidth(avail).Render(lines[row])
		pad := avail - lipgloss.Width(left)
		if pad < 0 {
			pad = 0
		}
		lines[row] = left + strings.Repeat(" ", pad) + pill
	}
	return strings.Join(lines, "\n")
}
