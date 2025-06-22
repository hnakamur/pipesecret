package internal

import (
	"context"

	"github.com/hnakamur/pipesecret/internal/jsonrpc2x"
	"golang.org/x/exp/jsonrpc2"
)

type RemoteRPCServer struct {
	listener jsonrpc2.Listener
	server   *jsonrpc2.Server
}

func NewJSONRPC2ListenerLocalhostTcpRandomPort(ctx context.Context, options jsonrpc2.NetListenOptions) (jsonrpc2.Listener, error) {
	return jsonrpc2x.NetListenerTCPLocalhostRandomPort(ctx, options)
}

func getDialAddressFromListener(listener jsonrpc2.Listener) jsonrpc2x.DialAddress {
	if addresser := listener.(jsonrpc2x.DialAddresser); addresser != nil {
		return addresser.DialAddress()
	}
	return nil
}
