package unixsocketrpc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"

	"golang.org/x/exp/jsonrpc2"
)

type Server struct {
	socketPath          string
	listener            jsonrpc2.Listener
	handler             jsonrpc2.Handler
	shutdownGracePeriod time.Duration
}

func Listen(ctx context.Context, socketPath string) (*Server, error) {
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to remove socket: %s", err)
	}
	listener, err := jsonrpc2.NetListener(ctx, "unix", socketPath, jsonrpc2.NetListenOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to listen unix socket: %s", err)
	}

	return &Server{
		socketPath: socketPath,
		listener:   listener,
	}, nil
}

func (s *Server) Run(ctx context.Context, handler jsonrpc2.Handler, shutdownMethod string, shutdownGracePeriod time.Duration) error {
	shutdownCh := make(chan struct{}, 1)
	wrappedHandler := func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		switch req.Method {
		case shutdownMethod:
			close(shutdownCh)
			return "", nil
		default:
			return handler.Handle(ctx, req)
		}
	}

	connOpts := jsonrpc2.ConnectionOptions{
		Handler: jsonrpc2.HandlerFunc(wrappedHandler),
	}
	server, err := jsonrpc2.Serve(ctx, s.listener, connOpts)
	if err != nil {
		log.Printf("serve error=%v", err)
		return err
	}

	var closeErr error
	go func() {
		<-shutdownCh
		<-time.After(s.shutdownGracePeriod)
		if err := s.listener.Close(); err != nil && !errors.Is(err, fs.ErrNotExist) {
			closeErr = err
			log.Printf("closeErr=%v", closeErr)
		}
	}()
	if err := server.Wait(); err != nil || closeErr != nil {
		log.Printf("waitErr=%v", err)
		return errors.Join(err, closeErr)
	}
	return nil
}
