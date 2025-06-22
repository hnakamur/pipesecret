package piperpc

import (
	"context"
	"errors"
	"io"
	"log"

	"golang.org/x/exp/jsonrpc2"
)

type Server struct {
	framer  jsonrpc2.Framer
	handler jsonrpc2.Handler
}

func NewServer(framer jsonrpc2.Framer, handler jsonrpc2.Handler) *Server {
	return &Server{
		framer:  framer,
		handler: handler,
	}
}

func (c *Server) Run(ctx context.Context, in io.Reader, out io.WriteCloser) error {
	r := c.framer.Reader(in)
	w := c.framer.Writer(out)
	for {
		log.Print("server: reading")
		reqMsg, _, err := r.Read(ctx)
		if err != nil {
			return err
		}
		log.Printf("server: read message, reqMsg=%+v", reqMsg)
		req, ok := reqMsg.(*jsonrpc2.Request)
		if !ok {
			return errors.New("expected a jsonrpc2 request")
		}

		result, resultErr := c.handler.Handle(ctx, req)
		respMsg, err := jsonrpc2.NewResponse(req.ID, result, resultErr)
		if err != nil {
			return err
		}
		log.Printf("server: built message, respMsg=%+v, result=%s", respMsg, string(respMsg.Result))

		if _, err := w.Write(ctx, respMsg); err != nil {
			return err
		}
		log.Printf("server: sent message, respMsg=%+v", respMsg)
	}
}

// func (c *Server) Handle(ctx context.Context, req *jsonrpc2.Request) (any, error) {
// 	switch req.Method {
// 	case "heartbeat":
// 		return "ack", nil
// 	default:
// 		return nil, jsonrpc2.ErrNotHandled
// 	}
// }
