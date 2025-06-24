package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hnakamur/pipesecret/internal/unixsocketrpc"
	"golang.org/x/xerrors"
)

func GetQueryItem(ctx context.Context, socketPath string, timeout time.Duration, itemName, query string) (any, error) {
	logger := slog.Default().With("program", "unixSocketClient")
	logger.DebugContext(ctx, "GetQueryItem", "socketPath", socketPath)

	client, err := unixsocketrpc.Connect(ctx, socketPath, timeout)
	if err != nil {
		return nil, xerrors.Errorf("failed to connect unix socket server: %s", err)
	}
	defer client.Close()

	params := GetQueryItemRequestParams{
		Item:  itemName,
		Query: query,
	}
	resultJSON, _, err := client.CallSync(ctx, "getQueryItem", params)
	if err != nil {
		return nil, xerrors.Errorf("failed to call getQueryItem: %s", err)
	}

	var resultObj any
	if err := json.Unmarshal([]byte(resultJSON), &resultObj); err != nil {

	}
	return resultObj, nil
}
