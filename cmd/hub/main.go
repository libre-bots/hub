package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/libre-bots/hub/internal/auth"
)

var version = "dev"

const usage = "usage: hub [serve|token|health|version]"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	if len(args) > 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var err error
	switch command {
	case "serve":
		err = serve(ctx, getenv, stderr)
	case "token":
		err = printToken(stdout)
	case "health":
		err = health(ctx, getenv)
	case "version":
		fmt.Fprintln(stdout, version)
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "hub: %v\n", err)
		return 1
	}
	return 0
}

func printToken(stdout io.Writer) error {
	token, err := auth.NewToken()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "token: %s\nsha256: %s\n", token, auth.HashToken(token))
	return nil
}
