package rpc

import (
	"context"
	"encoding/json"
	"log/slog"

	"fmt"
	"io"
	"sync"
	"time"

	"github.com/hnakamur/pipesecret/internal/myerrors"
	"github.com/hnakamur/pipesecret/internal/piperpc"
	"github.com/hnakamur/pipesecret/internal/unixsocketrpc"
	"golang.org/x/exp/jsonrpc2"
	"golang.org/x/xerrors"
)

type RemoteServer struct {
	socketPath        string
	framer            jsonrpc2.Framer
	requestC          chan piperpc.RequestQueueItem
	heartbeatInterval time.Duration
}

func NewRemoteServer(socketPath string, heartbeatInterval time.Duration) *RemoteServer {
	return &RemoteServer{
		socketPath:        socketPath,
		framer:            jsonrpc2.RawFramer(),
		requestC:          make(chan piperpc.RequestQueueItem, 1),
		heartbeatInterval: heartbeatInterval,
	}
}

type GetQueryItemRequestParams struct {
	Item  string
	Query string
}

const shutdownMethod = "shutdown"

func (s *RemoteServer) Run(ctx context.Context, out io.WriteCloser, in io.Reader) error {
	var unixsocketErr, pipeErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		unixsocketErr = s.runUnixSocketServer(ctx)
	}()
	go func() {
		defer wg.Done()
		pipeErr = s.runPipeClient(ctx, out, in)
	}()
	wg.Wait()

	return myerrors.Join(unixsocketErr, pipeErr)
}

func (s *RemoteServer) runUnixSocketServer(ctx context.Context) error {
	logger := slog.Default().With("program", "remote-serve")

	us, err := unixsocketrpc.Listen(ctx, s.socketPath)
	if err != nil {
		return err
	}
	logger.DebugContext(ctx, "unixSocketServer start listening", "socketPath", s.socketPath)

	handler := func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		logger.DebugContext(ctx, "handler start", "method", req.Method)
		defer func() {
			logger.DebugContext(ctx, "handler exit", "method", req.Method)
		}()
		switch req.Method {
		case "getQueryItem":
			var params GetQueryItemRequestParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, xerrors.Errorf("%w: %s", jsonrpc2.ErrParse, err)
			}
			resultC := make(chan *jsonrpc2.Response)
			s.requestC <- piperpc.RequestQueueItem{
				Request: req,
				ResultC: resultC,
			}
			select {
			case <-ctx.Done():
				logger.DebugContext(ctx, "unixSocketServer received ctx.Done", "err", ctx.Err())
				return nil, ctx.Err()
			case result := <-resultC:
				logger.DebugContext(ctx, "unixSocketServer received result", "result", result, "result.Result", string(result.Result))
				return result.Result, result.Error
			}
		default:
			return nil, jsonrpc2.ErrNotHandled
		}
	}
	shutdownGracePeriod := time.Second
	if err := us.Run(ctx, jsonrpc2.HandlerFunc(handler), shutdownMethod, shutdownGracePeriod); err != nil {
		return fmt.Errorf("failed to run server: %s", err)
	}
	return nil
}

func (s *RemoteServer) runPipeClient(ctx context.Context, out io.Writer, in io.Reader) error {
	client := piperpc.NewClient(jsonrpc2.RawFramer(), s.requestC, s.heartbeatInterval)
	return client.Run(ctx, out, in)
}
