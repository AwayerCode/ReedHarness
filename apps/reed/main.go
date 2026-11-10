package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/AwayerCode/ReedHarness/apps/reed/internal/control"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var Version = "0.6.0-dev"

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	if len(args) == 1 && args[0] == "version" {
		json.NewEncoder(os.Stdout).Encode(map[string]string{"name": "reed", "version": Version})
		return 0
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Println("reed serve --config <file> [--listen 127.0.0.1:47832]\nreed version")
		return 0
	}
	if args[0] != "serve" {
		fmt.Fprintln(os.Stderr, "unknown command; professional checks are provided by swe")
		return 2
	}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	config := flags.String("config", "", "service configuration JSON")
	address := flags.String("listen", "127.0.0.1:47832", "IPv4 loopback listen address")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *config == "" || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "serve requires --config and no positional arguments")
		return 2
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil || host != "127.0.0.1" {
		fmt.Fprintln(os.Stderr, "listen must use 127.0.0.1")
		return 2
	}
	c, err := control.LoadConfig(*config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	listener, err := net.Listen("tcp4", *address)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer listener.Close()
	app, err := control.NewServer(c, control.Execute, listener.Addr().String())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer app.Close()
	server := &http.Server{Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		end, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(end)
	}()
	json.NewEncoder(os.Stdout).Encode(map[string]string{"service": "reed-control", "url": "http://" + listener.Addr().String(), "version": Version})
	if err = server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}
