package piperpc

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/hnakamur/pipesecret/internal/jsonrpc2debug"
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

func (c *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	logger := slog.Default().With("subcommand", "serve")

	r := c.framer.Reader(in)
	w := c.framer.Writer(out)
	for {
		logger.DebugContext(ctx, "reading message")
		reqMsg, _, err := r.Read(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.DebugContext(ctx, "piperpc.Server received EOF, exiting")
				return nil
			}
			return err
		}
		req, ok := reqMsg.(*jsonrpc2.Request)
		if !ok {
			return errors.New("expected a jsonrpc2 request")
		}
		logger.DebugContext(ctx, "pipeServer read request", "req",
			jsonrpc2debug.DebugMarshalMessage{Msg: req})

		result, resultErr := c.handler.Handle(ctx, req)
		respMsg, err := jsonrpc2.NewResponse(req.ID, result, resultErr)
		if err != nil {
			return err
		}

		if _, err := w.Write(ctx, respMsg); err != nil {
			return err
		}
		logger.DebugContext(ctx, "pipeServer written response", "resp",
			jsonrpc2debug.DebugMarshalMessage{Msg: respMsg})
	}
}
