// Command consume proves the generated code is real and usable: it implements
// the generated GreeterServer, registers it, and constructs a client — all
// against types produced by protogenall from greeter.proto.
package main

import (
	"context"
	"fmt"

	greeter "github.com/dvislobokov/protogen/gen"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// validationInterceptor rejects any request message that violates its
// protovalidate constraints before it reaches the handler.
func validationInterceptor(v protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if m, ok := req.(proto.Message); ok {
			if err := v.Validate(m); err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
		return handler(ctx, req)
	}
}

type server struct {
	greeter.UnimplementedGreeterServer
}

func (server) SayHello(ctx context.Context, in *greeter.HelloRequest) (*greeter.HelloReply, error) {
	return &greeter.HelloReply{Message: fmt.Sprintf("hello, %s", in.GetName())}, nil
}

func main() {
	// Runtime validation via protovalidate — reads the buf.validate constraints
	// straight from the generated messages' descriptors. No generated validator.
	v, err := protovalidate.New()
	if err != nil {
		panic(err)
	}

	// Every RPC now validates its request automatically via the interceptor.
	s := grpc.NewServer(grpc.UnaryInterceptor(validationInterceptor(v)))
	greeter.RegisterGreeterServer(s, server{})

	// A real client would pass a live *grpc.ClientConn here.
	var cc grpc.ClientConnInterface
	_ = greeter.NewGreeterClient(cc)
	_ = context.Background()
	fmt.Println("generated gRPC server registered (with validation interceptor) OK")

	valid := &greeter.HelloRequest{Name: "Ada", Times: 3}
	fmt.Printf("valid   %-28v -> %v\n", valid, describe(v.Validate(valid)))

	invalid := &greeter.HelloRequest{Name: "", Times: 99} // breaks min_len and lte
	fmt.Printf("invalid %-28v -> %v\n", invalid, describe(v.Validate(invalid)))
}

func describe(err error) string {
	if err == nil {
		return "OK"
	}
	return "rejected: " + err.Error()
}
