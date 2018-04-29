package server

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // anonymous import to get the pprof handler registered
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	
	"github.com/pinterb/common/signals"

	"github.com/go-kit/kit/metrics/prometheus"
	"github.com/pkg/errors"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/go-kit/kit/log"
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


// New makes a new Server
func New(cfg Config, logger Logger) (*Server, error) {
	// Setup listeners first, so we can fail early if the port is in use.
	httpListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.HTTPListenPort))
	if err != nil {
		return nil, err
	}

	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCListenPort))
	if err != nil {
		return nil, err
	}

	grpcOptions := []grpc.ServerOption{}
	grpcOptions = append(grpcOptions, cfg.GRPCOptions...)
	grpcServer := grpc.NewServer(grpcOptions...)

	// Setup HTTP server
	router := mux.NewRouter()
	if cfg.RegisterInstrumentation {
		RegisterInstrumentation(router)
	}

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

	// Setup gRPC server
	// for HTTP over gRPC, ensure we don't double-count the middleware
	httpgrpc.RegisterHTTPServer(s.GRPC, httpgrpc_server.NewServer(s.HTTP))
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