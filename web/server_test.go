package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/doge/koinu"
	"github.com/dogeorg/indexer/index"
	"github.com/dogeorg/indexer/spec"
)

// MockStore implements spec.Store for testing
type MockStore struct {
	balance       spec.Balance
	utxos         []spec.UTXO
	currentHeight int64
	resumePoint   []byte
	balanceErr    error
	utxoErr       error
	heightErr     error
	resumeErr     error
}

// MockIndexer implements index.IndexerMonitor for testing
type MockIndexer struct {
	blockHistory []index.BlockHistory
}

func (m *MockIndexer) GetBlockHistory() []index.BlockHistory {
	return m.blockHistory
}

func (m *MockStore) GetCurrentHeight() (int64, error) {
	return m.currentHeight, m.heightErr
}

func (m *MockStore) GetResumePoint() ([]byte, error) {
	return m.resumePoint, m.resumeErr
}

func (m *MockStore) GetBalance(kind doge.ScriptType, address []byte, confirmations int64) (spec.Balance, error) {
	return m.balance, m.balanceErr
}

func (m *MockStore) FindUTXOs(kind doge.ScriptType, address []byte) ([]spec.UTXO, error) {
	return m.utxos, m.utxoErr
}

// Implement other required methods with no-op implementations
func (m *MockStore) WithCtx(ctx context.Context) spec.Store {
	return m
}

func (m *MockStore) Begin() (spec.StoreTx, error) {
	return m, nil
}

func (m *MockStore) Commit() error {
	return nil
}

func (m *MockStore) Rollback() error {
	return nil
}

func (m *MockStore) Close() {
	// No-op for testing
}

func (m *MockStore) SetResumePoint(hash []byte, height int64) error {
	return nil
}

func (m *MockStore) RemoveUTXOs(removeUTXOs []spec.OutPointKey, height int64) error {
	return nil
}

func (m *MockStore) CreateUTXOs(createUTXOs []spec.UTXO, height int64) error {
	return nil
}

func (m *MockStore) UndoAbove(height int64) error {
	return nil
}

func (m *MockStore) TrimSpentUTXOs(height int64) error {
	return nil
}

func (m *MockStore) Transact(fn func(spec.StoreTx) error) error {
	return fn(m)
}

