package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"

	"github.com/hnakamur/pipesecret/internal"
	"github.com/hnakamur/pipesecret/internal/piperpc"
	"golang.org/x/exp/jsonrpc2"
	"golang.org/x/xerrors"
)

func RunLocalServer(ctx context.Context, sshPath, host, remoteCommand, opExePath string) error {
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
		log.Printf("got interrupt signal")
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("failed to kill remote process: %s", err)
		} else {
			log.Printf("killed remote process")
		}
		cancel()
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("pipeClient: %s", line)
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, fs.ErrClosed) {
			log.Printf("failed to scan pipeClient stderr: %s", err)
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
		log.Printf("localErr=%v", localErr)
	} else {
		log.Printf("after server Run")
	}

	remoteErr := cmd.Wait()
	if remoteErr != nil {
		log.Printf("remoteErr=%v", remoteErr)
	} else {
		log.Printf("after cmd.Wait")
	}

	if localErr != nil || remoteErr != nil {
		return errors.Join(localErr, remoteErr)
	}
	return nil
}
