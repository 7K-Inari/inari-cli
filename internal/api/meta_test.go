package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetContractMetadataNoTenantOrAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/contracts" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("unexpected auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pluginContracts":["inari.plugin.v1"],"uiHostApis":["inari.ui.v1"],"serverVersion":"4.1.0"}`))
	}))
	t.Cleanup(srv.Close)
	metadata, err := GetContractMetadata(context.Background(), srv.URL)
	if err != nil || metadata == nil || metadata.ServerVersion != "4.1.0" {
		t.Fatalf("metadata = %#v, %v", metadata, err)
	}
	if metadata.PluginContracts == nil || len(*metadata.PluginContracts) != 1 || metadata.UiHostApis == nil || len(*metadata.UiHostApis) != 1 {
		t.Fatalf("contract sets = %#v", metadata)
	}
}

func TestGetContractMetadataFailures(t *testing.T) {
	if metadata, err := GetContractMetadata(context.Background(), ""); err != nil || metadata != nil {
		t.Fatalf("empty server = %#v, %v", metadata, err)
	}
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"missing", `{"detail":"missing"}`, http.StatusNotFound},
		{"malformed", `{not json`, http.StatusOK},
		{"empty sets", `{"pluginContracts":[],"uiHostApis":[],"serverVersion":"dev"}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			if _, err := GetContractMetadata(context.Background(), srv.URL); err == nil {
				t.Fatal("expected non-fatal caller to receive metadata error")
			}
		})
	}
}
