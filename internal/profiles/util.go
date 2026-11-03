package profiles

import (
	"fmt"
	"net/url"
	"time"
)

func unescape(s string) (string, error) {
	return url.PathUnescape(s)
}

// FormatBytes 字节数人性化显示。
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
	return fmt.Sprintf("%.2f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// FormatExpire 过期时间显示。
func FormatExpire(unix int64) string {
	if unix <= 0 {
		return "长期有效"
	}
	t := time.Unix(unix, 0)
	days := int(time.Until(t).Hours() / 24)
	if days < 0 {
		return "已过期"
	}
	return fmt.Sprintf("%d 天后(%s)", days, t.Format("2006-01-02"))
}