func TestUtxoKindFromVersionByte(t *testing.T) {
	tests := []struct {
		name        string
		versionByte byte
		expected    doge.ScriptType
	}{
		// P2PKH tests
		{
			name:        "DogeMainNet P2PKH",
			versionByte: doge.DogeMainNetChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		{
			name:        "DogeTestNet P2PKH",
			versionByte: doge.DogeTestNetChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		{
			name:        "DogeRegTest P2PKH",
			versionByte: doge.DogeRegTestChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		{
			name:        "BitcoinMain P2PKH",
			versionByte: doge.BitcoinMainChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		{
			name:        "BitcoinTest P2PKH",
			versionByte: doge.BitcoinTestChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		// P2SH tests
		{
			name:        "DogeMainNet P2SH",
			versionByte: doge.DogeMainNetChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		{
			name:        "DogeTestNet P2SH",
			versionByte: doge.DogeTestNetChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		{
			name:        "DogeRegTest P2SH",
			versionByte: doge.DogeRegTestChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		{
			name:        "BitcoinMain P2SH",
			versionByte: doge.BitcoinMainChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		{
			name:        "BitcoinTest P2SH",
			versionByte: doge.BitcoinTestChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		// P2PK tests
		{
			name:        "DogeMainNet P2PK",
			versionByte: doge.DogeMainNetChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		{
			name:        "DogeTestNet P2PK",
			versionByte: doge.DogeTestNetChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		{
			name:        "DogeRegTest P2PK",
			versionByte: doge.DogeRegTestChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		{
			name:        "BitcoinMain P2PK",
			versionByte: doge.BitcoinMainChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		{
			name:        "BitcoinTest P2PK",
			versionByte: doge.BitcoinTestChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		// Invalid/unrecognized bytes
		{
			name:        "Invalid byte",
			versionByte: 0xFF,
			expected:    doge.ScriptTypeNone,
		},
		{
			name:        "Zero byte (Bitcoin P2PKH)",
			versionByte: 0x00,
			expected:    doge.ScriptTypeP2PKH, // 0x00 matches Bitcoin P2PKH prefix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utxoKindFromVersionByte(tt.versionByte)
			if result != tt.expected {
				t.Errorf("utxoKindFromVersionByte(%#x) = %v, expected %v", tt.versionByte, result, tt.expected)
			}
		})
	}
}

func TestHealthCheck(t *testing.T) {
	tests := []struct {
		name           string
		resumeErr      error
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Healthy",
			resumeErr:      nil,
			expectedStatus: 200,
			expectedBody:   `{"ok":true}`,
		},
		{
			name:           "Unhealthy",
			resumeErr:      fmt.Errorf("database error"),
			expectedStatus: 500,
			expectedBody:   `{"error":"error","reason":"database error"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{resumeErr: tt.resumeErr}
			mockIndexer := &MockIndexer{}
			server := New(":0", mockStore, mockIndexer, "")
			webAPI := server.(*WebAPI)
			webAPI.store = mockStore

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			webAPI.healthCheck(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if w.Body.String() != tt.expectedBody {
				t.Errorf("expected body %q, got %q", tt.expectedBody, w.Body.String())
			}
		})
	}
}

func TestGetHeight(t *testing.T) {
	tests := []struct {
		name           string
		height         int64
		heightErr      error
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Success",
			height:         123456,
			heightErr:      nil,
			expectedStatus: 200,
			expectedBody:   `{"height":123456}`,
		},
		{
			name:           "Zero height",
			height:         0,
			heightErr:      nil,
			expectedStatus: 200,
			expectedBody:   `{"height":0}`,
		},
		{
			name:           "Database error",
			height:         0,
			heightErr:      fmt.Errorf("connection failed"),
			expectedStatus: 500,
			expectedBody:   `{"error":"error","reason":"connection failed"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{
				currentHeight: tt.height,
				heightErr:     tt.heightErr,
			}
			mockIndexer := &MockIndexer{}
			server := New(":0", mockStore, mockIndexer, "")
			webAPI := server.(*WebAPI)
			webAPI.store = mockStore

			req := httptest.NewRequest("GET", "/height", nil)
			w := httptest.NewRecorder()

			webAPI.getHeight(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if w.Body.String() != tt.expectedBody {
				t.Errorf("expected body %q, got %q", tt.expectedBody, w.Body.String())
			}
		})
	}
}

func TestGetHeightOptions(t *testing.T) {
	mockStore := &MockStore{currentHeight: 123456}
	mockIndexer := &MockIndexer{}
	server := New(":0", mockStore, mockIndexer, "")
	webAPI := server.(*WebAPI)
	webAPI.store = mockStore

	req := httptest.NewRequest("OPTIONS", "/height", nil)
	w := httptest.NewRecorder()

	webAPI.getHeight(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}

	if w.Header().Get("Allow") != "GET, OPTIONS" {
		t.Errorf("expected Allow header 'GET, OPTIONS', got %q", w.Header().Get("Allow"))
	}
}

func TestGetBalance(t *testing.T) {
	validAddress := "D7nTLrBUiso28mNBj8MyHoyjdFypz3NzRS"
	validBalance := spec.Balance{
		Available: koinu.Koinu(100000000), // 1.0 DOGE in satoshis
		Incoming:  koinu.Koinu(50000000),  // 0.5 DOGE in satoshis
		Outgoing:  koinu.Koinu(0),
		Current:   koinu.Koinu(150000000), // 1.5 DOGE in satoshis
	}

	tests := []struct {
		name           string
		method         string
		address        string
		balance        spec.Balance
		balanceErr     error
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Valid address",
			method:         "GET",
			address:        validAddress,
			balance:        validBalance,
			balanceErr:     nil,
			expectedStatus: 200,
			expectedBody:   `{"incoming":"0.5","available":"1","outgoing":"0","current":"1.5"}`,
		},
		{
			name:           "Missing address",
			method:         "GET",
			address:        "",
			balance:        spec.Balance{},
			balanceErr:     nil,
			expectedStatus: 400,
			expectedBody:   `{"error":"bad-request","reason":"missing 'address' in the URL"}`,
		},
		{
			name:           "Invalid address",
			method:         "GET",
			address:        "invalid-address",
			balance:        spec.Balance{},
			balanceErr:     nil,
			expectedStatus: 400,
			expectedBody:   `{"error":"bad-request","reason":"invalid Dogecoin address"}`,
		},
		{
			name:           "Database error",
			method:         "GET",
			address:        validAddress,
			balance:        spec.Balance{},
			balanceErr:     fmt.Errorf("database error"),
			expectedStatus: 500,
			expectedBody:   `{"error":"error","reason":"database error"}`,
		},
		{
			name:           "OPTIONS method",
			method:         "OPTIONS",
			address:        validAddress,
			balance:        spec.Balance{},
			balanceErr:     nil,
			expectedStatus: http.StatusNoContent,
			expectedBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{
				balance:    tt.balance,
				balanceErr: tt.balanceErr,
			}
			mockIndexer := &MockIndexer{}
			server := New(":0", mockStore, mockIndexer, "")
			webAPI := server.(*WebAPI)
			webAPI.store = mockStore

			url := "/balance"
			if tt.address != "" {
				url += "?address=" + tt.address
			}
			req := httptest.NewRequest(tt.method, url, nil)
			w := httptest.NewRecorder()

			webAPI.getBalance(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.method == "OPTIONS" {
				if w.Header().Get("Allow") != "GET, OPTIONS" {
					t.Errorf("expected Allow header 'GET, OPTIONS', got %q", w.Header().Get("Allow"))
				}
			} else if w.Body.String() != tt.expectedBody {
				t.Errorf("expected body %q, got %q", tt.expectedBody, w.Body.String())
			}
		})
	}
}

func TestGetUtxo(t *testing.T) {
	validAddress := "D7nTLrBUiso28mNBj8MyHoyjdFypz3NzRS"
	validUtxos := []spec.UTXO{
		{
			TxID:   []byte{1, 2, 3, 4},
			VOut:   0,
			Value:  100000000, // 1 DOGE
			Type:   doge.ScriptTypeP2PKH,
			Script: []byte{0x76, 0xA9, 0x14, 0x88, 0xAC},
		},
	}

	tests := []struct {
		name           string
		method         string
		address        string
		utxos          []spec.UTXO
		utxoErr        error
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Valid address",
			method:         "GET",
			address:        validAddress,
			utxos:          validUtxos,
			utxoErr:        nil,
			expectedStatus: 200,
			expectedBody:   `{"utxo":[{"tx":"04030201","vout":0,"value":"1","type":"P2PKH","script":"76a91476a91488ac00000000000000000000000000000088ac"}]}`,
		},
		{
			name:           "Missing address",
			method:         "GET",
			address:        "",
			utxos:          nil,
			utxoErr:        nil,
			expectedStatus: 400,
			expectedBody:   `{"error":"bad-request","reason":"missing 'address' in the URL"}`,
		},
		{
			name:           "Invalid address",
			method:         "GET",
			address:        "invalid-address",
			utxos:          nil,
			utxoErr:        nil,
			expectedStatus: 400,
			expectedBody:   `{"error":"bad-request","reason":"invalid Dogecoin address"}`,
		},
		{
			name:           "Database error",
			method:         "GET",
			address:        validAddress,
			utxos:          nil,
			utxoErr:        fmt.Errorf("database error"),
			expectedStatus: 500,
			expectedBody:   `{"error":"error","reason":"database error"}`,
		},
		{
			name:           "OPTIONS method",
			method:         "OPTIONS",
			address:        validAddress,
			utxos:          nil,
			utxoErr:        nil,
			expectedStatus: http.StatusNoContent,
			expectedBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{
				utxos:   tt.utxos,
				utxoErr: tt.utxoErr,
			}
			mockIndexer := &MockIndexer{}
			server := New(":0", mockStore, mockIndexer, "")
			webAPI := server.(*WebAPI)
			webAPI.store = mockStore

			url := "/utxo"
			if tt.address != "" {
				url += "?address=" + tt.address
			}
			req := httptest.NewRequest(tt.method, url, nil)
			w := httptest.NewRecorder()

			webAPI.getUtxo(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.method == "OPTIONS" {
				if w.Header().Get("Allow") != "GET, OPTIONS" {
					t.Errorf("expected Allow header 'GET, OPTIONS', got %q", w.Header().Get("Allow"))
				}
			} else if w.Body.String() != tt.expectedBody {
				t.Errorf("expected body %q, got %q", tt.expectedBody, w.Body.String())
			}
		})
	}
}

func TestHeightEndpointIntegration(t *testing.T) {
	mockStore := &MockStore{currentHeight: 123456}
	mockIndexer := &MockIndexer{}
	server := New(":0", mockStore, mockIndexer, "")
	webAPI := server.(*WebAPI)
	webAPI.store = mockStore

	// Test that the endpoint is accessible
	req := httptest.NewRequest("GET", "/height", nil)
	w := httptest.NewRecorder()

	// Use the actual HTTP handler
	webAPI.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if height, ok := response["height"].(float64); !ok {
		t.Errorf("expected height field in response, got %T", response["height"])
	} else if int64(height) != 123456 {
		t.Errorf("expected height 123456, got %d", int64(height))
	}
}

func TestGetRecentBlocks(t *testing.T) {
	mockStore := &MockStore{}
	mockIndexer := &MockIndexer{
		blockHistory: []index.BlockHistory{
			{
				Height:         123456,
				Hash:           "abc123",
				Timestamp:      time.Now(),
				TxCount:        150,
				UTXOCreated:    200,
				UTXOSpent:      50,
				ProcessingTime: 100 * time.Millisecond,
			},
		},
	}

	server := New(":0", mockStore, mockIndexer, "")
	webAPI := server.(*WebAPI)
	webAPI.store = mockStore

	req := httptest.NewRequest("GET", "/blocks", nil)
	w := httptest.NewRecorder()

	webAPI.getRecentBlocks(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if blocks, ok := response["blocks"].([]interface{}); !ok {
		t.Errorf("expected blocks field in response, got %T", response["blocks"])
	} else if len(blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(blocks))
	}
}

func TestGetRecentBlocksOptions(t *testing.T) {
	mockStore := &MockStore{}
	mockIndexer := &MockIndexer{}

	server := New(":0", mockStore, mockIndexer, "")
	webAPI := server.(*WebAPI)
	webAPI.store = mockStore

	req := httptest.NewRequest("OPTIONS", "/blocks", nil)
	w := httptest.NewRecorder()

	webAPI.getRecentBlocks(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}

	if w.Header().Get("Allow") != "GET, OPTIONS" {
		t.Errorf("expected Allow header 'GET, OPTIONS', got %q", w.Header().Get("Allow"))
	}
}
