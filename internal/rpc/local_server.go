package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"

	"github.com/hnakamur/pipesecret/internal"
	"github.com/hnakamur/pipesecret/internal/piperpc"
	"golang.org/x/exp/jsonrpc2"
	"golang.org/x/xerrors"
)

func RunLocalServer(ctx context.Context, sshPath, host, remoteCommand, opExePath string) error {
	logger := slog.Default().With("subcommand", "serve")

	cmd := exec.Command(sshPath, host, remoteCommand)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	exitC := make(chan os.Signal, 1)
	signal.Notify(exitC, os.Interrupt)
	go func() {
		<-exitC
		logger.DebugContext(ctx, "got interrupt signal")
		if err := cmd.Process.Kill(); err != nil {
			logger.ErrorContext(ctx, "failed to kill remote-serve process", "err", err)
		} else {
			logger.DebugContext(ctx, "killed remote-serve process")
		}
		cancel()
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logger.DebugContext(ctx, "stderr from remote-serve", "line", line)
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, fs.ErrClosed) {
			logger.ErrorContext(ctx, "failed to read remote-serve stderr", "err", err)
		}
	}()

	handler := func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		switch req.Method {
		case "getQueryItem":
			var params GetQueryItemRequestParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, xerrors.Errorf("%w: %s", jsonrpc2.ErrParse, err)
			}
			getter, err := internal.NewOnePasswordItemGetter(opExePath)
			if err != nil {
				return nil, xerrors.Errorf("%w: %s", jsonrpc2.ErrInternal, err)
			}
			result, err := internal.GetQueryItem(ctx, getter, params.Item, params.Query)
			if err != nil {
				return nil, xerrors.Errorf("%w: %s", jsonrpc2.ErrInvalidRequest, err)
			}
			return result, nil
		case "heartbeat":
			return "ack", nil
		default:
			return nil, jsonrpc2.ErrNotHandled
		}
	}

	server := piperpc.NewServer(jsonrpc2.RawFramer(), jsonrpc2.HandlerFunc(handler))
	localErr := server.Run(ctx, stdout, stdin)
	if localErr != nil {
		logger.ErrorContext(ctx, "got error from local server", "localErr", localErr)
	} else {
		logger.DebugContext(ctx, "after server.Run")
	}

	remoteErr := cmd.Wait()
	if remoteErr != nil {
		logger.ErrorContext(ctx, "got error from local remote-serve", "remoteErr", remoteErr)
	} else {
		logger.DebugContext(ctx, "after cmd.Wait")
	}

	if localErr != nil || remoteErr != nil {
		return errors.Join(localErr, remoteErr)
	}
	return nil
}
