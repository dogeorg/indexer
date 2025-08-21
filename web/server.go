package web

import (
	"context"
	"encoding/hex"
	"log"
	"net/http"
	"time"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/doge/koinu"
	"github.com/dogeorg/governor"
	"github.com/dogeorg/indexer/index"
	"github.com/dogeorg/indexer/spec"
)

func New(bind string, store spec.Store, indexer index.IndexerMonitor, corsOrigin string) governor.Service {
	mux := http.NewServeMux()
	a := &WebAPI{
		_store:     store,
		indexer:    indexer,
		corsOrigin: corsOrigin,
		srv: http.Server{
			Addr:    bind,
			Handler: mux,
		},
	}

	mux.HandleFunc("/health", a.healthCheck)
	mux.HandleFunc("/balance", a.getBalance)
	mux.HandleFunc("/utxo", a.getUtxo)
	mux.HandleFunc("/height", a.getHeight)
	mux.HandleFunc("/blocks", a.getRecentBlocks)

	return a
}

type WebAPI struct {
	governor.ServiceCtx
	_store     spec.Store
	store      spec.Store
	indexer    index.IndexerMonitor
	corsOrigin string
	srv        http.Server
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
		sendError(w, 500, "error", err.Error(), "GET", a.corsOrigin)
	} else {
		sendJson(w, map[string]interface{}{"ok": true}, "GET", a.corsOrigin)
	}
}

func (a *WebAPI) getBalance(w http.ResponseWriter, r *http.Request) {
	options := "GET, OPTIONS"
	switch r.Method {
	case http.MethodGet:
		address := r.URL.Query().Get("address")
		if address == "" {
			sendError(w, 400, "bad-request", "missing 'address' in the URL", options, a.corsOrigin)
			return
		}
		pubkeyHash, err := doge.Base58DecodeCheck(address)
		if err != nil {
			sendError(w, 400, "bad-request", "invalid Dogecoin address", options, a.corsOrigin)
			return
		}
		if len(pubkeyHash) != 21 {
			sendError(w, 400, "bad-request", "invalid Dogecoin address", options, a.corsOrigin)
			return
		}
		kind := utxoKindFromVersionByte(pubkeyHash[0])
		hash := pubkeyHash[1:]
		bal, err := a.store.GetBalance(kind, hash, 6)
		bal.Current = bal.Available + bal.Incoming
		if err != nil {
			sendError(w, 500, "error", err.Error(), options, a.corsOrigin)
		} else {
			sendJson(w, bal, options, a.corsOrigin)
		}
	case http.MethodOptions:
		sendOptions(w, r, options, a.corsOrigin)
	}
}

func (a *WebAPI) getUtxo(w http.ResponseWriter, r *http.Request) {
	options := "GET, OPTIONS"
	switch r.Method {
	case http.MethodGet:
		address := r.URL.Query().Get("address")
		if address == "" {
			sendError(w, 400, "bad-request", "missing 'address' in the URL", options, a.corsOrigin)
			return
		}
		pubkeyHash, err := doge.Base58DecodeCheck(address)
		if err != nil {
			sendError(w, 400, "bad-request", "invalid Dogecoin address", options, a.corsOrigin)
			return
		}
		if len(pubkeyHash) != 21 {
			sendError(w, 400, "bad-request", "invalid Dogecoin address", options, a.corsOrigin)
			return
		}
		kind := utxoKindFromVersionByte(pubkeyHash[0])
		hash := pubkeyHash[1:]
		list, err := a.store.FindUTXOs(kind, hash)
		if err != nil {
			sendError(w, 500, "error", err.Error(), options, a.corsOrigin)
		} else {
			utxo := []UTXOItem{}
			for _, u := range list {
				utxo = append(utxo, UTXOItem{
					TxID:   doge.HexEncodeReversed(u.TxID),
					VOut:   u.VOut,
					Value:  koinu.Koinu(u.Value),
					Type:   utxoKindStr(u.Type),
					Script: hex.EncodeToString(doge.ExpandScript(u.Type, u.Script)),
				})
			}
			sendJson(w, UTXOResponse{UTXO: utxo}, options, a.corsOrigin)
		}
	case http.MethodOptions:
		sendOptions(w, r, options, a.corsOrigin)
	}
}

