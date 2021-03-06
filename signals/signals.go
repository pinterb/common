package signals

import (
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

// SignalReceiver represents a subsystem/server/... that can be stopped or
// queried about the status with a signal
type SignalReceiver interface {
	Stop() error
}

// Logger is something to log too.
type Logger interface {
	Log(keyvals ...interface{}) error
}

// Handler handles signals, can be interrupted.
// On SIGINT or SIGTERM it will exit, on SIGQUIT it
// will dump goroutine stacks to the Logger.
type Handler struct {
	log       Logger
	receivers []SignalReceiver
	quit      chan struct{}
}

// NewHandler makes a new Handler.
func NewHandler(log Logger, receivers ...SignalReceiver) *Handler {
	return &Handler{
		log:       log,
		receivers: receivers,
		quit:      make(chan struct{}),
	}
}

// Stop the handler
func (h *Handler) Stop() {
	close(h.quit)
}

// Loop handles signals.
func (h *Handler) Loop() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	buf := make([]byte, 1<<20)
	for {
		select {
		case <-h.quit:
			h.log.Log("sighandler", "=== Handler.Stop()'d ===")
			return
		case sig := <-sigs:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				h.log.Log("sighandler", "=== received SIGINT/SIGTERM ===")
				for _, subsystem := range h.receivers {
					subsystem.Stop()
				}
				return
			case syscall.SIGQUIT:
				stacklen := runtime.Stack(buf, true)
				h.log.Log("sighandler", "=== received SIGQUIT ===")
				h.log.Log("sighandler", "*** goroutine dump...start ***")
				h.log.Log("sighandler", string(buf[:stacklen]))
				h.log.Log("sighandler", "*** goroutine dump...end ***")
			}
		}
	}
}

// SignalHandlerLoop blocks until it receives a SIGINT, SIGTERM or SIGQUIT.
// For SIGINT and SIGTERM, it exits; for SIGQUIT is print a goroutine stack
// dump.
func SignalHandlerLoop(log Logger, ss ...SignalReceiver) {
	NewHandler(log, ss...).Loop()
}
