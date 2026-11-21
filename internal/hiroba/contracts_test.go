package hiroba

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func contractServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func validContractHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/contracts" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("meta/contracts unexpectedly requires auth")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ContractSet{PluginContracts: []string{"inari.plugin.v1"}, UIHostAPIs: []string{"inari.ui.v1"}, ServerVersion: "1.0.0"})
	}
}

func TestContractUsesGeneratedMetaClient(t *testing.T) {
	data, err := os.ReadFile("contracts.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "GetMetaContractsWithResponse") {
		t.Fatal("meta contract check must use the generated inari-api client")
	}
	if strings.Contains(text, "http.NewRequest") || strings.Contains(text, "/api/v1/meta/contracts\"") {
		t.Fatal("hand-rolled meta endpoint transport remains")
	}
}

func TestContractWarningCompatible(t *testing.T) {
	requests := 0
	server := contractServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		validContractHandler(t)(w, r)
	})
	out := &bytes.Buffer{}
	WarnOnContractMismatch(context.Background(), server.URL, Options{WithPlugin: true, WithUI: true}, out)
	if out.String() != "" {
		t.Fatalf("compatible contracts warned: %s", out)
	}
	if requests != 1 {
		t.Fatalf("compatible scaffold made %d meta requests, want 1", requests)
	}
}

func TestContractWarningEmptyIntersection(t *testing.T) {
	server := contractServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ContractSet{PluginContracts: []string{"inari.plugin.v2"}, UIHostAPIs: []string{"inari.ui.v2"}, ServerVersion: "2.0.0"})
	})
	out := &bytes.Buffer{}
	WarnOnContractMismatch(context.Background(), server.URL, Options{WithPlugin: true, WithUI: true}, out)
	for _, expected := range []string{"inari.plugin.v1", "inari.plugin.v2", "inari.ui.v1", "inari.ui.v2"} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("warning missing %q: %s", expected, out)
		}
	}
}

func TestContractWarningBackendPreset(t *testing.T) {
	server := contractServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ContractSet{PluginContracts: []string{"inari.plugin.v2"}, UIHostAPIs: []string{"inari.ui.v1"}, ServerVersion: "2.0.0"})
	})
	out := &bytes.Buffer{}
	WarnOnContractMismatch(context.Background(), server.URL, Options{Type: "extension"}, out)
	if !strings.Contains(out.String(), "inari.plugin.v1") {
		t.Fatalf("extension preset did not check backend contract: %s", out)
	}
}

func TestContractCheckUnavailableIsNonfatal(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "missing endpoint", handler: http.NotFound},
		{name: "malformed", handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"pluginContracts":`)) }},
		{name: "empty", handler: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(ContractSet{PluginContracts: []string{}, UIHostAPIs: []string{"inari.ui.v1"}, ServerVersion: "1"})
		}},
		{name: "duplicate", handler: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(ContractSet{PluginContracts: []string{"inari.plugin.v1", "inari.plugin.v1"}, UIHostAPIs: []string{"inari.ui.v1"}, ServerVersion: "1"})
		}},
		{name: "unsorted", handler: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(ContractSet{PluginContracts: []string{"inari.plugin.v2", "inari.plugin.v1"}, UIHostAPIs: []string{"inari.ui.v1"}, ServerVersion: "1"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := contractServer(t, test.handler)
			out := &bytes.Buffer{}
			WarnOnContractMismatch(context.Background(), server.URL, Options{WithPlugin: true}, out)
			if !strings.Contains(out.String(), "warning") || !strings.Contains(out.String(), "continuing") {
				t.Fatalf("expected nonfatal warning, got %q", out)
			}
		})
	}
}

func TestContractCheckTimeout(t *testing.T) {
	started := time.Now()
	server := contractServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		validContractHandler(t)(w, r)
	})
	out := &bytes.Buffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	WarnOnContractMismatch(ctx, server.URL, Options{WithUI: true}, out)
	if time.Since(started) > 90*time.Millisecond {
		t.Fatalf("contract check did not respect bounded context: %s", time.Since(started))
	}
	if !strings.Contains(out.String(), "continuing") {
		t.Fatalf("timeout did not produce nonfatal warning: %s", out)
	}
}
