package piperpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"time"

	"github.com/google/uuid"
	"golang.org/x/exp/jsonrpc2"
)

type Client struct {
	framer            jsonrpc2.Framer
	heartbeatInterval time.Duration
}

func NewClient(framer jsonrpc2.Framer, heartbeatInterval time.Duration) *Client {
	return &Client{
		framer:            framer,
		heartbeatInterval: heartbeatInterval,
	}
}

func (p *Client) Run(ctx context.Context, out io.WriteCloser, in io.Reader) error {
	w := p.framer.Writer(out)
	r := p.framer.Reader(in)
	for {
		reqID, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		req, err := jsonrpc2.NewCall(jsonrpc2.StringID(reqID.String()), "heartbeat", nil)
		if err != nil {
			return err
		}
		log.Printf("client: sending request ID=%s", reqID)
		if _, err := w.Write(ctx, req); err != nil {
			return err
		}
		log.Printf("client: sent request ID=%s", reqID)

		respMsg, _, err := r.Read(ctx)
		if err != nil {
			return err
		}
		resp, ok := respMsg.(*jsonrpc2.Response)
		if !ok {
			return errors.New("expected a jsonrpc2 response")
		}
		log.Printf("client: received resp=%#v", resp)
		if resp.Result != nil {
			var result any
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				return err
			}
			log.Printf("client: received result=%+v (%s)", result, string(resp.Result))
		}

		select {
		case <-ctx.Done():
			return out.Close()
		case <-time.After(p.heartbeatInterval):
		}
	}
}
