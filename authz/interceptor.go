// Package authz enforces the (protogen.authz.requires) proto annotations at
// runtime via gRPC server interceptors.
//
// Wire it up with a SubjectFunc that extracts the caller from the context
// (e.g. from a JWT in metadata):
//
//	s := grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(authz.UnaryServerInterceptor(subjectFromJWT)),
//	    grpc.ChainStreamInterceptor(authz.StreamServerInterceptor(subjectFromJWT)),
//	)
//
// Semantics per method:
//   - no (protogen.authz.requires) on the method and no
//     (protogen.authz.default_requires) on the service → not checked;
//   - public: true → allowed, SubjectFunc is not called;
//   - otherwise the subject must be non-nil (else Unauthenticated) and must
//     satisfy the roles and permissions rules (else PermissionDenied).
package authz

import (
	"context"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Subject is the authenticated caller as seen by the interceptors.
type Subject struct {
	Roles       []string
	Permissions []string
}

// SubjectFunc extracts the calling subject from the request context.
// Returning (nil, nil) means the request is anonymous; a non-nil error is
// returned to the client as codes.Unauthenticated unless it already carries a
// gRPC status.
type SubjectFunc func(ctx context.Context) (*Subject, error)

// UnaryServerInterceptor enforces the method's authz annotation before
// invoking the handler.
func UnaryServerInterceptor(subject SubjectFunc) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := Authorize(ctx, info.FullMethod, subject); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor is the streaming counterpart of
// UnaryServerInterceptor.
func StreamServerInterceptor(subject SubjectFunc) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := Authorize(ss.Context(), info.FullMethod, subject); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

// Authorize checks the annotation of fullMethod ("/pkg.Service/Method")
// against the subject extracted from ctx. Exported so that non-interceptor
// transports (e.g. a custom gateway) can reuse the exact same policy.
func Authorize(ctx context.Context, fullMethod string, subject SubjectFunc) error {
	req := requirementsFor(fullMethod)
	if req == nil || req.GetPublic() {
		return nil
	}
	sub, err := subject(ctx)
	if err != nil {
		if _, ok := status.FromError(err); ok {
			return err
		}
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if sub == nil {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	if !match(req.GetRoles(), sub.Roles) {
		return status.Errorf(codes.PermissionDenied, "%s: caller roles do not satisfy the method's requirements", fullMethod)
	}
	if !match(req.GetPermissions(), sub.Permissions) {
		return status.Errorf(codes.PermissionDenied, "%s: caller permissions do not satisfy the method's requirements", fullMethod)
	}
	return nil
}

// reqCache memoizes descriptor lookups per full method name; the descriptor
// registry never changes after init, so entries stay valid forever.
var reqCache sync.Map // string → *Requirements (possibly the nil sentinel)

func requirementsFor(fullMethod string) *Requirements {
	if v, ok := reqCache.Load(fullMethod); ok {
		return v.(*Requirements)
	}
	req := lookupRequirements(fullMethod)
	reqCache.Store(fullMethod, req)
	return req
}

// lookupRequirements resolves "/pkg.Service/Method" through the global
// protobuf registry. Methods without a resolvable descriptor or annotation
// yield nil (not checked) — policies apply only to what is declared.
func lookupRequirements(fullMethod string) *Requirements {
	svcName, methodName, ok := strings.Cut(strings.TrimPrefix(fullMethod, "/"), "/")
	if !ok {
		return nil
	}
	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(svcName))
	if err != nil {
		return nil
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil
	}
	method := svc.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		return nil
	}
	if opts, ok := method.Options().(*descriptorpb.MethodOptions); ok && proto.HasExtension(opts, E_Requires) {
		return proto.GetExtension(opts, E_Requires).(*Requirements)
	}
	if opts, ok := svc.Options().(*descriptorpb.ServiceOptions); ok && proto.HasExtension(opts, E_DefaultRequires) {
		return proto.GetExtension(opts, E_DefaultRequires).(*Requirements)
	}
	return nil
}

// match reports whether the subject's values satisfy the rule. Every set
// field must pass; a nil/empty rule always passes.
func match(r *Rule, have []string) bool {
	if r == nil {
		return true
	}
	set := make(map[string]bool, len(have))
	for _, v := range have {
		set[v] = true
	}
	if len(r.GetAnyOf()) > 0 {
		any := false
		for _, v := range r.GetAnyOf() {
			if set[v] {
				any = true
				break
			}
		}
		if !any {
			return false
		}
	}
	for _, v := range r.GetAllOf() {
		if !set[v] {
			return false
		}
	}
	for _, v := range r.GetNoneOf() {
		if set[v] {
			return false
		}
	}
	return true
}
