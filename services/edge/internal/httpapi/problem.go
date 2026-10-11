package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// The message describes the client's own input, so it's safe to show.
func requestError(w http.ResponseWriter, _ *http.Request, err error) {
	writeProblem(w, http.StatusBadRequest, err.Error())
}

// Services own the rules, so their InvalidArgument messages are safe to show. Handlers
// must return service errors unwrapped: for a wrapped error, gRPC's message is the
// whole wrapped text.
func responseError(w http.ResponseWriter, r *http.Request, err error) {
	if status.Code(err) == codes.InvalidArgument {
		writeProblem(w, http.StatusBadRequest, status.Convert(err).Message())
		return
	}
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	writeProblem(w, http.StatusInternalServerError, "")
}

// The spec validator's error handler. Its 4xx messages describe the client's input;
// a 5xx message is about our side.
func validationError(w http.ResponseWriter, message string, code int) {
	if code >= http.StatusInternalServerError {
		log.Printf("validate request: %s", message)
		writeProblem(w, code, "")
		return
	}
	writeProblem(w, code, message)
}

// about:blank means the status code says it all, so the title is its standard text.
func writeProblem(w http.ResponseWriter, code int, detail string) {
	problem := api.Problem{
		Type:   "about:blank",
		Title:  http.StatusText(code),
		Status: int32(code),
	}
	if detail != "" {
		problem.Detail = &detail
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		log.Printf("write problem: %v", err)
	}
}
