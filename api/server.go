package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dogeorg/governor"
	"github.com/dogeorg/indexer/spec"
)

func New(bind string, store spec.Store) governor.Service {
	mux := http.NewServeMux()
	a := &WebAPI{
		_store: store,
		srv: http.Server{
			Addr:    bind,
			Handler: mux,
		},
	}

	mux.HandleFunc("/health", a.healthCheck)

	return a
}

type WebAPI struct {
	governor.ServiceCtx
	_store spec.Store
	store  spec.Store
	srv    http.Server
}

// called on any Goroutine
func (a *WebAPI) Stop() {
	// new goroutine because Shutdown() blocks
	go func() {
		// cannot use ServiceCtx here because it's already cancelled
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		a.srv.Shutdown(ctx) // blocking call
		cancel()
	}()
}

// goroutine
func (a *WebAPI) Run() {
	a.store = a._store.WithCtx(a.Context) // Service Context is first available here
	log.Printf("HTTP server listening on: %v\n", a.srv.Addr)
	if err := a.srv.ListenAndServe(); err != http.ErrServerClosed { // blocking call
		log.Printf("HTTP server: %v\n", err)
	}
}

func (a *WebAPI) healthCheck(w http.ResponseWriter, r *http.Request) {
	_, err := a.store.GetResumePoint()
	if err != nil {
		w.Write([]byte(fmt.Sprintf(`{"ok":false,"error":"%v"}`, err)))
	} else {
		w.Write([]byte(`{"ok":true}`))
	}
}
