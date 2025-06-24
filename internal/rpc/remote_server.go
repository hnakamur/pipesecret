package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"

	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hnakamur/pipesecret/internal/unixsocketrpc"
	"golang.org/x/exp/jsonrpc2"
	"golang.org/x/xerrors"
)

type RemoteServer struct {
	socketPath        string
	framer            jsonrpc2.Framer
	requestC          chan secretQueryRequst
	heartbeatInterval time.Duration
}

type secretQueryRequst struct {
	request *jsonrpc2.Request
	resultC chan *jsonrpc2.Response
}

func NewRemoteServer(socketPath string, heartbeatInterval time.Duration) *RemoteServer {
	return &RemoteServer{
		socketPath:        socketPath,
		framer:            jsonrpc2.RawFramer(),
		requestC:          make(chan secretQueryRequst, 1),
		heartbeatInterval: heartbeatInterval,
	}
}

type GetQueryItemRequestParams struct {
	Item  string
	Query string
}

const shutdownMethod = "shutdown"

var RemoteServerLogger = sync.OnceValue(func() *slog.Logger {
	file, err := os.OpenFile("/tmp/pipesecret-remote-server-err.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		panic(err)
	}
	return slog.New(slog.NewTextHandler(file, nil))
})

func (s *RemoteServer) Run(ctx context.Context, out io.WriteCloser, in io.Reader) error {
	RemoteServerLogger().Info("run start")

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

	if unixsocketErr != nil || pipeErr != nil {
		RemoteServerLogger().Error("RemoteServer got error", "unixsocketErr", unixsocketErr, "pipeErr", pipeErr)
	}

	if unixsocketErr != nil && pipeErr != nil {
		return errors.Join(unixsocketErr, pipeErr)
	} else if unixsocketErr != nil {
		return unixsocketErr
	} else if pipeErr != nil {
		return pipeErr
	}
	return nil
}

func (s *RemoteServer) runUnixSocketServer(ctx context.Context) error {
	RemoteServerLogger().Info("runUnixSocketServer start")
	defer func() {
		RemoteServerLogger().Info("runUnixSocketServer exit")
	}()

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
			s.requestC <- secretQueryRequst{
				request: req,
				resultC: resultC,
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

func (s *RemoteServer) runPipeClient(ctx context.Context, out io.WriteCloser, in io.Reader) error {
	logger := slog.Default().With("program", "remote-serve")
	RemoteServerLogger().Info("runPipeClient start")
	defer func() {
		RemoteServerLogger().Info("runPipeClient exit")
	}()

	w := s.framer.Writer(out)
	r := s.framer.Reader(in)
	for {
		select {
		case <-ctx.Done():
			logger.DebugContext(ctx, "pipeClient received ctx.Done, exiting")
			err := out.Close()
			RemoteServerLogger().Error("closed out", "err", err)
			return err
		case <-time.After(s.heartbeatInterval):
			reqID, err := uuid.NewRandom()
			if err != nil {
				return err
			}
			req := &jsonrpc2.Request{
				ID:     jsonrpc2.StringID(reqID.String()),
				Method: "heartbeat",
			}
			if _, err := w.Write(ctx, req); err != nil {
				return err
			}
			logger.DebugContext(ctx, "pipeClient sent heartbeat request", "reqID", req.ID)
			respMsg, _, err := r.Read(ctx)
			if err != nil {
				return err
			}
			resp, ok := respMsg.(*jsonrpc2.Response)
			if !ok {
				return errors.New("expected a jsonrpc2 response")
			}
			logger.DebugContext(ctx, "pipeClient received heartbeat request", "resp", resp)
		case origReq := <-s.requestC:
			origReqID := origReq.request.ID
			reqID, err := uuid.NewRandom()
			if err != nil {
				return err
			}
			// We need to create a new request instead of reusing origReq.request here.
			req := &jsonrpc2.Request{
				ID:     jsonrpc2.StringID(reqID.String()),
				Method: origReq.request.Method,
				Params: origReq.request.Params,
			}
			logger.DebugContext(ctx, "pipeClient writing request", "reqID", req.ID, "origReqID", origReqID)
			if _, err := w.Write(ctx, req); err != nil {
				return err
			}
			logger.DebugContext(ctx, "pipeClient written request", "reqID", req.ID, "origReqID", origReqID)

			respMsg, _, err := r.Read(ctx)
			if err != nil {
				return err
			}
			resp, ok := respMsg.(*jsonrpc2.Response)
			if !ok {
				return errors.New("expected a jsonrpc2 response")
			}
			logger.DebugContext(ctx, "pipeClient received response", "resp", resp)
			resp.ID = origReqID
			origReq.resultC <- resp
			logger.DebugContext(ctx, "pipeClient esnt response to resultC")
		}
	}
}
