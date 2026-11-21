package scaffold

import "net/http"

// HTTPHandler exposes only synthetic endpoints for the ephemeral chart/smoke lane.
func HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/ping", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	return mux
}
