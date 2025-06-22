package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/alecthomas/kong"
	"github.com/hnakamur/pipesecret/internal"
	"github.com/hnakamur/pipesecret/internal/piperpc"
	"github.com/hnakamur/pipesecret/internal/rpc"
	"github.com/hnakamur/pipesecret/internal/unixsocketrpc"
	"golang.org/x/exp/jsonrpc2"
	"golang.org/x/xerrors"
)

const shutdownMethod = "shutdown"

type RemoteServeCmd struct {
	Socket string `default:"/tmp/pipesecret.sock" help:"unix socket path"`
}

func (c *RemoteServeCmd) Run(ctx context.Context) error {
	s := rpc.NewRemoteServer(c.Socket)
	if err := s.Run(ctx, os.Stdout, os.Stdin); err != nil {
		return err
	}
	return nil
}

// func (c *RemoteServeCmd) Run(ctx context.Context) error {
// 	client := piperpc.NewClient(jsonrpc2.RawFramer(), time.Second)
// 	if err := client.Run(ctx, os.Stdout, os.Stdin); err != nil {
// 		return err
// 	}
// 	return nil
// }

// func (c *RemoteServeCmd) Run(ctx context.Context) error {
// 	fmt.Printf("remote-server, socket=%s\n", c.Socket)
// 	if err := os.Remove(c.Socket); err != nil && !errors.Is(err, fs.ErrNotExist) {
// 		return err
// 	}
// 	listener, err := jsonrpc2.NetListener(ctx, "unix", c.Socket, jsonrpc2.NetListenOptions{})
// 	if err != nil {
// 		return err
// 	}

// 	quitCh := make(chan struct{}, 1)
// 	handle := func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
// 		switch req.Method {
// 		case "hello":
// 			return "hi", nil
// 		case "quit":
// 			quitCh <- struct{}{}
// 			return "exiting", nil
// 		default:
// 			return nil, jsonrpc2.ErrNotHandled
// 		}
// 	}

// 	connOpts := jsonrpc2.ConnectionOptions{Handler: jsonrpc2.HandlerFunc(handle)}
// 	server, err := jsonrpc2.Serve(ctx, listener, connOpts)
// 	if err != nil {
// 		return err
// 	}

// 	var closeErr error
// 	go func() {
// 		<-quitCh
// 		if err := listener.Close(); err != nil && !errors.Is(err, fs.ErrNotExist) {
// 			closeErr = err
// 			log.Printf("closeErr=%v", closeErr)
// 		}
// 	}()
// 	if err := server.Wait(); err != nil || closeErr != nil {
// 		log.Printf("waitErr=%v", err)
// 		return errors.Join(err, closeErr)
// 	}
// 	return nil
// }

type RemoteCmd struct {
	Item    string        `default:"テスト 3" help:"item name to get"`
	Socket  string        `default:"/tmp/pipesecret.sock" help:"unix socket path"`
	Timeout time.Duration `default:"5s" help:"dialer timeout"`
}

func (c *RemoteCmd) Run(ctx context.Context) error {
	timeout := 5 * time.Second
	client, err := unixsocketrpc.Connect(ctx, socketPath, timeout)
	if err != nil {
		return xerrors.Errorf("failed to connect unix socket server: %s", err)
	}
	defer client.Close()

	params := GetQueryItemRequestParams{
		Item:  c.Item,
		Query: `{"username": .fields[] | select(.id == "username").value, "password": .fields[] | select(.id == "password").value}`,
	}
	if res, id, err := client.CallSync(ctx, "getQueryItem", params); err != nil {
		return xerrors.Errorf("failed to call getQueryItem: %s", err)
	} else {
		log.Printf("got response for getQueryItem: %s, id=%v", res, id)
	}

	// if res, id, err := client.CallSync(ctx, shutdownMethod, nil); err != nil {
	// 	return xerrors.Errorf("failed to call shutdown: %s", err)
	// } else {
	// 	log.Printf("got response for shutdown: %s, id=%v", res, id)
	// }
	return nil
}

// func (c *RemoteCmd) Run(ctx context.Context) error {
// 	dialer := jsonrpc2.NetDialer("unix", c.Socket, net.Dialer{
// 		Timeout: c.Timeout,
// 	})
// 	client, err := jsonrpc2.Dial(ctx,
// 		dialer,
// 		jsonrpc2.ConnectionOptions{})
// 	if err != nil {
// 		return err
// 	}
// 	call := client.Call(ctx, "hello", nil)
// 	var resp any
// 	if err := call.Await(ctx, &resp); err != nil {
// 		return err
// 	}
// 	log.Printf("resp=%+v", resp)

