package stream

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chat "github.com/dvislobokov/protogen/genchat"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// server implements every RPC kind of the generated ChatServer.
type server struct {
	chat.UnimplementedChatServer
}

func (server) Send(ctx context.Context, in *chat.Message) (*chat.Ack, error) {
	return &chat.Ack{Id: "ack-" + in.GetText()}, nil
}

func (server) Subscribe(req *chat.SubscribeRequest, stream grpc.ServerStreamingServer[chat.Message]) error {
	for i := 0; i < 3; i++ {
		if err := stream.Send(&chat.Message{Room: req.GetRoom(), Author: "srv", Text: fmt.Sprintf("msg %d", i)}); err != nil {
			return err
		}
	}
	return nil
}

func (server) Upload(stream grpc.ClientStreamingServer[chat.Chunk, chat.UploadSummary]) error {
	var total int64
	for {
		c, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&chat.UploadSummary{TotalBytes: total})
		}
		if err != nil {
			return err
		}
		total += int64(len(c.GetData()))
	}
}

func (server) Converse(stream grpc.BidiStreamingServer[chat.Message, chat.Message]) error {
	for {
		m, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&chat.Message{Text: "echo: " + m.GetText()}); err != nil {
			return err
		}
	}
}

func dial(t *testing.T) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	chat.RegisterChatServer(gs, server{})
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
	return conn
}

func TestAllRPCKinds(t *testing.T) {
	client := chat.NewChatClient(dial(t))
	ctx := context.Background()

	// unary
	ack, err := client.Send(ctx, &chat.Message{Text: "hi"})
	if err != nil || ack.GetId() != "ack-hi" {
		t.Fatalf("Send = %v, %v", ack, err)
	}

	// server streaming
	sub, err := client.Subscribe(ctx, &chat.SubscribeRequest{Room: "general"})
	if err != nil {
		t.Fatal(err)
	}
	var got int
	for {
		m, err := sub.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if m.GetRoom() != "general" {
			t.Fatalf("room = %q", m.GetRoom())
		}
		got++
	}
	if got != 3 {
		t.Fatalf("server stream got %d messages, want 3", got)
	}

	// client streaming
	up, err := client.Upload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{10, 20, 30} {
		if err := up.Send(&chat.Chunk{Data: make([]byte, n)}); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := up.CloseAndRecv()
	if err != nil || sum.GetTotalBytes() != 60 {
		t.Fatalf("Upload = %v, %v; want 60 bytes", sum, err)
	}

	// bidi
	conv, err := client.Converse(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := conv.Send(&chat.Message{Text: "ping"}); err != nil {
		t.Fatal(err)
	}
	reply, err := conv.Recv()
	if err != nil || reply.GetText() != "echo: ping" {
		t.Fatalf("Converse reply = %v, %v", reply, err)
	}
	conv.CloseSend()
}

// TestGatewayServerStreaming drives the server-streaming method over HTTP: the
// gateway forwards each gRPC message as a chunked JSON object.
func TestGatewayServerStreaming(t *testing.T) {
	mux := runtime.NewServeMux()
	if err := chat.RegisterChatHandler(context.Background(), mux, dial(t)); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/rooms/general/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	for _, want := range []string{"msg 0", "msg 1", "msg 2"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("stream body missing %q; got:\n%s", want, body)
		}
	}
}