func (a *WebAPI) getHeight(w http.ResponseWriter, r *http.Request) {
	options := "GET, OPTIONS"
	switch r.Method {
	case http.MethodGet:
		height, err := a.store.GetCurrentHeight()
		if err != nil {
			sendError(w, 500, "error", err.Error(), options, a.corsOrigin)
		} else {
			sendJson(w, map[string]interface{}{"height": height}, options, a.corsOrigin)
		}
	case http.MethodOptions:
		sendOptions(w, r, options, a.corsOrigin)
	}
}

func (a *WebAPI) getRecentBlocks(w http.ResponseWriter, r *http.Request) {
	options := "GET, OPTIONS"
	switch r.Method {
	case http.MethodGet:
		blocks := a.indexer.GetBlockHistory()
		sendJson(w, map[string]interface{}{"blocks": blocks}, options, a.corsOrigin)
	case http.MethodOptions:
		sendOptions(w, r, options, a.corsOrigin)
	}
}

type UTXOResponse struct {
	UTXO []UTXOItem `json:"utxo"`
}
type UTXOItem struct {
	TxID   string      `json:"tx"`     // hex-encoded transaction ID (byte-reversed)
	VOut   uint32      `json:"vout"`   // transaction output number
	Value  koinu.Koinu `json:"value"`  // UTXO value to 8 decimal places, as a decimal string
	Type   string      `json:"type"`   // UTXO type (determines what you need to sign it)
	Script string      `json:"script"` // hex-encoded UTXO locking script (needed to sign the UTXO)
}

func utxoKindFromVersionByte(version byte) doge.ScriptType {
	switch version {
	case doge.DogeMainNetChain.P2PKH_Address_Prefix,
		doge.DogeTestNetChain.P2PKH_Address_Prefix,
		doge.DogeRegTestChain.P2PKH_Address_Prefix,
		doge.BitcoinMainChain.P2PKH_Address_Prefix,
		doge.BitcoinTestChain.P2PKH_Address_Prefix:
		return doge.ScriptTypeP2PKH
	case doge.DogeMainNetChain.P2SH_Address_Prefix,
		doge.DogeTestNetChain.P2SH_Address_Prefix,
		doge.DogeRegTestChain.P2SH_Address_Prefix,
		doge.BitcoinMainChain.P2SH_Address_Prefix,
		doge.BitcoinTestChain.P2SH_Address_Prefix:
		return doge.ScriptTypeP2SH
	case doge.DogeMainNetChain.PKey_Prefix,
		doge.DogeTestNetChain.PKey_Prefix,
		doge.DogeRegTestChain.PKey_Prefix,
		doge.BitcoinMainChain.PKey_Prefix,
		doge.BitcoinTestChain.PKey_Prefix:
		return doge.ScriptTypeP2PK
	}
	return doge.ScriptTypeNone
}

func utxoKindStr(scriptType doge.ScriptType) string {
	switch scriptType {
	case doge.ScriptTypeNone:
		return "None"
	case doge.ScriptTypeP2PK:
		return "P2PK"
	case doge.ScriptTypeP2PKH:
		return "P2PKH"
	case doge.ScriptTypeP2SH:
		return "P2SH"
	case doge.ScriptTypeMultiSig:
		return "MultiSig"
	case doge.ScriptTypeP2PKHW:
		return "P2PKHW"
	case doge.ScriptTypeP2SHW:
		return "P2SHW"
	case doge.ScriptTypeNullData:
		return "NullData"
	case doge.ScriptTypeNonStandard:
		return "NonStandard"
	}
	return "None"
}
