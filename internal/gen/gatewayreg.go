package gen

import (
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

func genRegisterHandlerServer(g *protogen.GeneratedFile, svc *protogen.Service, bs []binding) {
	ctx := g.QualifiedGoIdent(contextPkg.Ident("Context"))
	mux := "*" + g.QualifiedGoIdent(runtimePkg.Ident("ServeMux"))

	g.P("// Register", svc.GoName, "HandlerServer registers the http handlers for service ", svc.GoName, " to \"mux\",")
	g.P("// calling ", svc.GoName, "Server directly (in-process, no network round trip).")
	g.P("func Register", svc.GoName, "HandlerServer(ctx ", ctx, ", mux ", mux, ", server ", svc.GoName, "Server) error {")
	for i := range bs {
		genHandleBlock(g, svc, &bs[i], true)
	}
	g.P("return nil")
	g.P("}")
	g.P()
}

func genRegisterHandlerClient(g *protogen.GeneratedFile, svc *protogen.Service, bs []binding) {
	ctx := g.QualifiedGoIdent(contextPkg.Ident("Context"))
	mux := "*" + g.QualifiedGoIdent(runtimePkg.Ident("ServeMux"))

	g.P("// Register", svc.GoName, "HandlerClient registers the http handlers for service ", svc.GoName, ",")
	g.P("// forwarding requests to the gRPC endpoint over the given ", svc.GoName, "Client.")
	g.P("func Register", svc.GoName, "HandlerClient(ctx ", ctx, ", mux ", mux, ", client ", svc.GoName, "Client) error {")
	for i := range bs {
		genHandleBlock(g, svc, &bs[i], false)
	}
	g.P("return nil")
	g.P("}")
	g.P()
}

// genHandleBlock emits one mux.Handle(...) closure for a binding.
func genHandleBlock(g *protogen.GeneratedFile, svc *protogen.Service, b *binding, local bool) {
	m := b.method
	base := handlerBase(svc, m)
	httpMethod := g.QualifiedGoIdent(httpPkg.Ident(b.httpMethod))
	respWriter := g.QualifiedGoIdent(httpPkg.Ident("ResponseWriter"))
	httpReq := "*" + g.QualifiedGoIdent(httpPkg.Ident("Request"))

	// Server-streaming has no in-process transport: emit an Unimplemented stub.
	if b.serverStream && local {
		g.P("mux.Handle(", httpMethod, ", pattern_", base, ", func(w ", respWriter, ", req ", httpReq, ", pathParams map[string]string) {")
		g.P("err := ", g.QualifiedGoIdent(statusPkg.Ident("Error")), "(", g.QualifiedGoIdent(codesPkg.Ident("Unimplemented")), `, "streaming calls are not yet supported in the in-process transport")`)
		g.P("_, outboundMarshaler := ", g.QualifiedGoIdent(runtimePkg.Ident("MarshalerForRequest")), "(mux, req)")
		g.P(g.QualifiedGoIdent(runtimePkg.Ident("HTTPError")), "(ctx, mux, outboundMarshaler, w, req, err)")
		g.P("})")
		return
	}

	g.P("mux.Handle(", httpMethod, ", pattern_", base, ", func(w ", respWriter, ", req ", httpReq, ", pathParams map[string]string) {")
	g.P("ctx, cancel := ", g.QualifiedGoIdent(contextPkg.Ident("WithCancel")), "(req.Context())")
	g.P("defer cancel()")

	if local {
		g.P("var stream ", g.QualifiedGoIdent(runtimePkg.Ident("ServerTransportStream")))
		g.P("ctx = ", g.QualifiedGoIdent(grpcPkg.Ident("NewContextWithServerTransportStream")), "(ctx, &stream)")
	}
	g.P("inboundMarshaler, outboundMarshaler := ", g.QualifiedGoIdent(runtimePkg.Ident("MarshalerForRequest")), "(mux, req)")

	annotate := "AnnotateContext"
	if local {
		annotate = "AnnotateIncomingContext"
	}
	g.P("annotatedContext, err := ", g.QualifiedGoIdent(runtimePkg.Ident(annotate)), "(ctx, mux, req, ", strconv.Quote(fullMethod(svc, m)), ", ", g.QualifiedGoIdent(runtimePkg.Ident("WithHTTPPathPattern")), "(", strconv.Quote(b.path), "))")
	g.P("if err != nil {")
	g.P(g.QualifiedGoIdent(runtimePkg.Ident("HTTPError")), "(ctx, mux, outboundMarshaler, w, req, err)")
	g.P("return")
	g.P("}")

	decoder := "request_" + base
	callee := "client"
	if local {
		decoder = "local_request_" + base
		callee = "server"
	}
	g.P("resp, md, err := ", decoder, "(annotatedContext, inboundMarshaler, ", callee, ", req, pathParams)")
	if local {
		join := g.QualifiedGoIdent(metadataPkg.Ident("Join"))
		g.P("md.HeaderMD, md.TrailerMD = ", join, "(md.HeaderMD, stream.Header()), ", join, "(md.TrailerMD, stream.Trailer())")
	}
	g.P("annotatedContext = ", g.QualifiedGoIdent(runtimePkg.Ident("NewServerMetadataContext")), "(annotatedContext, md)")
	g.P("if err != nil {")
	g.P(g.QualifiedGoIdent(runtimePkg.Ident("HTTPError")), "(annotatedContext, mux, outboundMarshaler, w, req, err)")
	g.P("return")
	g.P("}")
	if b.serverStream {
		// Forward each streamed message; ForwardResponseStream pumps resp.Recv().
		protoMsg := g.QualifiedGoIdent(protoRefPkg.Ident("Message"))
		g.P("forward_", base, "(annotatedContext, mux, outboundMarshaler, w, req, func() (", protoMsg, ", error) { return resp.Recv() }, mux.GetForwardResponseOptions()...)")
	} else {
		g.P("forward_", base, "(annotatedContext, mux, outboundMarshaler, w, req, resp, mux.GetForwardResponseOptions()...)")
	}
	g.P("})")
}

func genRegisterFromEndpoint(g *protogen.GeneratedFile, svc *protogen.Service) {
	ctx := g.QualifiedGoIdent(contextPkg.Ident("Context"))
	mux := "*" + g.QualifiedGoIdent(runtimePkg.Ident("ServeMux"))
	dialOpt := g.QualifiedGoIdent(grpcPkg.Ident("DialOption"))
	newClient := g.QualifiedGoIdent(grpcPkg.Ident("NewClient"))
	errorf := g.QualifiedGoIdent(grpclogPkg.Ident("Errorf"))

	g.P("// Register", svc.GoName, "HandlerFromEndpoint dials \"endpoint\" and registers the handlers.")
	g.P("func Register", svc.GoName, "HandlerFromEndpoint(ctx ", ctx, ", mux ", mux, ", endpoint string, opts []", dialOpt, ") (err error) {")
	g.P("conn, err := ", newClient, "(endpoint, opts...)")
	g.P("if err != nil { return err }")
	g.P("defer func() {")
	g.P("if err != nil {")
	g.P("if cerr := conn.Close(); cerr != nil { ", errorf, `("Failed to close conn to %s: %v", endpoint, cerr) }`)
	g.P("return")
	g.P("}")
	g.P("go func() {")
	g.P("<-ctx.Done()")
	g.P("if cerr := conn.Close(); cerr != nil { ", errorf, `("Failed to close conn to %s: %v", endpoint, cerr) }`)
	g.P("}()")
	g.P("}()")
	g.P("return Register", svc.GoName, "Handler(ctx, mux, conn)")
	g.P("}")
	g.P()
}

func genRegisterHandler(g *protogen.GeneratedFile, svc *protogen.Service) {
	ctx := g.QualifiedGoIdent(contextPkg.Ident("Context"))
	mux := "*" + g.QualifiedGoIdent(runtimePkg.Ident("ServeMux"))
	clientConn := "*" + g.QualifiedGoIdent(grpcPkg.Ident("ClientConn"))

	g.P("// Register", svc.GoName, "Handler registers the http handlers, forwarding over \"conn\".")
	g.P("func Register", svc.GoName, "Handler(ctx ", ctx, ", mux ", mux, ", conn ", clientConn, ") error {")
	g.P("return Register", svc.GoName, "HandlerClient(ctx, mux, New", svc.GoName, "Client(conn))")
	g.P("}")
	g.P()
}

func genPatternsAndForwards(g *protogen.GeneratedFile, svc *protogen.Service, bs []binding) {
	mustPattern := g.QualifiedGoIdent(runtimePkg.Ident("MustPattern"))
	newPattern := g.QualifiedGoIdent(runtimePkg.Ident("NewPattern"))
	forwardMsg := g.QualifiedGoIdent(runtimePkg.Ident("ForwardResponseMessage"))
	forwardStream := g.QualifiedGoIdent(runtimePkg.Ident("ForwardResponseStream"))

	g.P("var (")
	for i := range bs {
		base := handlerBase(svc, bs[i].method)
		t := bs[i].tmpl
		g.P("pattern_", base, " = ", mustPattern, "(", newPattern, "(1, []int{", intList(t.OpCodes), "}, []string{", strList(t.Pool), "}, ", strconv.Quote(t.Verb), "))")
	}
	g.P(")")
	g.P()
	g.P("var (")
	for i := range bs {
		base := handlerBase(svc, bs[i].method)
		forward := forwardMsg
		if bs[i].serverStream {
			forward = forwardStream
		}
		g.P("forward_", base, " = ", forward)
	}
	g.P(")")
	g.P()
}

func intList(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ", ")
}

func strList(xs []string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Quote(x)
	}
	return strings.Join(parts, ", ")
}
