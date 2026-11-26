// Command timelapse records a timelapse of every print on one printer for
// Prusa Connect Local, without a line of extra G-code.
//
// It reads the camera's stream during the print, splits it into layers by the
// Z height the printer reports, and keeps one frame per layer: the one on
// which the print head hides the least. When the print ends it renders the
// frames into an MP4 and puts it into a storage of the server.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

const (
	name    = "timelapse"
	release = extension.Version("0.3.0")
)

//go:embed openapi.json
var openAPI []byte

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	line, err := wire.Handshake{
		Protocol: wire.ProtocolMajor,
		MinMinor: wire.MinMinor,
		MaxMinor: wire.MaxMinor,
		Address:  wire.StdioAddress,
	}.Line()
	if err != nil {
		return fmt.Errorf("render the handshake: %w", err)
	}

	if _, err = os.Stdout.WriteString(line); err != nil {
		return fmt.Errorf("write the handshake: %w", err)
	}

	rec := newRecorder()
	methods := wirerpc.Methods{}

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{Role: wirerpc.RoleChild, Methods: methods})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	methods[wirerpc.MethodInfo] = func(context.Context, wirerpc.Call) (any, error) {
		return routesapi.Describe(wirerpc.Info{
			Name:     name,
			Version:  release,
			Contract: wirerpc.Contract,
			Serves:   []wirerpc.Method{wirerpc.MethodConfigure},
		}, openAPI)
	}
	methods[wirerpc.MethodShutdown] = func(context.Context, wirerpc.Call) (any, error) {
		stop()

		return nil, nil
	}
	methods[wirerpc.MethodConfigure] = wirerpc.ConfigureGranted(
		func(_ context.Context, held Settings, grants wirerpc.Grants) error {
			if err := held.check(); err != nil {
				return err
			}

			if grants.Data == "" {
				return fmt.Errorf("%w: the module was installed without its data directory; install it with -approve", errSettings)
			}

			// The recorder starts on the first configure and lives as long as
			// the process: a changed setting restarts the module anyway.
			rec.start(ctx, conn, held, grants.Data)

			return nil
		})
	rec.access = accessapi.NewClient(conn)
	methods[routesapi.MethodServe] = routesapi.Serve(rec.routes())

	fmt.Fprintf(os.Stderr, "%s %s is serving on stdio as pid %d\n", name, release, os.Getpid())

	if err = conn.Serve(ctx); err != nil && ctx.Err() == nil {
		return fmt.Errorf("serve: %w", err)
	}

	rec.wait()

	return nil
}

// stdio is the channel the host handed this process.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
