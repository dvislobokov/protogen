package gen

import (
	"strconv"

	"google.golang.org/protobuf/compiler/protogen"
)

// ---- client ----

func genClient(g *protogen.GeneratedFile, svc *protogen.Service) {
	h := newIdents(g)
	unexported := unexport(svc.GoName) + "Client"
	streams := streamIndex(svc)

	g.P("// ", svc.GoName, "Client is the client API for the ", svc.GoName, " service.")
	g.P("type ", svc.GoName, "Client interface {")
	for _, m := range svc.Methods {
		g.P(m.GoName, "(", clientParams(h, m), ") ", clientRet(h, m))
	}
	g.P("}")
	g.P()

	g.P("type ", unexported, " struct {")
	g.P("cc ", h.clientConn)
	g.P("}")
	g.P()
	g.P("func New", svc.GoName, "Client(cc ", h.clientConn, ") ", svc.GoName, "Client {")
	g.P("return &", unexported, "{cc}")
	g.P("}")
	g.P()

	for _, m := range svc.Methods {
		full := strconv.Quote(fullMethodName(svc, m))
		g.P("func (c *", unexported, ") ", m.GoName, "(", clientParams(h, m), ") ", clientRet(h, m), " {")
		if !isStreaming(m) {
			g.P("out := new(", h.out(m), ")")
			g.P("err := c.cc.Invoke(ctx, ", full, ", in, out, opts...)")
			g.P("if err != nil { return nil, err }")
			g.P("return out, nil")
			g.P("}")
			g.P()
			continue
		}
		g.P("stream, err := c.cc.NewStream(ctx, &", svc.GoName, "_ServiceDesc.Streams[", streams[m], "], ", full, ", opts...)")
		g.P("if err != nil { return nil, err }")
		g.P("x := &", h.generic("GenericClientStream", h.in(m), h.out(m)), "{ClientStream: stream}")
		if !m.Desc.IsStreamingClient() { // server-streaming: send the single request now
			g.P("if err := x.ClientStream.SendMsg(in); err != nil { return nil, err }")
			g.P("if err := x.ClientStream.CloseSend(); err != nil { return nil, err }")
		}
		g.P("return x, nil")
		g.P("}")
		g.P()
	}
}

// clientParams: server-streaming and unary take a request `in`; client-streaming
// and bidi do not (the client sends on the stream instead).
func clientParams(h *idents, m *protogen.Method) string {
	if m.Desc.IsStreamingClient() {
		return "ctx " + h.ctx + ", opts ..." + h.callOpt
	}
	return "ctx " + h.ctx + ", in *" + h.in(m) + ", opts ..." + h.callOpt
}

func clientRet(h *idents, m *protogen.Method) string {
	switch {
	case !isStreaming(m):
		return "(*" + h.out(m) + ", error)"
	case m.Desc.IsStreamingClient() && m.Desc.IsStreamingServer():
		return "(" + h.generic("BidiStreamingClient", h.in(m), h.out(m)) + ", error)"
	case m.Desc.IsStreamingClient():
		return "(" + h.generic("ClientStreamingClient", h.in(m), h.out(m)) + ", error)"
	default: // server-streaming
		return "(" + h.generic("ServerStreamingClient", h.out(m)) + ", error)"
	}
}

// ---- server ----

func genServer(g *protogen.GeneratedFile, svc *protogen.Service) {
	h := newIdents(g)
	unimpl := "Unimplemented" + svc.GoName + "Server"
	statusErr := g.QualifiedGoIdent(statusPkg.Ident("Error"))
	unimplemented := g.QualifiedGoIdent(codesPkg.Ident("Unimplemented"))

	g.P("// ", svc.GoName, "Server is the server API for the ", svc.GoName, " service.")
	g.P("type ", svc.GoName, "Server interface {")
	for _, m := range svc.Methods {
		g.P(serverMethodSig(h, m))
	}
	g.P("mustEmbedUnimplemented", svc.GoName, "Server()")
	g.P("}")
	g.P()

	g.P("// ", unimpl, " must be embedded to have forward compatible implementations.")
	g.P("type ", unimpl, " struct{}")
	g.P()
	for _, m := range svc.Methods {
		g.P("func (", unimpl, ") ", serverMethodSig(h, m), " {")
		msg := strconv.Quote("method " + m.GoName + " not implemented")
		if isStreaming(m) {
			g.P("return ", statusErr, "(", unimplemented, ", ", msg, ")")
		} else {
			g.P("return nil, ", statusErr, "(", unimplemented, ", ", msg, ")")
		}
		g.P("}")
	}
	g.P("func (", unimpl, ") mustEmbedUnimplemented", svc.GoName, "Server() {}")
	g.P()

	registrar := g.QualifiedGoIdent(grpcPkg.Ident("ServiceRegistrar"))
	g.P("func Register", svc.GoName, "Server(s ", registrar, ", srv ", svc.GoName, "Server) {")
	g.P("s.RegisterService(&", svc.GoName, "_ServiceDesc, srv)")
	g.P("}")
	g.P()

	for _, m := range svc.Methods {
		genHandler(g, h, svc, m)
	}
}

