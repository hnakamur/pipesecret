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

func NewRemoteServer(socketPath string) *RemoteServer {
	return &RemoteServer{
		socketPath:        socketPath,
		framer:            jsonrpc2.RawFramer(),
		requestC:          make(chan secretQueryRequst, 1),
		heartbeatInterval: 5 * time.Second,
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
				return nil, ctx.Err()
			case result := <-resultC:
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
		case req := <-s.requestC:
			origReqID := req.request.ID
			reqID, err := uuid.NewRandom()
			if err != nil {
				return err
			}
			req.request.ID = jsonrpc2.StringID(reqID.String())
			// pipeReq, err := jsonrpc2.NewCall(jsonrpc2.StringID(reqID.String()),
			// 	"getQueryItem", req.params)
			// if err != nil {
			// 	return err
			// }
			log.Printf("client: sending request ID=%v, origReqID=%v", req.request.ID, origReqID)
			if _, err := w.Write(ctx, req.request); err != nil {
				return err
			}
			log.Printf("client: sent request ID=%v", req.request.ID)

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
			req.resultC <- resp
			// if resp.Result != nil {
			// 	var result string
			// 	if err := json.Unmarshal(resp.Result, &result); err != nil {
			// 		return err
			// 	}
			// 	log.Printf("client: received result=%+v (%s)", result, string(resp.Result))
			// 	req.resultC <- secretQueryResponse{
			// 		result: result,
			// 	}
			// }
		}
	}
}
