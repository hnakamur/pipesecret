package piperpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"golang.org/x/exp/jsonrpc2"
)

type Client struct {
	framer            jsonrpc2.Framer
	requestC          <-chan RequestQueueItem
	heartbeatInterval time.Duration
}

type RequestQueueItem struct {
	Request *jsonrpc2.Request
	ResultC chan *jsonrpc2.Response
}

func NewClient(framer jsonrpc2.Framer, requestC <-chan RequestQueueItem, heartbeatInterval time.Duration) *Client {
	return &Client{
		framer:            framer,
		requestC:          requestC,
		heartbeatInterval: heartbeatInterval,
	}
}

func (c *Client) Run(ctx context.Context, out io.Writer, in io.Reader) error {
	logger := slog.Default().With("program", "remote-serve")

	w := c.framer.Writer(out)
	r := c.framer.Reader(in)
	for {
		select {
		case <-ctx.Done():
			logger.DebugContext(ctx, "pipeClient received ctx.Done, exiting")
			return nil
		case <-time.After(c.heartbeatInterval):
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
		case origReq := <-c.requestC:
			origReqID := origReq.Request.ID
			reqID, err := uuid.NewRandom()
			if err != nil {
				return err
			}
			// We need to create a new request instead of reusing origReq.request here.
			req := &jsonrpc2.Request{
				ID:     jsonrpc2.StringID(reqID.String()),
				Method: origReq.Request.Method,
				Params: origReq.Request.Params,
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
			origReq.ResultC <- resp
			logger.DebugContext(ctx, "pipeClient esnt response to resultC")
		}
	}
}
