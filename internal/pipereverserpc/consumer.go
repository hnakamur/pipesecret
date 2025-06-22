package pipereverserpc

import (
	"context"
	"errors"
	"io"
	"log"

	"golang.org/x/exp/jsonrpc2"
)

type Consumer struct {
	framer jsonrpc2.Framer
}

func NewConsumer(framer jsonrpc2.Framer) *Consumer {
	return &Consumer{
		framer: framer,
	}
}

func (c *Consumer) Run(ctx context.Context, in io.Reader, out io.WriteCloser) error {
	r := c.framer.Reader(in)
	w := c.framer.Writer(out)
	for {
		log.Print("consumer: reading")
		reqMsg, _, err := r.Read(ctx)
		if err != nil {
			return err
		}
		log.Printf("consumer: read message, reqMsg=%+v", reqMsg)
		req, ok := reqMsg.(*jsonrpc2.Request)
		if !ok {
			return errors.New("expected a jsonrpc2 request")
		}

		result, resultErr := c.Handle(ctx, req)
		respMsg, err := jsonrpc2.NewResponse(req.ID, result, resultErr)
		if err != nil {
			return err
		}
		log.Printf("consumer: built message, respMsg=%+v", respMsg)

		if _, err := w.Write(ctx, respMsg); err != nil {
			return err
		}
		log.Printf("consumer: sent message, respMsg=%+v", respMsg)
	}
}

func (c *Consumer) Handle(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	switch req.Method {
	case "heartbeat":
		return "ack", nil
	default:
		return nil, jsonrpc2.ErrNotHandled
	}
}
