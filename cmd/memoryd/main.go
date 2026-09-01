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
	"github.com/caelis-labs/memory/internal/steward"
)

func main() {
	if err := run(); err != nil {
		log.SetFlags(0)
		log.Printf("memoryd: %v", err)
		os.Exit(1)
	}
}

func run() error {
	var dataDir, stewardProvidersPath string
	var showVersion bool
	flag.StringVar(&dataDir, "data-dir", "", "owner-only memoryd data directory (required)")
	flag.StringVar(&stewardProvidersPath, "steward-providers", "", "owner-only Steward provider configuration")
	flag.BoolVar(&showVersion, "version", false, "print service version and build revision")
	flag.Parse()
	if showVersion {
		fmt.Printf("memoryd %s (%s)\n", buildinfo.ServiceVersion, buildinfo.BuildRevision)
		return nil
	}
	if dataDir == "" {
		return fmt.Errorf("-data-dir is required")
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()
	var providers map[string]steward.Provider
	var workerOptions steward.Options
	if stewardProvidersPath != "" {
		_, configured, options, err := steward.LoadConfig(stewardProvidersPath)
		if err != nil {
			return err
		}
		providers, workerOptions = configured, options
	}
	store, err := appliance.Open(ctx, appliance.Options{DataDir: dataDir})
	if err != nil {
		return err
	}
	var stewardPool *steward.Pool
	if len(providers) > 0 {
		stewardPool, err = steward.NewPool(store, providers, workerOptions)
		if err != nil {
			_ = store.Close()
			return err
		}
	}
	store.SetStewardRuntimeDiagnostics(len(providers), workerOptions.Workers)
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
	var stewardDone chan struct{}
	if stewardPool != nil {
		stewardDone = make(chan struct{})
		go func() {
			stewardPool.Run(ctx)
			close(stewardDone)
		}()
	}
	log.SetFlags(0)
	readiness := "ready"
	if err := store.Ready(context.Background()); err != nil {
		readiness = "management_only"
	}
	log.Printf("memoryd listening: socket=%s management_credential=%s readiness=%s steward_workers=%d", socketPath, store.ManagementCredentialPath(), readiness, workerOptions.Workers)
	select {
	case <-ctx.Done():
		cancel()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr := localtransport.ShutdownServer(shutdownCtx, server)
		waitSteward(stewardDone)
		return errors.Join(shutdownErr, store.Close())
	case err := <-serveErr:
		cancel()
		waitSteward(stewardDone)
		if errors.Is(err, http.ErrServerClosed) {
			return store.Close()
		}
		return errors.Join(err, store.Close())
	}
}

func waitSteward(done <-chan struct{}) {
	if done != nil {
		<-done
	}
}
