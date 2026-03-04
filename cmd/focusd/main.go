package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"dbf/internal/catalog"
	"dbf/internal/executor"
	"dbf/internal/server"
	"dbf/internal/storage"
)

func main() {
	addr := flag.String("addr", ":4444", "listen address")
	dataDir := flag.String("data", "./data", "data directory for persistence")
	maxConns := flag.Int("max-conns", 20, "max concurrent connections (default: 20 for 512MB systems)")
	bufSize := flag.Int("buf-size", 4096, "buffer size per connection in bytes (default: 4096)")
	flag.Parse()

	cat := catalog.New()

	st, err := storage.NewPebbleStorage(*dataDir)
	if err != nil {
		log.Fatalf("focus: failed to initialize storage: %v", err)
	}
	defer st.Close()

	if err := st.LoadAll(cat); err != nil {
		log.Printf("focus: warning: failed to load persisted data: %v", err)
	}

	exe := executor.New(cat, st)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start job scheduler
	exe.StartJobScheduler(ctx)
	log.Printf("focus: job scheduler started")

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Printf("focus: shutting down...")
		cancel()
	}()

	log.Printf("focus: starting on %s (data dir: %s) [pebble backend]", *addr, *dataDir)
	log.Printf("focus: limits - max connections: %d, buffer size: %d bytes", *maxConns, *bufSize)

	handler := executeHandler{
		executor: exe,
		catalog:  cat,
	}

	if err := server.ListenAndServeWithConfig(*addr, handler, cat, *maxConns, *bufSize); err != nil {
		log.Fatalf("focus: %v", err)
	}
}
