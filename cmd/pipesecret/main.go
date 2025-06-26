package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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

	Run         RunCmd         `cmd:"" help:"Run the specified command with injecting secrets. This subcommand is supposed to be executed on the remote server."`
	RemoteServe RemoteServeCmd `cmd:"" help:"The remote server which is executed automatically by serve subcommand."`
	Serve       ServeCmd       `cmd:"" help:"Run local server. This subcommand is supposed to be executed on the local machine."`
	Version     VersionCmd     `cmd:"" help:"Show version and exit."`
}

type RunCmd struct {
	Item  string `group:"query" required:"" help:"Item name in password manager to get"`
	Query string `group:"query" required:"" default:"{\"username\": .fields[] | select(.id == \"username\").value, \"password\": .fields[] | select(.id == \"password\").value}" env:"PIPESECRET_QUERY" help:"query string for gojq"`

	Stdin  string            `group:"inject" help:"inject secret to stdin if not empty. format: Go text/template string. example: {{.username}}{{\"\\n\"}}{{.password}}{{\"\\n\"}}"`
	DirKey string            `group:"inject" help:"create temporary directory with random name for files which will be created --file. example: --dir-key=secret_dir --file=token.txt={{.username}};secret.txt={{.password}} --env=TOKEN_FILE={{.secret_dir}}/token.txt;SECRET_FILE={{.secret_dir}}/secret.txt"`
	File   map[string]string `group:"inject" help:"inject secret in a temporary file, value format: NAME1=TEMPLATE1;NAME2=TEMPLATE2, example: token.txt={{.username}};secret.txt={{.password}}"`
	Env    map[string]string `group:"inject" help:"inject secret with an environment variable, value format: NAME1=TEMPLATE1;NAME2=TEMPLATE2, example: TOKEN={{.username}};SECRET={{.password}}"`

	Socket         string        `group:"connect" required:"" default:"${default_socket_path}" env:"PIPESECRET_SOCKET" help:"unix socket path"`
	ConnectTimeout time.Duration `group:"connect" default:"5s" help:"connect timeout"`

	Command string   `group:"exec" arg:"" help:"path to command to be executed"`
	Args    []string `group:"exec" arg:"" optional:"" help:"arguments for the command to be executed"`
}

func (c *RunCmd) Run(ctx context.Context) (err error) {
	if len(c.Stdin) == 0 && len(c.Env) == 0 && len(c.File) == 0 {
		return errors.New("specify at least one of --stdin, --env, or --file")
	}
	slog.Debug("run subcommand", "len(Stdin)", len(c.Stdin), "len(Env)", len(c.Env), "len(File)", len(c.File))

	var stdinTmpl *template.Template
	if len(c.Stdin) > 0 {
		stdinTmpl, err = parseTemplate(c.Stdin)
		if err != nil {
			return err
		}
	}

	envTmplMap, err := parseTemplateMap(c.Env)
	if err != nil {
		return err
	}

	fileTmplMap, err := parseTemplateMap(c.File)
	if err != nil {
		return err
	}

	result, err := rpc.GetQueryItem(ctx, c.Socket, c.ConnectTimeout, c.Item, c.Query)
	if err != nil {
		return err
	}

	values, ok := result.(map[string]any)
	if !ok {
		return errors.New("query result is not a JSON object")
	}

	cmd := exec.CommandContext(ctx, c.Command, c.Args...)
	if stdinTmpl != nil {
		stdinText, err := executeTemplate(stdinTmpl, values)
		if err != nil {
			return err
		}
		cmd.Stdin = strings.NewReader(stdinText)
	} else {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	var secretDir string
	if len(fileTmplMap) > 0 {
		if c.DirKey != "" {
			secretDir, err = os.MkdirTemp("", "pipesecret-*")
			if err != nil {
				return err
			}
			defer func() {
				if err2 := os.RemoveAll(secretDir); err2 != nil && !errors.Is(err, fs.ErrNotExist) {
					err = errors.Join(err, err2)
				}
			}()

			values[c.DirKey] = secretDir
		}

		for k, tmpl := range fileTmplMap {
			v, err := executeTemplate(tmpl, values)
			if err != nil {
				return err
			}

			var filename string
			if secretDir != "" {
				filename = filepath.Join(secretDir, k)
			} else {
				filename = k
			}

			if err := os.WriteFile(filename, []byte(v), 0o600); err != nil {
				return err
			}
			defer func() {
				if err2 := os.Remove(k); err2 != nil && !errors.Is(err, fs.ErrNotExist) {
					err = errors.Join(err, err2)
				}
			}()
		}
	}

	if len(envTmplMap) > 0 || secretDir != "" {
		cmd.Env = cmd.Environ()
		for k, tmpl := range envTmplMap {
			v, err := executeTemplate(tmpl, values)
			if err != nil {
				return err
			}
			slog.Debug("adding environment variable", "name", k, "value", v)
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}

		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", c.DirKey, secretDir))
	}

	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to run command: %s", err)
	}

	return nil
}

func parseTemplate(tmpl string) (*template.Template, error) {
	return template.New("").Parse(tmpl)
}

func parseTemplateMap(tmplMap map[string]string) (map[string]*template.Template, error) {
	if len(tmplMap) == 0 {
		return nil, nil
	}

	resultMap := make(map[string]*template.Template)
	for k, v := range tmplMap {
		tmpl, err := parseTemplate(v)
		if err != nil {
			return nil, err
		}
		resultMap[k] = tmpl
	}
	return resultMap, nil
}

func executeTemplate(tmpl *template.Template, values any) (string, error) {
	var output strings.Builder
	if err := tmpl.Execute(&output, values); err != nil {
		return "", err
	}
	return output.String(), nil
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
