package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func verificationPolicyFixture() map[string]any {
	return map[string]any{
		"policyRevision": 1, "enabled": true,
		"binding": map[string]any{
			"appItemId": "hiroba/example", "appVersion": "1.0.0",
			"appArtifactDigest": "sha256:" + strings.Repeat("a", 64), "specDigest": "sha256:" + strings.Repeat("b", 64),
			"suiteGrantId": "vsg-1", "suiteDigest": "sha256:" + strings.Repeat("c", 64),
			"profile": "stage", "parameterContract": "inari.smoke.params.v1",
		},
		"triggers": map[string]any{"onRevisionChange": map[string]any{"enabled": true}, "onSync": map[string]any{"enabled": false}, "manual": map[string]any{"enabled": true}, "cron": map[string]any{"enabled": false}},
		"timeout":  "10m", "concurrency": map[string]any{"maxActive": 1, "maxPending": 1, "queuedRevisionPolicy": "latestOnly"},
		"retention":   map[string]any{"workflowTTLSeconds": 3600, "successfulHistory": 3, "failedHistory": 3, "resultDays": 30},
		"addonAbsent": "block", "failureAction": "pausePromotion",
	}
}

func writeVerificationPolicy(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	raw, _ := json.Marshal(verificationPolicyFixture())
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerificationSetDryRunDoesNotPut(t *testing.T) {
	var previewCalled, putCalled bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/instances/inst-1/verification/preview", func(w http.ResponseWriter, r *http.Request) {
		previewCalled = true
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"policy": body, "gate": map[string]any{"renderAvailable": false, "deployedVerification": false}, "sideEffectsPerformed": 0})
	})
	mux.HandleFunc("/api/v1/tenants/acme/instances/inst-1/verification", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verificationPolicyFixture())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"verification", "set", "inst-1", "--file", writeVerificationPolicy(t), "--dry-run", "-o", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("verification dry-run: %v", err)
	}
	if !previewCalled || putCalled {
		t.Fatalf("preview=%v put=%v", previewCalled, putCalled)
	}
}

func TestVerificationRunDryRunAndRunLinks(t *testing.T) {
	var runCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/instances/inst-1/verification/runs", func(w http.ResponseWriter, r *http.Request) {
		runCalls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["dryRun"] != true || body["idempotencyKey"] == "" {
			t.Errorf("run dry-run body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"preview": map[string]any{"gate": map[string]any{"renderAvailable": false, "deployedVerification": false}, "sideEffectsPerformed": 0}})
	})
	mux.HandleFunc("/api/v1/tenants/acme/instances/inst-1/verification/runs/vr-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"runId": "vr-1", "runKey": "sha256:key", "orgId": "org:1", "instanceId": "inst-1", "instanceUid": "uid",
			"appRevision": "sha256:rev", "appArtifactDigest": "sha256:app", "suiteDigest": "sha256:suite", "profile": "stage",
			"trigger": map[string]any{"kind": "manual", "profile": "stage", "requestKey": "req"}, "triggerFingerprint": "sha256:trigger",
			"status": "failed", "policyRevision": 1, "grantRevision": 1, "latestAttempt": 1,
			"createdAt": "2026-11-21T01:00:00Z", "updatedAt": "2026-11-21T01:00:00Z",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"verification", "run", "inst-1", "--dry-run", "-o", "json"})
	if err := root.Execute(); err != nil || runCalls != 1 {
		t.Fatalf("run dry-run = %v, calls=%d", err, runCalls)
	}
	out.Reset()
	root.SetArgs([]string{"verification", "run-status", "inst-1", "vr-1", "-o", "table"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "https://") || !strings.Contains(out.String(), "Links:       none") {
		t.Fatalf("non-ready run fabricated links: %q", out.String())
	}
}
