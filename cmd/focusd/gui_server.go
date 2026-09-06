package main

// gui_server.go: montaje del servidor HTTP del GUI Studio (estáticos embebidos,
// middlewares y ruteo). Los handlers y DTOs del API viven en gui_api.go.

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"dbf/internal/catalog"
)

//go:embed static
var staticFiles embed.FS

// withStaticHeaders sets caching policy for embedded static assets: vendored
// libraries are immutable (versioned by path), app files are always revalidated.
func withStaticHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/vendor/") {
			w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// withRecover turns handler panics into a JSON 500 instead of killing the request.
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("focus: gui: panic serving %s: %v", r.URL.Path, rec)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withMethod rejects requests whose method differs from the expected one.
func withMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

// newGUIMux mounts the API routes (and, when static is non-nil, the embedded
// GUI files). Separated from startGUIServer so tests can drive the API with
// httptest without opening a port.
func newGUIMux(h executeHandler, cat *catalog.Catalog, queryTimeout time.Duration, static fs.FS) *http.ServeMux {
	mux := http.NewServeMux()
	if static != nil {
		mux.Handle("/", withStaticHeaders(http.FileServer(http.FS(static))))
	}
	mux.HandleFunc("/api/query", withMethod(http.MethodPost, handleAPIQuery(h, queryTimeout)))
	mux.HandleFunc("/api/script", withMethod(http.MethodPost, handleAPIScript(h, queryTimeout)))
	mux.HandleFunc("/api/validate", withMethod(http.MethodPost, handleAPIValidate()))
	mux.HandleFunc("/api/schemas", withMethod(http.MethodGet, handleAPISchemas(cat)))
	mux.HandleFunc("/api/schema", withMethod(http.MethodGet, handleAPISchema(cat)))
	mux.HandleFunc("/api/objects", withMethod(http.MethodGet, handleAPIObjects(cat)))
	mux.HandleFunc("/api/diagram", withMethod(http.MethodGet, handleAPIDiagram(cat)))
	mux.HandleFunc("/api/table-data", withMethod(http.MethodGet, handleAPITableData(cat)))
	return mux
}

func startGUIServer(addr string, h executeHandler, cat *catalog.Catalog, queryTimeout time.Duration) {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Printf("focus: gui: failed to mount static files: %v", err)
		return
	}
	mux := newGUIMux(h, cat, queryTimeout, sub)

	srv := &http.Server{
		Addr:              addr,
		Handler:           withRecover(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("focus: GUI available at http://localhost%s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("focus: GUI server error: %v", err)
	}
}
