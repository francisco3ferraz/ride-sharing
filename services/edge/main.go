package main

import (
	"log"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/httpapi"
)

const (
	listenAddr  = ":8080"
	pricingAddr = "localhost:50051"
)

func main() {
	// NewClient doesn't connect yet. It connects on the first call and reconnects
	// on its own, so one long-lived connection serves every request.
	conn, err := grpc.NewClient(pricingAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("create pricing client: %v", err)
	}

	handler := httpapi.NewHandler(pricingv1.NewPricingServiceClient(conn))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/fare-estimate", handler.FareEstimate)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: mux,
		// Stops a client from holding a connection open by sending headers very slowly.
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("edge listening on %s", listenAddr)
	log.Fatal(srv.ListenAndServe())
}
