package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shop "github.com/dvislobokov/protogen/genshop"
	"github.com/dvislobokov/protogen/rest"

	"buf.build/go/protovalidate"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func newGateway(t *testing.T) *httptest.Server {
	t.Helper()
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer(grpc.UnaryInterceptor(rest.ValidationInterceptor(v)))
	shop.RegisterCheckoutServer(gs, checkoutServer{})
	go gs.Serve(lis)
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	mux := runtime.NewServeMux(runtime.WithErrorHandler(rest.ProblemErrorHandler))
	if err := shop.RegisterCheckoutHandler(context.Background(), mux, conn); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestInvalidRequestReturnsProblemJSON(t *testing.T) {
	ts := newGateway(t)
	body := `{"customerEmail":"nope","customerName":"A","currency":0,"items":[],"acceptTerms":false}`
	resp, err := http.Post(ts.URL+"/v1/orders", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}

	var pd rest.ValidationProblemDetails
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &pd); err != nil {
		t.Fatalf("decode: %v (%s)", err, b)
	}
	if pd.Status != 400 || pd.Title == "" {
		t.Fatalf("unexpected problem details: %+v", pd)
	}
	// Keys must be camelCase (matching the JSON payload), not proto snake_case.
	for _, want := range []string{"customerEmail", "customerName", "currency", "items", "acceptTerms"} {
		if _, ok := pd.Errors[want]; !ok {
			t.Errorf("missing validation error for %q; got keys %v", want, keys(pd.Errors))
		}
	}
	if _, bad := pd.Errors["customer_email"]; bad {
		t.Errorf("errors use proto snake_case; expected camelCase")
	}
}

func TestValidRequestSucceeds(t *testing.T) {
	ts := newGateway(t)
	body := `{"customerEmail":"ada@example.com","customerName":"Ada","currency":"USD",
	          "idempotencyKey":"3f8b1c2e-1a2b-4c3d-9e8f-1234567890ab",
	          "items":[{"sku":"ABC-1","quantity":2,"unitPrice":9.99}],"acceptTerms":true,
	          "shippingAddress":{"line1":"1 Main St","city":"London","country":"GB","postalCode":"EC1A"}}`
	resp, err := http.Post(ts.URL+"/v1/orders", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", resp.StatusCode, b)
	}
	if !strings.Contains(string(b), "orderId") {
		t.Fatalf("unexpected body: %s", b)
	}
}

func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
