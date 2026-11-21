package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func observabilityPlan(body map[string]any, state string) map[string]any {
	source := body["source"]
	if source == nil {
		source = map[string]any{"kind": "catalog", "itemId": "hiroba/payments-api", "version": "1.4.0"}
	}
	deployment := body["deployment"]
	if deployment == nil {
		deployment = map[string]any{"targetId": "", "targetRevision": 0, "spec": map[string]any{}, "ownerTeam": "payments-api-maintainers", "namespace": "payments"}
	}
	plan := map[string]any{
		"id": "dp-1", "orgId": "acme", "orgSlug": "acme", "operation": body["operation"], "source": source, "deployment": deployment,
		"app": map[string]any{"itemId": "hiroba/payments-api", "version": "1.4.0", "digest": "sha256:abc"},
		"capability": map[string]any{
			"apiVersion": "inari.dev/v1alpha1", "kind": "Observability", "app": "payments-api", "folder": "payments-api",
			"datasources": map[string]string{"prometheus": "${DS_PROMETHEUS}"}, "linkVariables": []string{"namespace"},
			"dashboards": []map[string]any{{"title": "Service overview", "uid": "payments-api-overview", "file": "dashboards/overview.json"}},
			"rules":      []map[string]any{{"file": "rules/availability.yaml", "evaluation": "central"}}, "targets": []string{"central"}, "severityRouting": map[string]string{"critical": "platform-security"},
			"minimumAppVersion": ">=1.3.0 <2.0.0", "observabilityPack": map[string]any{"package": "observability-pack", "versionRange": ">=0.2.0 <0.3.0"},
		},
		"capabilityDigest": "sha256:cap", "observability": map[string]any{"mode": "provision"},
		"planFingerprint": "sha256:plan", "state": state, "gate": map[string]any{"companionRendering": false, "deployedVerification": false},
		"createdAt": "2026-11-15T21:42:00Z", "updatedAt": "2026-11-15T21:42:00Z",
	}
	if requested, ok := body["observability"].(map[string]any); ok && body["operation"] != "provisionExisting" {
		if requested["mode"] == "" || requested["mode"] == nil {
			requested["mode"] = "skip"
		}
		plan["observability"] = requested
	}
	if body["operation"] == "provisionExisting" {
		plan["appInstanceId"] = "app-1"
		deployment := plan["deployment"].(map[string]any)
		deployment["ownerTeam"] = "payments-api-maintainers"
		deployment["namespace"] = "payments"
		if requested, ok := body["observability"].(map[string]any); ok {
			plan["observability"] = requested
		}
	}
	obs := plan["observability"].(map[string]any)
	if obs["mode"] == "provision" {
		obs["pack"] = map[string]any{"itemId": "hiroba/observability-pack", "version": "0.2.0", "digest": "sha256:pack", "renderContract": "inari.observability-pack/v2"}
		obs["target"] = map[string]any{"id": "rt-mon", "revision": 7, "selection": map[string]any{"folder": "acme-inc/payments-api", "datasource": "metrics-central", "evaluation": "central"}}
	}
	if state == "submitted" && obs["mode"] == "provision" {
		plan["appInstanceId"] = "app-1"
		plan["companion"] = map[string]any{
			"id": "oc-1", "planId": "dp-1", "appInstanceId": "app-1", "appRevision": "1.4.0@sha256:abc", "appSourceDigest": "sha256:abc", "capabilityDigest": "sha256:cap",
			"generation": 3, "targetId": "rt-mon", "targetRevision": 7, "targetClusterId": "cluster-mon", "targetSelection": obs["target"].(map[string]any)["selection"],
			"pack": obs["pack"], "state": "blocked_external_gate", "links": []any{}, "createdAt": "2026-11-15T21:42:00Z", "updatedAt": "2026-11-15T21:42:00Z",
		}
	}
	return plan
}

func observabilityServer(t *testing.T, ready bool) *httptest.Server {
	t.Helper()
	return observabilityServerWithSubmit(t, ready, nil)
}

