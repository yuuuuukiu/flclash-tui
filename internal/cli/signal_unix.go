//go:build !windows

package cli

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyInterrupt 把 SIGINT/SIGTERM 转发到 ch。
func notifyInterrupt(ch chan interface{}) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-sig
		ch <- s
	}()
}
