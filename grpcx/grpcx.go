// Package grpcx bundles the two things almost every gRPC server wants but that
// aren't generated: server reflection (so grpcurl / Postman can discover the
// API) and the standard health service.
package grpcx

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Register enables server reflection and the health service on s, and marks the
// overall server (the empty service name) as SERVING. The returned *health.Server
// lets you flip per-service status as dependencies become (un)available:
//
//	hs := grpcx.Register(s)
//	hs.SetServingStatus("shop.v1.Checkout", healthpb.HealthCheckResponse_NOT_SERVING)
func Register(s *grpc.Server) *health.Server {
	reflection.Register(s)

	hs := health.NewServer()
	healthpb.RegisterHealthServer(s, hs)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	return hs
}
