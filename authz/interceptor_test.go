// External test package: gen/greeter.pb.go imports authz (for the extension
// descriptors), so an in-package test would create an import cycle.
package authz_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/dvislobokov/protogen/authz"
	greeter "github.com/dvislobokov/protogen/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type server struct {
	greeter.UnimplementedGreeterServer
}

func (server) SayHello(ctx context.Context, in *greeter.HelloRequest) (*greeter.HelloReply, error) {
	return &greeter.HelloReply{Message: "hello " + in.GetName()}, nil
}

func (server) GetGreeting(ctx context.Context, in *greeter.GetGreetingRequest) (*greeter.HelloReply, error) {
	return &greeter.HelloReply{Message: "hi " + in.GetName()}, nil
}

// subjectFromMD builds the subject from plain metadata keys; tests use this
// in place of real token verification.
func subjectFromMD(ctx context.Context) (*authz.Subject, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if len(md.Get("x-broken")) > 0 {
		return nil, errors.New("token verification failed")
	}
	if len(md.Get("x-roles")) == 0 && len(md.Get("x-perms")) == 0 {
		return nil, nil // anonymous
	}
	return &authz.Subject{Roles: md.Get("x-roles"), Permissions: md.Get("x-perms")}, nil
}

func newClient(t *testing.T) greeter.GreeterClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(authz.UnaryServerInterceptor(subjectFromMD)),
		grpc.ChainStreamInterceptor(authz.StreamServerInterceptor(subjectFromMD)),
	)
	greeter.RegisterGreeterServer(s, server{})
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return greeter.NewGreeterClient(conn)
}

func TestAuthz(t *testing.T) {
	client := newClient(t)

	tests := []struct {
		name string
		md   metadata.MD
		call func(ctx context.Context) error
		want codes.Code
	}{
		{
			name: "anonymous denied on annotated method",
			call: func(ctx context.Context) error {
				_, err := client.SayHello(ctx, &greeter.HelloRequest{Name: "a", Times: 1})
				return err
			},
			want: codes.Unauthenticated,
		},
		{
			name: "subject error maps to Unauthenticated",
			md:   metadata.Pairs("x-broken", "1"),
			call: func(ctx context.Context) error {
				_, err := client.SayHello(ctx, &greeter.HelloRequest{Name: "a", Times: 1})
				return err
			},
			want: codes.Unauthenticated,
		},
		{
			name: "wrong role denied",
			md:   metadata.Pairs("x-roles", "viewer", "x-perms", "greetings.write"),
			call: func(ctx context.Context) error {
				_, err := client.SayHello(ctx, &greeter.HelloRequest{Name: "a", Times: 1})
				return err
			},
			want: codes.PermissionDenied,
		},
		{
			name: "role ok but missing permission denied",
			md:   metadata.Pairs("x-roles", "admin"),
			call: func(ctx context.Context) error {
				_, err := client.SayHello(ctx, &greeter.HelloRequest{Name: "a", Times: 1})
				return err
			},
			want: codes.PermissionDenied,
		},
		{
			name: "any_of role + all_of permission allowed",
			md:   metadata.Pairs("x-roles", "greeter", "x-perms", "greetings.write"),
			call: func(ctx context.Context) error {
				_, err := client.SayHello(ctx, &greeter.HelloRequest{Name: "a", Times: 1})
				return err
			},
			want: codes.OK,
		},
		{
			name: "service default public admits anonymous",
			call: func(ctx context.Context) error {
				_, err := client.GetGreeting(ctx, &greeter.GetGreetingRequest{Name: "a"})
				return err
			},
			want: codes.OK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.md != nil {
				ctx = metadata.NewOutgoingContext(ctx, tt.md)
			}
			if got := status.Code(tt.call(ctx)); got != tt.want {
				t.Fatalf("got code %v, want %v", got, tt.want)
			}
		})
	}
}
