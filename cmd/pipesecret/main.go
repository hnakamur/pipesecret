package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
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

var cli struct {
	Debug bool `help:"Enable debug mode."`

	Remote      RemoteCmd      `cmd:"" help:"remote client subcommand."`
	RemoteServe RemoteServeCmd `cmd:"" help:"remote server which is executed automatically by serve subcommand."`
	Serve       ServeCmd       `cmd:"" help:"controller server subcommand."`
}

const defaultSocketPathEnvName = "PIPESECRET_SOCKET"

var defaultSocketPath string

type RemoteServeCmd struct {
	Socket    string        `group:"listen" required:"" default:"${default_socket_path}" env:"PIPESECRET_SOCKET" help:"unix socket path"`
	Heartbeat time.Duration `group:"pipe rpc" default:"5s" help:"heartbeat interval"`
}

func (c *RemoteServeCmd) Run(ctx context.Context) error {
	log.Printf("remote-serve socket=%s", c.Socket)
	s := rpc.NewRemoteServer(c.Socket, c.Heartbeat)
	if err := s.Run(ctx, os.Stdout, os.Stdin); err != nil {
		return err
	}
	return nil
}

type RemoteCmd struct {
	Item    string        `group:"query" required:"" help:"item name to get"`
	Query   string        `group:"query" required:"" default:"{\"username\": .fields[] | select(.id == \"username\").value, \"password\": .fields[] | select(.id == \"password\").value}" env:"PIPESECRET_QUERY" help:"query string for gojq"`
	Socket  string        `group:"connect" required:"" default:"${default_socket_path}" env:"PIPESECRET_SOCKET" help:"unix socket path"`
	Timeout time.Duration `group:"connect" default:"5s" help:"connect timeout"`
}

type GetQueryItemRequestParams struct {
	Item  string
	Query string
}

func (c *RemoteCmd) Run(ctx context.Context) error {
	client, err := unixsocketrpc.Connect(ctx, c.Socket, c.Timeout)
	if err != nil {
		return xerrors.Errorf("failed to connect unix socket server: %s", err)
	}
	defer client.Close()

	params := GetQueryItemRequestParams{
		Item:  c.Item,
		Query: c.Query,
	}
	if res, id, err := client.CallSync(ctx, "getQueryItem", params); err != nil {
		return xerrors.Errorf("failed to call getQueryItem: %s", err)
	} else {
		log.Printf("got response for getQueryItem: %s, id=%v", res, id)
	}

	return nil
}

type ServeCmd struct {
	SSH     string `group:"pipe rpc" required:"" default:"ssh" env:"PIPESECRET_SSH" help:"ssh command"`
	Host    string `group:"pipe rpc" required:"" env:"PIPESECRET_HOST" help:"destination hostname"`
	Command string `group:"pipe rpc" required:"" default:"${default_command}" env:"PIPESECRET_COMMAND" help:"command and arguements to execute on the destination host"`
	Op      string `required:"" env:"PIPESECRET_OP" help:"path to 1Password CLI"`
}

func (c *ServeCmd) Run(ctx context.Context) error {
	cmd := exec.Command(c.SSH, c.Host, c.Command)
	cmd.Env = append(cmd.Environ(), fmt.Sprintf("%s=%s", defaultSocketPathEnvName, defaultSocketPath))
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
	log.Printf("ssh pid=%d", cmd.Process.Pid)

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
			getter, err := internal.NewOnePasswordItemGetter(c.Op)
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

func main() {
	defaultSocketPath = os.Getenv(defaultSocketPathEnvName)
	if defaultSocketPath == "" {
		defaultSocketPath = "/tmp/pipesecret.sock"
	}
	log.Printf("pipesecret defaultSocketPath=%s", defaultSocketPath)

	ctx := kong.Parse(&cli, kong.Vars{
		"default_socket_path": defaultSocketPath,
		"default_command":     os.Getenv("PIPESECRET_COMMAND"),
	})
	// kong.BindTo is needed to bind a context.Context value.
	// See https://github.com/alecthomas/kong/issues/48
	ctx.BindTo(context.Background(), (*context.Context)(nil))
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
