package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------- 颜色 ----------

var (
	colorAccent = lipgloss.Color("39") // 蓝
	colorGreen  = lipgloss.Color("42")
	colorYellow = lipgloss.Color("214")
	colorRed    = lipgloss.Color("204")
	colorMuted  = lipgloss.Color("244")
	colorFg     = lipgloss.Color("252")
	colorBorder = lipgloss.Color("238")
	colorSelBg  = lipgloss.Color("236")
	colorPurple = lipgloss.Color("141")
)

// ---------- 基础样式 ----------

var (
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))

	styleTab       = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	styleTabActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("24")).Padding(0, 1)

	styleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	styleCardTitle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleValue  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	styleAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	styleGreen  = lipgloss.NewStyle().Foreground(colorGreen)
	styleYellow = lipgloss.NewStyle().Foreground(colorYellow)
	styleRed    = lipgloss.NewStyle().Foreground(colorRed)
	stylePurple = lipgloss.NewStyle().Foreground(colorPurple)

	styleSelItem = lipgloss.NewStyle().Background(colorSelBg)

	styleStatusBar = lipgloss.NewStyle().Foreground(colorMuted)

	styleToastInfo = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("24")).Bold(true).Padding(0, 1)
	styleToastOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("28")).Bold(true).Padding(0, 1)
	styleToastErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("124")).Bold(true).Padding(0, 1)

	styleHelpKey  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleHelpDesc = lipgloss.NewStyle().Foreground(colorFg)

	styleModal = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2).
			Background(lipgloss.Color("235"))
)

// ---------- 帮助函数 ----------

func delayStyle(ms int) lipgloss.Style {
	switch {
	case ms <= 0:
		return styleMuted
	case ms < 200:
		return styleGreen
	case ms < 500:
		return styleYellow
	default:
		return styleRed
	}
}

// RenderDelay 渲染延迟单元格。
func RenderDelay(ms int) string {
	if ms <= 0 {
		return styleMuted.Render(" --  ")
	}
	return delayStyle(ms).Render(fmt.Sprintf("%4dms", ms))
}

func FormatSpeed(bps int64) string {
	const unit = 1024
	if bps < unit {
		return fmt.Sprintf("%d B/s", bps)
	}
	div, exp := int64(unit), 0
	for m := bps / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB/s", float64(bps)/float64(div), "KMGTPE"[exp])
}

func FormatBytes(n int64) string {
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

func HumanDuration(d interface{ Seconds() float64 }) string {
	secs := int(d.Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	min := secs / 60
	if min < 60 {
		return fmt.Sprintf("%dm%02ds", min, secs%60)
	}
	h := min / 60
	if h < 24 {
		return fmt.Sprintf("%dh%02dm", h, min%60)
	}
	return fmt.Sprintf("%dd%02dh", h/24, h%24)
}

// TruncateCJK 截断字符串到最大显示宽度（考虑中文双宽）。
func TruncateCJK(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	w := 0
	for i, r := range s {
		rw := 1
		if r > 0x1100 && (r <= 0x9FFF || r >= 0xAC00 || (r >= 0x2E80 && r <= 0x9FFF)) {
			// 粗略 CJK 判定
			if isWide(r) {
				rw = 2
			}
		}
		if w+rw > maxW-1 {
			return s[:i] + "…"
		}
		w += rw
	}
	return s
}

func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x303E, // CJK 部首、符号
		r >= 0x3041 && r <= 0x33FF, // 假名、CJK 符号
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF, // CJK 统一表意
		r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3, // 谚文
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60, // 全角
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD:
		return true
	}
	return false
}

// PadCJK 右侧补空格到显示宽度。
func PadCJK(s string, width int) string {
	w := lipgloss.Width(s)
	if w > width {
		return TruncateCJK(s, width)
	}
	return s + strings.Repeat(" ", width-w)
}
