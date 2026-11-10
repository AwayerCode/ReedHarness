package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AwayerCode/ReedHarness/apps/reed/internal/systemdesign"
)

func systemDesignServe(args []string, out io.Writer) int {
	fail := func(err error) int { return encode(out, systemdesign.UnavailableReport("serve", Version, err), 2) }
	flags := flag.NewFlagSet("system-design serve", flag.ContinueOnError)
	flags.SetOutput(out)
	root := flags.String("root", ".", "authorized workspace directory")
	expected := flags.String("expect-version", "", "required exact CLI version")
	parent := flags.Bool("parent-stdin", false, "stop when the owning plugin closes stdin")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return fail(err)
	}
	if flags.NArg() != 0 || *expected != Version {
		return fail(fmt.Errorf("serve requires --expect-version %s and no positional arguments", Version))
	}
	const host = "127.0.0.1:47831"
	service, err := systemdesign.NewService(*root, Version, host)
	if err != nil {
		return fail(err)
	}
	listener, err := net.Listen("tcp4", host)
	if err != nil {
		return fail(fmt.Errorf("fixed address %s unavailable: %w", host, err))
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *parent {
		go func() { _, _ = io.Copy(io.Discard, os.Stdin); stop() }()
	}
	server := &http.Server{Handler: service, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		case <-finished:
		}
	}()
	if encode(out, map[string]string{"url": "http://" + host + "/reed/", "version": Version}, 0) != 0 {
		return 2
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(err)
	}
	return 0
}
