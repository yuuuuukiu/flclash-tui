//go:build windows

package cli

import (
	"os"
	"os/signal"
)

// notifyInterrupt 把 Ctrl-C 转发到 ch。
func notifyInterrupt(ch chan interface{}) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		s := <-sig
		ch <- s
	}()
}
