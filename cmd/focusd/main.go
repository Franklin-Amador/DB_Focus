package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dbf/internal/catalog"
	"dbf/internal/server"
	"dbf/internal/storage"
)

func main() {
	addr := flag.String("addr", ":4444", "PostgreSQL wire protocol address")
	guiAddr := flag.String("gui", ":9011", "GUI studio address")
	dataDir := flag.String("data", "./data", "data directory for persistence")
	maxConns := flag.Int("max-conns", 20, "max concurrent connections")
	bufSize := flag.Int("buf-size", 4096, "buffer size per connection in bytes")
	queryTimeout := flag.Duration("query-timeout", 60*time.Second, "max duration for a GUI query (0 = no limit)")
	flag.Parse()

	cluster := catalog.NewCluster()

	st, err := storage.NewPebbleStorage(*dataDir)
	if err != nil {
		log.Fatalf("focus: failed to initialize storage: %v", err)
	}
	defer st.Close()

	// Loads the default database and every other persisted database into the cluster.
	if err := st.LoadAll(cluster.Default()); err != nil {
		log.Printf("focus: warning: failed to load persisted data: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	// One executor + job scheduler per database (created lazily for new ones).
	handler := newExecuteHandler(ctx, cluster, st)
	log.Printf("focus: %d database(s) online, job schedulers started", len(cluster.ListDatabases()))

	// If $PORT is set (Render, Railway, Fly.io…) the GUI listens there so the
	// platform can route external HTTP traffic to it. Otherwise use -gui flag.
	guiListen := *guiAddr
	if envPort := os.Getenv("PORT"); envPort != "" {
		guiListen = "0.0.0.0:" + envPort
	}
	go startGUIServer(guiListen, handler, cluster, *queryTimeout)

	if err := server.ListenAndServeWithConfig(*addr, handler, cluster, *maxConns, *bufSize); err != nil {
		log.Fatalf("focus: %v", err)
	}
}
