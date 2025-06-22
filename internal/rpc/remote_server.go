package rpc

import (
	"context"
	"encoding/json"
	"errors"

	"fmt"
	"io"
	"log"
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
	us, err := unixsocketrpc.Listen(ctx, s.socketPath)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		defer func() {
			log.Printf("remoteServer handler exiting, method=%s", req.Method)
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
				log.Printf("unixSocketServer ctx.Done, ctx.Err=%v", ctx.Err())
				return nil, ctx.Err()
			case result := <-resultC:
				log.Printf("unix socket server received result=%#v, result.Result=%s", result, string(result.Result))
				if result.Error != nil {
					log.Printf("result.Error=%v", result.Error)
				}
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
	w := s.framer.Writer(out)
	r := s.framer.Reader(in)
	for {
		select {
		case <-ctx.Done():
			return out.Close()
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
			log.Printf("client: sent heartbeat request ID=%v", req.ID)
			respMsg, _, err := r.Read(ctx)
			if err != nil {
				return err
			}
			resp, ok := respMsg.(*jsonrpc2.Response)
			if !ok {
				return errors.New("expected a jsonrpc2 response")
			}
			log.Printf("client: received heartbeat resp=%#v", resp)
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
			log.Printf("client: sending request ID=%v, origReqID=%v", req.ID, origReqID)
			if _, err := w.Write(ctx, req); err != nil {
				return err
			}
			log.Printf("client: sent request ID=%v", req.ID)

			respMsg, _, err := r.Read(ctx)
			if err != nil {
				return err
			}
			resp, ok := respMsg.(*jsonrpc2.Response)
			if !ok {
				return errors.New("expected a jsonrpc2 response")
			}
			log.Printf("client: received resp=%#v", resp)
			resp.ID = origReqID
			origReq.resultC <- resp
			log.Printf("client: sent response to resultC")
		}
	}
}