// 	call = client.Call(ctx, "quit", nil)
// 	if err := call.Await(ctx, &resp); err != nil {
// 		return err
// 	}
// 	log.Printf("resp#2=%+v", resp)

// 	return nil
// }

type ServeCmd struct {
}

func (c *ServeCmd) Run(ctx context.Context) error {
	cmd := exec.Command("ssh", "ggear", "/home/hnakamur/.local/bin/pipesecret remote-serve")
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

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("pipeClient: %s", line)
		}
		if err := scanner.Err(); err != nil {
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
		case "howdy":
			return "hohoho", nil
		default:
			return nil, jsonrpc2.ErrNotHandled
		}
	}

	server := piperpc.NewServer(jsonrpc2.RawFramer(), jsonrpc2.HandlerFunc(handler))
	localErr := server.Run(ctx, stdout, stdin)
	remoteErr := cmd.Wait()
	if localErr != nil {
		log.Printf("localErr=%v", localErr)
	}
	if remoteErr != nil {
		log.Printf("remoteErr=%v", remoteErr)
	}
	if localErr != nil || remoteErr != nil {
		return errors.Join(localErr, remoteErr)
	}
	return nil
}

const socketPath = "/tmp/pipesecret.sock"

const opExePath = "/mnt/c/Users/hnakamur/AppData/Local/Microsoft/WinGet/Links/op.exe"

type GetQueryItemRequestParams struct {
	Item  string
	Query string
}

// func (c *ServeCmd) Run(ctx context.Context) error {
// 	s, err := unixsocketrpc.Listen(ctx, socketPath)
// 	if err != nil {
// 		return err
// 	}

// 	handler := func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
// 		switch req.Method {
// 		case "getQueryItem":
// 			var params GetQueryItemRequestParams
// 			if err := json.Unmarshal(req.Params, &params); err != nil {
// 				return nil, xerrors.Errorf("%w: %s", jsonrpc2.ErrParse, err)
// 			}
// 			getter, err := internal.NewOnePasswordItemGetter(opExePath)
// 			if err != nil {
// 				return nil, xerrors.Errorf("%w: %s", jsonrpc2.ErrInternal, err)
// 			}
// 			result, err := internal.GetQueryItem(ctx, getter, params.Item, params.Query)
// 			if err != nil {
// 				return nil, xerrors.Errorf("%w: %s", jsonrpc2.ErrInvalidRequest, err)
// 			}
// 			return result, nil
// 		case "howdy":
// 			return "hohoho", nil
// 		default:
// 			return nil, jsonrpc2.ErrNotHandled
// 		}
// 	}
// 	shutdownGracePeriod := time.Second
// 	if err := s.Run(ctx, jsonrpc2.HandlerFunc(handler), shutdownMethod, shutdownGracePeriod); err != nil {
// 		return fmt.Errorf("failed to run server: %s", err)
// 	}
// 	return nil
// }

type LocalCmd struct {
	Paths []string `optional:"" name:"path" help:"Paths to list." type:"path"`
}

func (l *LocalCmd) Run(ctx context.Context) error {
	timeout := 5 * time.Second
	client, err := unixsocketrpc.Connect(ctx, socketPath, timeout)
	if err != nil {
		return xerrors.Errorf("failed to connect unix socket server: %s", err)
	}
	defer client.Close()

	params := GetQueryItemRequestParams{
		Item:  "テスト 3",
		Query: `{"username": .fields[] | select(.id == "username").value, "password": .fields[] | select(.id == "password").value}`,
	}
	if res, id, err := client.CallSync(ctx, "getQueryItem", params); err != nil {
		return xerrors.Errorf("failed to call getQueryItem: %s", err)
	} else {
		log.Printf("got response for getQueryItem: %s, id=%v", res, id)
	}

	// if res, id, err := client.CallSync(ctx, shutdownMethod, nil); err != nil {
	// 	return xerrors.Errorf("failed to call shutdown: %s", err)
	// } else {
	// 	log.Printf("got response for shutdown: %s, id=%v", res, id)
	// }
	return nil
}

var cli struct {
	Debug bool `help:"Enable debug mode."`

	Local       LocalCmd       `cmd:"" help:"local client subcommand."`
	Remote      RemoteCmd      `cmd:"" help:"remote client subcommand."`
	RemoteServe RemoteServeCmd `cmd:"" help:"remote server subcommand."`
	Serve       ServeCmd       `cmd:"" help:"controller server subcommand."`
}

func main() {
	ctx := kong.Parse(&cli)
	// kong.BindTo is needed to bind a context.Context value.
	// See https://github.com/alecthomas/kong/issues/48
	ctx.BindTo(context.Background(), (*context.Context)(nil))
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
