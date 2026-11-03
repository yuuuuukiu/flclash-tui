// Package coremgr 管理mihomo 内核进程的生命周期。
package coremgr

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"flclash-tui/internal/paths"
	"flclash-tui/internal/settings"
)

type State int

const (
	StateStopped State = iota
	StateStarting
	StateRunning
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return "启动中"
	case StateRunning:
		return "运行中"
	default:
		return "已停止"
	}
}

type Manager struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	logFile  *os.File
	state    State
	lastErr  string
	exited   chan struct{} // 进程退出时关闭
	exitCode int
	stopping bool // 主动停止中（用于抑制退出告警）

	secret    string
	base      string
	startTime time.Time
}

func New() *Manager {
	return &Manager{state: StateStopped}
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *Manager) LastError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

func (m *Manager) ExitCode() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exitCode
}

// PID 返回当前内核进程号（未运行时为 0）。
func (m *Manager) PID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.cmd.Process == nil {
		return 0
	}
	return m.cmd.Process.Pid
}

// StoppedIntentionally 最近一次退出是否由主动 Stop/Restart 触发。
func (m *Manager) StoppedIntentionally() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopping
}

func (m *Manager) Uptime() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != StateRunning {
		return 0
	}
	return time.Since(m.startTime)
}

// ExitedCh 返回进程退出通知通道（每次 Start 重建）。
func (m *Manager) ExitedCh() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exited
}

// Start 拉起 mihomo（managed 模式）并等待 controller 就绪。
func (m *Manager) Start(s *settings.Settings) error {
	m.mu.Lock()
	if m.state == StateRunning || m.state == StateStarting {
		m.mu.Unlock()
		return fmt.Errorf("内核已在运行")
	}
	if s.CorePath == "" {
		m.mu.Unlock()
		return fmt.Errorf("未设置 mihomo 内核路径，请在设置页配置")
	}
	if _, err := os.Stat(s.CorePath); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("内核文件不存在: %s", s.CorePath)
	}

	// 清理旧日志
	logFile, err := os.Create(paths.CoreLogFile())
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("无法创建日志文件: %w", err)
	}

	cmd := exec.Command(s.CorePath, "-d", paths.CoreDir(), "-f", paths.CoreConfigFile())
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		m.state = StateStopped
		m.lastErr = err.Error()
		m.mu.Unlock()
		return fmt.Errorf("启动内核失败: %w", err)
	}

	m.cmd = cmd
	m.logFile = logFile
	m.secret = s.Secret
	m.base = s.APIBase()
	m.state = StateStarting
	m.lastErr = ""
	m.stopping = false
	m.startTime = time.Now()
	exited := make(chan struct{})
	m.exited = exited
	pid := cmd.Process.Pid
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		logFile.Close()
		m.mu.Lock()
		m.state = StateStopped
		m.exitCode = cmd.ProcessState.ExitCode()
		if err != nil {
			m.lastErr = err.Error()
		}
		m.mu.Unlock()
		close(exited)
	}()

	_ = pid

	// 等待 controller 可用
	if !m.WaitReady(10 * time.Second) {
		return fmt.Errorf("内核已启动但控制器未就绪（查看日志: %s）", paths.CoreLogFile())
	}
	m.mu.Lock()
	m.state = StateRunning
	m.mu.Unlock()
	return nil
}

// WaitReady 轮询 /version 直到就绪或超时。
func (m *Manager) WaitReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 800 * time.Millisecond}
	for time.Now().Before(deadline) {
		m.mu.Lock()
		state := m.state
		base, secret := m.base, m.secret
		m.mu.Unlock()
		if state == StateStopped { // 进程已退出
			return false
		}
		req, err := http.NewRequest(http.MethodGet, base+"/version", nil)
		if err == nil {
			if secret != "" {
				req.Header.Set("Authorization", "Bearer "+secret)
			}
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return true
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// Stop 停止内核进程。
func (m *Manager) Stop() {
	m.mu.Lock()
	cmd := m.cmd
	m.stopping = true
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	// 等待退出
	deadline := time.Now().Add(3 * time.Second)
	for m.State() != StateStopped && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

// Restart = Stop + Start。
func (m *Manager) Restart(s *settings.Settings) error {
	m.Stop()
	return m.Start(s)
}

// ReadLogTail 返回内核日志最后 n 行（用于诊断崩溃）。
func (m *Manager) ReadLogTail(n int) []string {
	f, err := os.Open(paths.CoreLogFile())
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines
}

// PortInUse 检查端口占用（启动前预警）。
func PortInUse(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
