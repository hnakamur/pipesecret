// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonrpc2x

import (
	"context"
	"io"
	"net"
	"os"
	"time"

	"golang.org/x/exp/jsonrpc2"
)

// This file contains implementations of the transport primitives that use the standard network
// package.

type DialAddresser interface {
	DialAddress() DialAddress
}

type DialAddress interface {
	Network() string
	Address() string
}

// NetListenerTCPLocalhostRandomPort returns a new Listener that listens on a localhost TCP socket
// with a random port.
//
// The returned listener implements DialAddresser interface.
func NetListenerTCPLocalhostRandomPort(ctx context.Context, options jsonrpc2.NetListenOptions) (jsonrpc2.Listener, error) {
	return NetListener(ctx, "tcp", "localhost:0", options)
}

// NetListener returns a new Listener that listens on a socket using the net package.
func NetListener(ctx context.Context, network, address string, options jsonrpc2.NetListenOptions) (jsonrpc2.Listener, error) {
	ln, err := options.NetListenConfig.Listen(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &netListener{net: ln}, nil
}

// netListener is the implementation of Listener for connections made using the net package.
//
// This is slightly modified version of netListner in
// golang.org/x/exp/jsonrpc2 v0.0.0-20250620022241-b7579e27df2b
type netListener struct {
	net net.Listener
}

// Accept blocks waiting for an incoming connection to the listener.
func (l *netListener) Accept(ctx context.Context) (io.ReadWriteCloser, error) {
	return l.net.Accept()
}

// Close will cause the listener to stop listening. It will not close any connections that have
// already been accepted.
func (l *netListener) Close() error {
	addr := l.net.Addr()
	err := l.net.Close()
	if addr.Network() == "unix" {
		rerr := os.Remove(addr.String())
		if rerr != nil && err == nil {
			err = rerr
		}
	}
	return err
}

// Dialer returns a dialer that can be used to connect to the listener.
func (l *netListener) Dialer() jsonrpc2.Dialer {
	return jsonrpc2.NetDialer(l.net.Addr().Network(), l.net.Addr().String(), net.Dialer{
		Timeout: 5 * time.Second,
	})
}

func (l *netListener) DialAddress() DialAddress {
	return &dialAddress{
		network: l.net.Addr().Network(),
		address: l.net.Addr().String(),
	}
}

type dialAddress struct {
	network string
	address string
}

func (a *dialAddress) Network() string { return a.network }
func (a *dialAddress) Address() string { return a.address }
