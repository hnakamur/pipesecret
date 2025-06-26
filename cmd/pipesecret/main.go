package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"text/template"
	"time"

	"github.com/alecthomas/kong"
	"github.com/hnakamur/pipesecret/internal/rpc"
	"golang.org/x/xerrors"
)

var cli struct {
	Debug bool `help:"Enable debug mode."`

	PassToStdin PassToStdinCmd `cmd:"" help:"Run the specified command with passing secrets to stdin. This subcommand is supposed to be executed on the remote server."`
	PassWithEnv PassWithEnvCmd `cmd:"" help:"Run the specified command with passing secrets with environment variables. This subcommand is supposed to be executed on the remote server."`
	RemoteServe RemoteServeCmd `cmd:"" help:"The remote server which is executed automatically by serve subcommand."`
	Serve       ServeCmd       `cmd:"" help:"Run local server. This subcommand is supposed to be executed on the local machine."`
	Version     VersionCmd     `cmd:"" help:"Show version and exit."`
}

type PassWithEnvCmd struct {
	Item    string        `group:"query" required:"" help:"Item name in password manager to get"`
	Query   string        `group:"query" required:"" default:"{\"username\": .fields[] | select(.id == \"username\").value, \"password\": .fields[] | select(.id == \"password\").value}" env:"PIPESECRET_QUERY" help:"query string for gojq"`
	Socket  string        `group:"connect" required:"" default:"${default_socket_path}" env:"PIPESECRET_SOCKET" help:"unix socket path"`
	Timeout time.Duration `group:"connect" default:"5s" help:"connect timeout"`

	Command string   `group:"exec" arg:"" help:"path to command to be executed"`
	Args    []string `group:"exec" arg:"" optional:"" help:"arguments for the command to be executed"`
}

func (c *PassWithEnvCmd) Run(ctx context.Context) error {
	result, err := rpc.GetQueryItem(ctx, c.Socket, c.Timeout, c.Item, c.Query)
	if err != nil {
		return err
	}

	values, ok := result.(map[string]any)
	if !ok {
		return errors.New("query result is not a JSON object")
	}

	cmd := exec.CommandContext(ctx, c.Command, c.Args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = cmd.Environ()
	for k, v := range values {
		if s, ok := v.(string); ok {
			slog.Debug("adding environment variable", "name", k, "value", s)
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, s))
		}
	}
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to run command: %s", err)
	}

	return nil
}

type PassToStdinCmd struct {
	Item    string        `group:"query" required:"" help:"Item name in password manager to get"`
	Query   string        `group:"query" required:"" default:"{\"PIPESECRET_USER\": .fields[] | select(.id == \"username\").value, \"PIPESECRET_PASS\": .fields[] | select(.id == \"password\").value}" env:"PIPESECRET_QUERY" help:"query string for gojq"`
	Socket  string        `group:"connect" required:"" default:"${default_socket_path}" env:"PIPESECRET_SOCKET" help:"unix socket path"`
	Timeout time.Duration `group:"connect" default:"5s" help:"connect timeout"`

	Template string   `group:"exec" required:"" default:"{{.PIPESECRET_USER}}{{\"\\n\"}}{{.PIPESECRET_PASS}}{{\"\\n\"}}" help:"Go text/template string to format secrets to be passed to stdin."`
	Command  string   `group:"exec" arg:"" help:"path to command to be executed"`
	Args     []string `group:"exec" arg:"" optional:"" help:"arguments for the command to be executed"`
}

func (c *PassToStdinCmd) Run(ctx context.Context) error {
	tmpl, err := template.New("template1").Parse(c.Template)
	if err != nil {
		return err
	}

	result, err := rpc.GetQueryItem(ctx, c.Socket, c.Timeout, c.Item, c.Query)
	if err != nil {
		return err
	}

	values, ok := result.(map[string]any)
	if !ok {
		return errors.New("query result is not a JSON object")
	}

	var templateOutput strings.Builder
	if err := tmpl.Execute(&templateOutput, values); err != nil {
		return err
	}
	slog.Debug("rendered template", "templateOutput", templateOutput)

	cmd := exec.CommandContext(ctx, c.Command, c.Args...)
	cmd.Stdin = strings.NewReader(templateOutput.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to run command: %s", err)
	}

	return nil
}

type RemoteServeCmd struct {
	Socket    string        `group:"listen" required:"" default:"${default_socket_path}" help:"unix socket path"`
	Heartbeat time.Duration `group:"pipe rpc" default:"5s" help:"heartbeat interval"`
}

func (c *RemoteServeCmd) Run(ctx context.Context) error {
	slog.Debug("remote-serve", "socketPath", c.Socket)
	s := rpc.NewRemoteServer(c.Socket, c.Heartbeat)
	if err := s.Run(ctx, os.Stdout, os.Stdin); err != nil {
		return err
	}
	return nil
}

type ServeCmd struct {
	SSH     string `group:"pipe rpc" required:"" default:"ssh" env:"PIPESECRET_SSH" help:"ssh command"`
	Host    string `group:"pipe rpc" required:"" env:"PIPESECRET_HOST" help:"destination hostname"`
	Command string `group:"pipe rpc" required:"" env:"PIPESECRET_COMMAND" help:"command and arguements to execute on the destination host"`
	Op      string `required:"" env:"PIPESECRET_OP" help:"path to 1Password CLI"`
}

func (c *ServeCmd) Run(ctx context.Context) error {
	return rpc.RunLocalServer(ctx, c.SSH, c.Host, c.Command, c.Op)
}

type VersionCmd struct{}

func (c *VersionCmd) Run(ctx context.Context) error {
	fmt.Println(Version())
	return nil
}

func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(devel)"
}

func main() {
	slogLevel := new(slog.LevelVar)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel}))
	slog.SetDefault(logger)

	ctx := kong.Parse(&cli, kong.Vars{
		"default_socket_path": "/tmp/pipesecret.sock",
	})
	if cli.Debug {
		slogLevel.Set(slog.LevelDebug)
	}
	// kong.BindTo is needed to bind a context.Context value.
	// See https://github.com/alecthomas/kong/issues/48
	ctx.BindTo(context.Background(), (*context.Context)(nil))
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
