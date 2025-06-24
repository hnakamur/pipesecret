package unixsocketrpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"golang.org/x/exp/jsonrpc2"
)

type Client struct {
	conn *jsonrpc2.Connection
}

func Connect(ctx context.Context, socketPath string, timeout time.Duration) (*Client, error) {
	dialer := jsonrpc2.NetDialer("unix", socketPath, net.Dialer{
		Timeout: timeout,
	})
	conn, err := jsonrpc2.Dial(ctx, dialer, jsonrpc2.ConnectionOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to dial to unix socket: %s", err)
	}
	return &Client{
		conn: conn,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) CallSync(ctx context.Context, method string, params any) (string, jsonrpc2.ID, error) {
	logger := slog.Default().With("program", "unixSocketClient")

	call := c.conn.Call(ctx, method, params)
	logger.DebugContext(ctx, "client: created a call", "id", call.ID())
	var result string
	if err := call.Await(ctx, &result); err != nil {
		return "", jsonrpc2.ID{}, fmt.Errorf("failed to wait result from unix socket: %s", err)
	}
	logger.DebugContext(ctx, "client: received response for a call", "id", call.ID(), "result", result)
	return result, call.ID(), nil
}
