package main

import (
	"log"
	"net"

	"google.golang.org/grpc"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/pricing/internal/grpcapi"
)

const listenAddr = ":50051"

func main() {
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("listen on %s: %v", listenAddr, err)
	}

	srv := grpc.NewServer()
	pricingv1.RegisterPricingServiceServer(srv, &grpcapi.Server{})

	log.Printf("pricing listening on %s", listenAddr)
	log.Fatal(srv.Serve(lis))
}
