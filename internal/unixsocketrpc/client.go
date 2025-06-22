package unixsocketrpc

import (
	"context"
	"fmt"
	"log"
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

func (c *Client) CallSync(ctx context.Context, method string, params any) (any, jsonrpc2.ID, error) {
	call := c.conn.Call(ctx, method, params)
	log.Printf("client: created a call, id=%v", call.ID())
	var result any
	if err := call.Await(ctx, &result); err != nil {
		return nil, jsonrpc2.ID{}, fmt.Errorf("failed to wait result from unix socket: %s", err)
	}
	return result, call.ID(), nil
}
