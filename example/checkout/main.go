// Command checkout demonstrates the full stack for one POST endpoint:
//
//	HTTP/JSON  ->  grpc-gateway  ->  gRPC (protovalidate interceptor)  ->  handler
//
// A bad request comes back as ASP.NET Core-style problem+json (HTTP 400 with a
// field->messages "errors" map); a valid one returns 200.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"

	shop "github.com/dvislobokov/protogen/genshop"
	"github.com/dvislobokov/protogen/grpcx"
	"github.com/dvislobokov/protogen/rest"

	"buf.build/go/protovalidate"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type checkoutServer struct {
	shop.UnimplementedCheckoutServer
}

func (checkoutServer) PlaceOrder(ctx context.Context, in *shop.PlaceOrderRequest) (*shop.PlaceOrderResponse, error) {
	return &shop.PlaceOrderResponse{OrderId: "ord_123", Currency: in.GetCurrency()}, nil
}

func main() {
	// --- gRPC server on an in-process bufconn, with the validation interceptor.
	v, err := protovalidate.New()
	if err != nil {
		panic(err)
	}
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer(grpc.UnaryInterceptor(rest.ValidationInterceptor(v)))
	shop.RegisterCheckoutServer(gs, checkoutServer{})
	grpcx.Register(gs) // server reflection + health service
	go gs.Serve(lis)
	defer gs.Stop()

	// --- client connection to that server.
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// --- gateway mux with the ASP.NET-style problem+json error handler.
	mux := runtime.NewServeMux(runtime.WithErrorHandler(rest.ProblemErrorHandler))
	if err := shop.RegisterCheckoutHandler(context.Background(), mux, conn); err != nil {
		panic(err)
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	bad := `{
	  "customerEmail": "not-an-email",
	  "customerName": "A",
	  "currency": 0,
	  "items": [],
	  "discountPercent": 250,
	  "acceptTerms": false
	}`
	good := `{
	  "customerEmail": "ada@example.com",
	  "customerName": "Ada Lovelace",
	  "idempotencyKey": "3f8b1c2e-1a2b-4c3d-9e8f-1234567890ab",
	  "currency": 1,
	  "items": [{"sku": "ABC-1", "quantity": 2, "unitPrice": 9.99}],
	  "discountPercent": 10,
	  "acceptTerms": true,
	  "shippingAddress": {"line1": "1 Main St", "city": "London", "country": "GB", "postalCode": "EC1A"}
	}`

	post(ts.URL, "INVALID request", bad)
	post(ts.URL, "VALID request", good)
}

func post(base, label, body string) {
	resp, err := http.Post(base+"/v1/orders", "application/json", strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Printf("\n=== %s ===\nHTTP %d  (%s)\n%s\n", label, resp.StatusCode, resp.Header.Get("Content-Type"), b)
}
