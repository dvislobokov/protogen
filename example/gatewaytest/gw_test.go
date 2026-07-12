package gatewaytest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	greeter "github.com/dvislobokov/protogen/gen"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

type server struct {
	greeter.UnimplementedGreeterServer
}

func (server) SayHello(ctx context.Context, in *greeter.HelloRequest) (*greeter.HelloReply, error) {
	return &greeter.HelloReply{Message: fmt.Sprintf("hello, %s x%d", in.GetName(), in.GetTimes())}, nil
}

func (server) GetGreeting(ctx context.Context, in *greeter.GetGreetingRequest) (*greeter.HelloReply, error) {
	return &greeter.HelloReply{Message: "greetings, " + in.GetName()}, nil
}

// TestGatewayRoundTrip drives the generated *.pb.gw.go end to end: REST in,
// gRPC handler out, JSON back — all in-process.
func TestGatewayRoundTrip(t *testing.T) {
	mux := runtime.NewServeMux()
	if err := greeter.RegisterGreeterHandlerServer(context.Background(), mux, server{}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	cases := []struct {
		name, method, path, body, want string
	}{
		{"post body", http.MethodPost, "/v1/greeter/hello", `{"name":"Ada","times":2}`, "hello, Ada x2"},
		{"get path param", http.MethodGet, "/v1/greeter/World", "", "greetings, World"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, b)
			}
			if !strings.Contains(string(b), tc.want) {
				t.Fatalf("body %q does not contain %q", b, tc.want)
			}
			t.Logf("%s %s -> %s", tc.method, tc.path, b)
		})
	}
}
