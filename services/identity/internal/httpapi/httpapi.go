// Package httpapi serves the JWKS, which the Edge fetches to verify access tokens. It
// isn't traced: the Edge fetches it in the background, so each fetch would be a trace
// of its own, every few minutes.
package httpapi

import (
	"log"
	"net/http"
)

// Routes serves jwks as it is. The key can't change while Identity runs, so it's
// marshalled once, at startup.
func Routes(jwks []byte) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(jwks); err != nil {
			log.Printf("write jwks: %v", err)
		}
	})
	return mux
}
