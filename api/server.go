package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dogeorg/doge"
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
	mux.HandleFunc("/balance", a.getBalance)
	mux.HandleFunc("/utxo", a.getUtxo)

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

func (a *WebAPI) getBalance(w http.ResponseWriter, r *http.Request) {
	options := "GET, OPTIONS"
	if r.Method == http.MethodGet {
		address := r.URL.Query().Get("address")
		if address == "" {
			sendError(w, 400, "bad-request", "missing 'address' in the URL", options)
			return
		}
		pubkeyHash, err := doge.Base58DecodeCheck(address)
		if err != nil {
			sendError(w, 400, "bad-request", "invalid Dogecoin address", options)
			return
		}
		if len(pubkeyHash) != 21 {
			sendError(w, 400, "bad-request", "invalid Dogecoin address", options)
			return
		}
		kind := utxoKindFromVersionByte(pubkeyHash[0])
		hash := pubkeyHash[1:]
		bal, err := a.store.GetBalance(kind, hash, 6)
		bal.Current = bal.Available + bal.Incoming
		if err != nil {
			sendError(w, 500, "error", err.Error(), options)
		} else {
			sendJson(w, bal, options)
		}
	} else if r.Method == http.MethodOptions {
		sendOptions(w, r, options)
	}
}

func (a *WebAPI) getUtxo(w http.ResponseWriter, r *http.Request) {
	_, err := a.store.GetResumePoint()
	if err != nil {
		w.Write([]byte(fmt.Sprintf(`{"ok":false,"error":"%v"}`, err)))
	} else {
		w.Write([]byte(`{"ok":true}`))
	}
}

func utxoKindFromVersionByte(version byte) doge.ScriptType {
	switch version {
	case doge.DogeMainNetChain.P2PKH_Address_Prefix:
	case doge.DogeTestNetChain.P2PKH_Address_Prefix:
	case doge.DogeRegTestChain.P2PKH_Address_Prefix:
	case doge.BitcoinMainChain.P2PKH_Address_Prefix:
	case doge.BitcoinTestChain.P2PKH_Address_Prefix:
		return doge.ScriptTypeP2PKH
	case doge.DogeMainNetChain.P2SH_Address_Prefix:
	case doge.DogeTestNetChain.P2SH_Address_Prefix:
	case doge.DogeRegTestChain.P2SH_Address_Prefix:
	case doge.BitcoinMainChain.P2SH_Address_Prefix:
	case doge.BitcoinTestChain.P2SH_Address_Prefix:
		return doge.ScriptTypeP2SH
	case doge.DogeMainNetChain.PKey_Prefix:
	case doge.DogeTestNetChain.PKey_Prefix:
	case doge.DogeRegTestChain.PKey_Prefix:
	case doge.BitcoinMainChain.PKey_Prefix:
	case doge.BitcoinTestChain.PKey_Prefix:
		return doge.ScriptTypeP2PK
	}
	return doge.ScriptTypeNone
}
