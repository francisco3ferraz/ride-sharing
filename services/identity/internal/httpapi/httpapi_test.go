package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

const jwks = `{"keys":[{"kty":"OKP"}]}`

func TestRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{name: "the jwks", method: http.MethodGet, path: "/.well-known/jwks.json", wantStatus: http.StatusOK, wantBody: jwks},
		{name: "the jwks with another method", method: http.MethodPost, path: "/.well-known/jwks.json", wantStatus: http.StatusMethodNotAllowed},
		{name: "another path", method: http.MethodGet, path: "/jwks.json", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Routes([]byte(jwks)).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				assert.Equal(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}
