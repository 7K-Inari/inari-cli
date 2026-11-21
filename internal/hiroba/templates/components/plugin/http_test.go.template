package scaffold

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPHandler(t *testing.T) {
	for _, path := range []string{"/healthz", "/api/ping"} {
		recorder := httptest.NewRecorder()
		HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, recorder.Code)
		}
	}
}
