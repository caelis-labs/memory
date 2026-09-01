package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/caelis-labs/memory/internal/appliance"
	"github.com/caelis-labs/memory/internal/buildinfo"
	"github.com/caelis-labs/memory/internal/localtransport"
)

func main() {
	if err := run(); err != nil {
		log.SetFlags(0)
		log.Printf("memoryd: %v", err)
		os.Exit(1)
	}
}

func run() error {
	var dataDir string
	var showVersion bool
	flag.StringVar(&dataDir, "data-dir", "", "owner-only memoryd data directory (required)")
	flag.BoolVar(&showVersion, "version", false, "print service version and build revision")
	flag.Parse()
	if showVersion {
		fmt.Printf("memoryd %s (%s)\n", buildinfo.ServiceVersion, buildinfo.BuildRevision)
		return nil
	}
	if dataDir == "" {
		return fmt.Errorf("-data-dir is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
	if err != nil {
		return err
	}
	socketPath := filepath.Join(dataDir, appliance.SocketFilename)
	listener, err := localtransport.ListenUnix(socketPath)
	if err != nil {
		_ = store.Close()
		return err
	}
	server := &http.Server{
		Handler: localtransport.Handler(store, localtransport.ServiceInfo{
			Version: buildinfo.ServiceVersion, Revision: buildinfo.BuildRevision,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	log.SetFlags(0)
	readiness := "ready"
	if err := store.Ready(context.Background()); err != nil {
		readiness = "management_only"
	}
	log.Printf("memoryd listening: socket=%s management_credential=%s readiness=%s", socketPath, store.ManagementCredentialPath(), readiness)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr := localtransport.ShutdownServer(shutdownCtx, server)
		return errors.Join(shutdownErr, store.Close())
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return store.Close()
		}
		return errors.Join(err, store.Close())
	}
}