func serverMethodSig(h *idents, m *protogen.Method) string {
	switch {
	case !isStreaming(m):
		return m.GoName + "(" + h.ctx + ", *" + h.in(m) + ") (*" + h.out(m) + ", error)"
	case m.Desc.IsStreamingClient() && m.Desc.IsStreamingServer():
		return m.GoName + "(" + h.generic("BidiStreamingServer", h.in(m), h.out(m)) + ") error"
	case m.Desc.IsStreamingClient():
		return m.GoName + "(" + h.generic("ClientStreamingServer", h.in(m), h.out(m)) + ") error"
	default: // server-streaming
		return m.GoName + "(*" + h.in(m) + ", " + h.generic("ServerStreamingServer", h.out(m)) + ") error"
	}
}

func genHandler(g *protogen.GeneratedFile, h *idents, svc *protogen.Service, m *protogen.Method) {
	name := handlerName(svc, m)
	full := strconv.Quote(fullMethodName(svc, m))

	if !isStreaming(m) {
		g.P("func ", name, "(srv interface{}, ctx ", h.ctx, ", dec func(interface{}) error, interceptor ", g.QualifiedGoIdent(grpcPkg.Ident("UnaryServerInterceptor")), ") (interface{}, error) {")
		g.P("in := new(", h.in(m), ")")
		g.P("if err := dec(in); err != nil { return nil, err }")
		g.P("if interceptor == nil { return srv.(", svc.GoName, "Server).", m.GoName, "(ctx, in) }")
		g.P("info := &", g.QualifiedGoIdent(grpcPkg.Ident("UnaryServerInfo")), "{Server: srv, FullMethod: ", full, "}")
		g.P("handler := func(ctx ", h.ctx, ", req interface{}) (interface{}, error) {")
		g.P("return srv.(", svc.GoName, "Server).", m.GoName, "(ctx, req.(*", h.in(m), "))")
		g.P("}")
		g.P("return interceptor(ctx, in, info, handler)")
		g.P("}")
		g.P()
		return
	}

	serverStream := g.QualifiedGoIdent(grpcPkg.Ident("ServerStream"))
	genericStream := h.generic("GenericServerStream", h.in(m), h.out(m))
	g.P("func ", name, "(srv interface{}, stream ", serverStream, ") error {")
	if !m.Desc.IsStreamingClient() { // server-streaming: receive the single request
		g.P("m := new(", h.in(m), ")")
		g.P("if err := stream.RecvMsg(m); err != nil { return err }")
		g.P("return srv.(", svc.GoName, "Server).", m.GoName, "(m, &", genericStream, "{ServerStream: stream})")
	} else {
		g.P("return srv.(", svc.GoName, "Server).", m.GoName, "(&", genericStream, "{ServerStream: stream})")
	}
	g.P("}")
	g.P()
}

// ---- service descriptor ----

func genServiceDesc(g *protogen.GeneratedFile, svc *protogen.Service) {
	g.P("// ", svc.GoName, "_ServiceDesc is the grpc.ServiceDesc for the ", svc.GoName, " service.")
	g.P("var ", svc.GoName, "_ServiceDesc = ", g.QualifiedGoIdent(grpcPkg.Ident("ServiceDesc")), "{")
	g.P("ServiceName: ", strconv.Quote(string(svc.Desc.FullName())), ",")
	g.P("HandlerType: (*", svc.GoName, "Server)(nil),")

	g.P("Methods: []", g.QualifiedGoIdent(grpcPkg.Ident("MethodDesc")), "{")
	for _, m := range svc.Methods {
		if isStreaming(m) {
			continue
		}
		g.P("{")
		g.P("MethodName: ", strconv.Quote(string(m.Desc.Name())), ",")
		g.P("Handler: ", handlerName(svc, m), ",")
		g.P("},")
	}
	g.P("},")

	g.P("Streams: []", g.QualifiedGoIdent(grpcPkg.Ident("StreamDesc")), "{")
	for _, m := range svc.Methods {
		if !isStreaming(m) {
			continue
		}
		g.P("{")
		g.P("StreamName: ", strconv.Quote(string(m.Desc.Name())), ",")
		g.P("Handler: ", handlerName(svc, m), ",")
		if m.Desc.IsStreamingServer() {
			g.P("ServerStreams: true,")
		}
		if m.Desc.IsStreamingClient() {
			g.P("ClientStreams: true,")
		}
		g.P("},")
	}
	g.P("},")
	g.P("Metadata: ", strconv.Quote(svc.Desc.ParentFile().Path()), ",")
	g.P("}")
	g.P()
}

// ---- misc ----

func unexport(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}
