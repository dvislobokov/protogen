package grpcx_test

import (
	"context"
	"net"
	"testing"

	"github.com/dvislobokov/protogen/grpcx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestRegisterHealthAndReflection(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	grpcx.Register(s)
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	// Reflection and health services must be registered on the server.
	info := s.GetServiceInfo()
	for _, want := range []string{"grpc.health.v1.Health", "grpc.reflection.v1.ServerReflection"} {
		if _, ok := info[want]; !ok {
			t.Errorf("service %q not registered; have %v", want, keys(info))
		}
	}

	// The health check must report SERVING for the overall server.
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	resp, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health status = %v, want SERVING", resp.GetStatus())
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
