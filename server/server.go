package server

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // anonymous import to get the pprof handler registered
	"time"

	"github.com/gorilla/mux"
	"github.com/pinterb/common/middleware"
	"github.com/pinterb/common/signals"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

// Config for a Server
type Config struct {
	MetricsNamespace string
	HTTPListenPort   int
	GRPCListenPort   int

	RegisterInstrumentation bool
	ExcludeRequestInLog     bool

	ServerGracefulShutdownTimeout time.Duration
	HTTPServerReadTimeout         time.Duration
	HTTPServerWriteTimeout        time.Duration
	HTTPServerIdleTimeout         time.Duration

	GRPCOptions    []grpc.ServerOption
	GRPCMiddleware []grpc.UnaryServerInterceptor
	HTTPMiddleware []middleware.Interface
}

// Logger is something to log too.
type Logger interface {
	Log(keyvals ...interface{}) error
}

// Server wraps a HTTP and gRPC server, and some common initialization.
//
// Servers will be automatically instrumented for Prometheus metrics.
type Server struct {
	cfg          Config
	handler      *signals.Handler
	httpListener net.Listener
	grpcListener net.Listener
	httpServer   *http.Server

	HTTP *mux.Router
	GRPC *grpc.Server
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.HTTPListenPort, "server.http-listen-port", 80, "HTTP server listen port.")
	f.IntVar(&cfg.GRPCListenPort, "server.grpc-listen-port", 9095, "gRPC server listen port.")
	f.BoolVar(&cfg.RegisterInstrumentation, "server.register-instrumentation", true, "Register the intrumentation handlers (/metrics etc).")
	f.DurationVar(&cfg.ServerGracefulShutdownTimeout, "server.graceful-shutdown-timeout", 5*time.Second, "Timeout for graceful shutdowns")
	f.DurationVar(&cfg.HTTPServerReadTimeout, "server.http-read-timeout", 5*time.Second, "Read timeout for HTTP server")
	f.DurationVar(&cfg.HTTPServerWriteTimeout, "server.http-write-timeout", 5*time.Second, "Write timeout for HTTP server")
	f.DurationVar(&cfg.HTTPServerIdleTimeout, "server.http-idle-timeout", 120*time.Second, "Idle timeout for HTTP server")
}

// New makes a new Server
func New(cfg Config, logger Logger) (*Server, error) {
	// Setup listeners first, so we can fail early if the port is in use.
	httpListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.HTTPListenPort))
	if err != nil {
		return nil, errors.Wrap(err, "New Server")
	}

	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCListenPort))
	if err != nil {
		return nil, errors.Wrap(err, "New Server")
	}

	grpcOptions := []grpc.ServerOption{}
	grpcOptions = append(grpcOptions, cfg.GRPCOptions...)
	grpcServer := grpc.NewServer(grpcOptions...)

	// Setup HTTP server
	router := mux.NewRouter()
	if cfg.RegisterInstrumentation {
		RegisterInstrumentation(router)
	}

	httpMiddleware := []middleware.Interface{}
	httpMiddleware = append(httpMiddleware, cfg.HTTPMiddleware...)
	httpServer := &http.Server{
		ReadTimeout:  cfg.HTTPServerReadTimeout,
		WriteTimeout: cfg.HTTPServerWriteTimeout,
		IdleTimeout:  cfg.HTTPServerIdleTimeout,
		Handler:      middleware.Merge(httpMiddleware...).Wrap(router),
	}

	return &Server{
		cfg:          cfg,
		httpListener: httpListener,
		grpcListener: grpcListener,
		httpServer:   httpServer,
		handler:      signals.NewHandler(logger),

		HTTP: router,
		GRPC: grpcServer,
	}, nil

}

// RegisterInstrumentation on the given router.
func RegisterInstrumentation(router *mux.Router) {
	router.Handle("/metrics", prometheus.Handler())
	router.PathPrefix("/debug/pprof").Handler(http.DefaultServeMux)
}

// Run the server; blocks until SIGTERM is received.
func (s *Server) Run() {
	go s.httpServer.Serve(s.httpListener)

	go s.GRPC.Serve(s.grpcListener)
	defer s.GRPC.GracefulStop()

	// Wait for a signal
	s.handler.Loop()
}

// Stop unblocks Run().
func (s *Server) Stop() {
	s.handler.Stop()
}

// Shutdown the server, gracefully.  Should be defered after New().
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ServerGracefulShutdownTimeout)
	defer cancel() // releases resources if httpServer.Shutdown completes before timeout elapses

	s.httpServer.Shutdown(ctx)
	s.GRPC.Stop()
}