func observabilityServerWithSubmit(t *testing.T, ready bool, onSubmit func(map[string]any)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/deployment-plans/preflight", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		plan := observabilityPlan(body, "preflight")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"preflight": map[string]any{"plan": plan, "schema": map[string]any{"type": "object"}, "compatiblePacks": []any{plan["observability"].(map[string]any)["pack"]},
				"monitoringTargets": []any{map[string]any{"id": "rt-mon", "revision": 7}}, "deploymentTargets": []any{map[string]any{"id": "rt-clu-1", "resourceRef": map[string]any{"id": "clu-1"}, "revision": 1}}, "providers": []any{}},
		})
	})
	mux.HandleFunc("/api/v1/tenants/acme/deployment-plans", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if onSubmit != nil {
			onSubmit(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"plan": observabilityPlan(body, "submitted")})
	})
	mux.HandleFunc("/api/v1/tenants/acme/instances/app-1/observability", func(w http.ResponseWriter, r *http.Request) {
		plan := observabilityPlan(map[string]any{"operation": "provisionExisting"}, "submitted")
		companion := plan["companion"].(map[string]any)
		companion["state"] = "verification_pending"
		companion["links"] = []any{}
		if ready {
			companion["state"] = "ready"
			companion["links"] = []any{map[string]any{"title": "Service overview", "uid": "payments-api-overview", "url": "https://grafana.team.example/d/payments-api-overview/overview?var-namespace=payments"}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"observability": map[string]any{"mode": "provision", "available": true, "companion": companion}})
	})
	return httptest.NewServer(mux)
}

func TestDeployChartAndTemplateUseCommonPlan(t *testing.T) {
	var submitted map[string]any
	srv := observabilityServerWithSubmit(t, false, func(body map[string]any) { submitted = body })
	defer srv.Close()
	for _, args := range [][]string{
		{"deploy", "chart", "oci://ghcr.io/7k-hiroba/payments-api/charts/payments-api", "--source", "hiroba", "--version", "1.4.0", "--digest", "sha256:abc", "--cluster", "clu-1", "--owner-team", "payments-api-maintainers", "--observability", "provision"},
		{"deploy", "template", "hiroba/service", "--name", "payments", "--owner-team", "payments-api-maintainers", "--observability", "provision"},
	} {
		out := setupAuthedContext(t, srv.URL)
		root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out.String(), "blocked_external_gate") {
			t.Fatalf("output = %q", out.String())
		}
		observability := submitted["observability"].(map[string]any)
		target := observability["target"].(map[string]any)
		if observability["mode"] != "provision" || target["id"] != "rt-mon" {
			t.Fatalf("policy-default observability target was not submitted: %#v", observability)
		}
	}
}

func TestExistingObservabilityProvisionDefaultsToProvision(t *testing.T) {
	var submitted map[string]any
	srv := observabilityServerWithSubmit(t, false, func(body map[string]any) { submitted = body })
	defer srv.Close()
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"resources", "observability", "provision", "app-1"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	observability := submitted["observability"].(map[string]any)
	if observability["mode"] != "provision" {
		t.Fatalf("existing-instance provision submitted mode %q", observability["mode"])
	}
	if submitted["operation"] != "provisionExisting" || submitted["instanceId"] != "app-1" {
		t.Fatalf("existing-instance submit body = %#v", submitted)
	}
}

func TestObservabilityStatusPrintsLinksOnlyWhenReady(t *testing.T) {
	for _, tc := range []struct{ ready, want bool }{{false, false}, {true, true}} {
		srv := observabilityServer(t, tc.ready)
		out := setupAuthedContext(t, srv.URL)
		root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
		root.SetArgs([]string{"resources", "observability", "status", "app-1"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		contains := strings.Contains(out.String(), "https://grafana.team.example/d/payments-api-overview/overview?var-namespace=payments")
		if contains != tc.want {
			t.Fatalf("ready=%t output=%q", tc.ready, out.String())
		}
		srv.Close()
	}
}
