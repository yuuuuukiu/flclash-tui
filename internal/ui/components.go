package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------- Sparkline（迷你趋势图）----------

var barRunes = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline 渲染数值序列，返回 width 宽的单行字符串。
func Sparkline(values []int64, width int, maxV int64, color lipgloss.Color) string {
	if width <= 0 {
		return ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	out := make([]rune, 0, width)
	// 左侧留空对齐
	for i := 0; i < width-len(values); i++ {
		out = append(out, ' ')
	}
	var peak int64
	for _, v := range values {
		if v > peak {
			peak = v
		}
	}
	if maxV > 0 {
		peak = maxV
	}
	for _, v := range values {
		idx := 0
		if peak > 0 {
			idx = int(float64(v) / float64(peak) * float64(len(barRunes)-1))
			if idx >= len(barRunes) {
				idx = len(barRunes) - 1
			}
		}
		out = append(out, barRunes[idx])
	}
	style := lipgloss.NewStyle().Foreground(color)
	return style.Render(string(out))
}

// ---------- 进度条 ----------

func ProgressBar(used, total int64, width int) string {
	if total <= 0 || used < 0 {
		return styleMuted.Render("░" + strings.Repeat("░", width-1))
	}
	pct := float64(used) / float64(total)
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	style := styleGreen
	if pct > 0.85 {
		style = styleRed
	} else if pct > 0.65 {
		style = styleYellow
	}
	return style.Render(strings.Repeat("█", filled)) + styleMuted.Render(strings.Repeat("░", width-filled))
}

// ---------- 通用列表窗口 ----------

// ListView 管理光标与窗口滚动，不负责渲染具体行。
type ListView struct {
	Cursor int
	Offset int
	Len    int // 由调用方每次更新
	Height int // 可视行数
}

func (l *ListView) Clamp() {
	if l.Len <= 0 {
		l.Cursor, l.Offset = 0, 0
		return
	}
	if l.Cursor >= l.Len {
		l.Cursor = l.Len - 1
	}
	if l.Cursor < 0 {
		l.Cursor = 0
	}
	if l.Height <= 0 {
		l.Height = 10
	}
	if l.Cursor < l.Offset {
		l.Offset = l.Cursor
	}
	if l.Cursor >= l.Offset+l.Height {
		l.Offset = l.Cursor - l.Height + 1
	}
	if l.Offset > l.Len-l.Height {
		l.Offset = l.Len - l.Height
	}
	if l.Offset < 0 {
		l.Offset = 0
	}
}

func (l *ListView) Up()     { l.Cursor--; l.Clamp() }
func (l *ListView) Down()   { l.Cursor++; l.Clamp() }
func (l *ListView) Top()    { l.Cursor = 0; l.Clamp() }
func (l *ListView) Bottom() { l.Cursor = l.Len - 1; l.Clamp() }
func (l *ListView) PageUp() {
	l.Cursor -= l.Height
	l.Clamp()
}
func (l *ListView) PageDown() {
	l.Cursor += l.Height
	l.Clamp()
}

// Window 返回 [from,to) 渲染区间。
func (l *ListView) Window() (int, int) {
	l.Clamp()
	from := l.Offset
	to := l.Offset + l.Height
	if to > l.Len {
		to = l.Len
	}
	return from, to
}
