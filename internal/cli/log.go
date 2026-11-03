package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var logCmd = command{
	name:    "log",
	summary: "内核日志流（tail/follow）",
	usage: `flclash-tui log tail [-n 50] [-f] [--level info|warning|error] [--filter 关键词] [--json]
flclash-tui log tail --duration 10s     # 采集固定时长后退出（适合脚本）`,
	sub: []command{
		{name: "tail", summary: "读取内核日志流"},
	},
	run: func(c *Ctx, args []string) int {
		if len(args) == 0 {
			printCommandHelp(c.Out, mustCommand("log"))
			return ExitUsage
		}
		switch args[0] {
		case "tail", "follow":
			return logTail(c, args[1:])
		}
		fmt.Fprintf(c.Err, "未知的 log 子命令: %s\n", args[0])
		return ExitUsage
	},
}

// logEntry 是一条内核日志。
type logEntry struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

func logTail(c *Ctx, argv []string) int {
	spec := argSpec{
		boolFlags: map[string]bool{"follow": false},
		alias:     map[string]string{"f": "follow", "n": "lines"},
		short:     map[string]string{"l": "level", "d": "duration"},
	}
	pa, err := spec.parse(argv)
	if err != nil {
		return c.fail(err)
	}
	level := pa.get("level", "info")
	switch level {
	case "info", "warning", "error", "debug":
	default:
		return c.fail(usageError("--level 只能是 info/warning/error/debug"))
	}
	n, err := pa.parseInt("lines", 0)
	if err != nil {
		return c.fail(err)
	}

	// 订阅日志流
	base := strings.TrimPrefix(c.Settings.APIBase(), "http://")
	u := "ws://" + base + "/logs?" + url.Values{"level": {level}}.Encode()
	hdr := map[string][]string{}
	if c.Settings.Secret != "" {
		hdr["Authorization"] = []string{"Bearer " + c.Settings.Secret}
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(u, hdr)
	if err != nil {
		return c.fail(fmt.Errorf("控制器不可达（%s）: %w", c.Settings.ControllerAddr, err))
	}
	defer conn.Close()

	// 只读取固定时长 / 固定条数时不需要 Ctrl-C
	var deadline <-chan time.Time
	if d := pa.get("duration", ""); d != "" {
		dur, err := time.ParseDuration(d)
		if err != nil {
			return c.fail(usageError("--duration 需要形如 10s / 1m 的时长"))
		}
		deadline = time.After(dur)
	}

	query := strings.ToLower(pa.get("filter", ""))
	emitted := 0
	started := make(chan struct{})
	go func() {
		close(started)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var e logEntry
			if json.Unmarshal(data, &e) != nil {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(e.Payload), query) {
				continue
			}
			if c.JSON {
				_ = json.NewEncoder(c.Out).Encode(e)
			} else {
				fmt.Fprintf(c.Out, "[%s] %s\n", e.Type, e.Payload)
			}
			emitted++
			if n > 0 && emitted >= n {
				conn.Close()
				return
			}
		}
	}()

	<-started
	if deadline != nil {
		<-deadline
		return ExitOK
	}
	if !isInteractive() {
		return c.fail(usageError("非交互环境请用 --duration 限定采集时长，或用 --json 配合管道"))
	}
	sig := make(chan struct{})
	waitSignal(sig)
	return ExitOK
}

// waitSignal 阻塞到收到中断信号（实现见 platform 文件）。
var waitSignal = defaultWaitSignal

func defaultWaitSignal(done chan struct{}) {
	sigCh := make(chan interface{}, 1)
	notifyInterrupt(sigCh)
	<-sigCh
	close(done)
}
