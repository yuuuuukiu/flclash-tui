package clashapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---------- 流消息类型 ----------

type TrafficMsg struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type MemoryMsg struct {
	Inuse   int64 `json:"inuse"`
	OSLimit int64 `json:"oslimit"`
}

type LogMsg struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// ---------- Manager ----------

// Manager 维护 4 条 WebSocket 流并在断线时自动重连。
// 每条流暴露一个带缓冲的 channel；readXXX 在 channel 满时丢弃最旧消息，
// 保证 TUI 消费慢时不会阻塞。
type Manager struct {
	base   string
	secret string

	mu         sync.Mutex
	running    bool
	cancel     context.CancelFunc
	Traffic    chan TrafficMsg
	Memory     chan MemoryMsg
	Logs       chan LogMsg
	Conns      chan ConnectionsResp
	ConnsErr   chan error
	ConnErrs   chan error
	TrafficErr chan error
}

func NewManager(base, secret string) *Manager {
	return &Manager{
		base:     base,
		secret:   secret,
		Traffic:  make(chan TrafficMsg, 16),
		Memory:   make(chan MemoryMsg, 8),
		Logs:     make(chan LogMsg, 256),
		Conns:    make(chan ConnectionsResp, 8),
		ConnsErr: make(chan error, 4),
	}
}

func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return
	}
	m.running = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.loopTraffic(ctx)
	go m.loopMemory(ctx)
	go m.loopLogs(ctx)
	go m.loopConns(ctx)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.running = false
}

func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Reconfigure 更新控制器地址与密钥（保持 channel 不变，已挂起的监听命令继续有效）。
func (m *Manager) Reconfigure(base, secret string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.base, m.secret = base, secret
}

// Restart 重启全部流（内核重载/切换控制器后调用）。
func (m *Manager) Restart() {
	m.Stop()
	// 等待旧 goroutine 退出
	time.Sleep(100 * time.Millisecond)
	m.Start()
}

func (m *Manager) dial(ctx context.Context, path string, query url.Values) (*websocket.Conn, error) {
	u := "ws://" + m.base[len("http://"):] + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	hdr := http.Header{}
	if m.secret != "" {
		hdr.Set("Authorization", "Bearer "+m.secret)
	}
	d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := d.DialContext(ctx, u, hdr)
	return conn, err
}

// runStream 循环：连接 -> 读消息 -> 断开后退避重连，直到 ctx 取消。
func runStream[T any](ctx context.Context, m *Manager, path string, query url.Values, sink func(T)) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		conn, err := m.dial(ctx, path, query)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 8*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				conn.Close()
				break
			}
			var msg T
			if json.Unmarshal(data, &msg) == nil {
				sink(msg)
			}
		}
	}
}

// dropOldestSend 发送消息，缓冲满时丢弃最旧的。
func dropOldestSend[T any](ch chan T, msg T) {
	select {
	case ch <- msg:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- msg:
		default:
		}
	}
}

func (m *Manager) loopTraffic(ctx context.Context) {
	runStream[TrafficMsg](ctx, m, "/traffic", nil, func(t TrafficMsg) {
		dropOldestSend(m.Traffic, t)
	})
}

func (m *Manager) loopMemory(ctx context.Context) {
	runStream[MemoryMsg](ctx, m, "/memory", nil, func(t MemoryMsg) {
		dropOldestSend(m.Memory, t)
	})
}

func (m *Manager) loopLogs(ctx context.Context) {
	runStream[LogMsg](ctx, m, "/logs", url.Values{"level": {"info"}}, func(l LogMsg) {
		dropOldestSend(m.Logs, l)
	})
}

func (m *Manager) loopConns(ctx context.Context) {
	runStream[ConnectionsResp](ctx, m, "/connections", nil, func(c ConnectionsResp) {
		dropOldestSend(m.Conns, c)
	})
}
