package httpapi

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// The message describes the client's own input, so it's safe to show.
func requestError(w http.ResponseWriter, _ *http.Request, err error) {
	writeProblem(w, http.StatusBadRequest, err.Error())
}

// Services own the rules, so their messages for these codes are written for the client
// and safe to show. Every other code, and any error that isn't from a service, is a 500.
var clientErrorStatuses = map[codes.Code]int{
	codes.InvalidArgument: http.StatusBadRequest,
	codes.Unauthenticated: http.StatusUnauthorized,
	codes.AlreadyExists:   http.StatusConflict,
}

// Handlers must return service errors unwrapped: for a wrapped error, gRPC's message
// is the whole wrapped text.
func responseError(w http.ResponseWriter, r *http.Request, err error) {
	if httpStatus, ok := clientErrorStatuses[status.Code(err)]; ok {
		writeProblem(w, httpStatus, status.Convert(err).Message())
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

// tooManyRequests answers 429 for a used-up rate limit. Retry-After counts whole
// seconds, so the wait is rounded up: rounded down, a client would retry straight into
// another 429.
func tooManyRequests(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := max(1, int(math.Ceil(retryAfter.Seconds())))
	// Before writeProblem writes the status line, after which a new header is dropped.
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeProblem(w, http.StatusTooManyRequests, "")
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
