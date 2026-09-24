package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"teamwork/core/internal/kernel"
	"teamwork/core/internal/manifest"
	"teamwork/core/internal/platform"
	"teamwork/core/internal/service"
)

var version = "2.0.0-alpha.2"

func main() {
	if err := run(); err != nil {
		slog.Error("teamwork failed", "error", err)
		os.Exit(1)
	}
}
func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	paths, err := platform.Resolve()
	if err != nil {
		return err
	}
	switch command {
	case "serve":
		return serve(paths)
	case "modules":
		items, err := manifest.Discover(paths.Modules)
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Printf("%s\t%s\t%s\n", item.Module.ID, item.Module.Version, item.Module.Name)
		}
		return nil
	case "paths":
		return json.NewEncoder(os.Stdout).Encode(paths)
	case "service-install":
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		path, err := service.Install(executable)
		if err == nil {
			fmt.Println(path)
		}
		return err
	case "version":
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("unknown command %q; use serve, modules, paths, service-install, or version", command)
	}
}
func serve(paths platform.Paths) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	core, err := kernel.New(paths, logger)
	if err != nil {
		return err
	}
	defer core.Close()
	if err := core.Start(); err != nil {
		return err
	}
	address := os.Getenv("TEAMWORK_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8090"
	}
	server := &http.Server{Addr: address, Handler: core.Handler(), ReadHeaderTimeout: 5 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	logger.Info("teamwork started", "address", "http://"+address, "modules", paths.Modules, "pid", os.Getpid(), "executable", filepath.Base(os.Args[0]))
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
