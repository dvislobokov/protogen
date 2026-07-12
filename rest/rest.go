// Package rest turns protovalidate failures into an ASP.NET Core-style HTTP 400
// response for grpc-gateway: RFC 9457 problem+json with a field->messages
// "errors" map, exactly like ASP.NET Core's ValidationProblemDetails.
//
// Two pieces work together:
//   - ValidationInterceptor validates each unary request and, on failure,
//     returns a gRPC InvalidArgument status carrying google.rpc.BadRequest
//     field violations.
//   - ProblemErrorHandler is a grpc-gateway error handler that renders those
//     violations as problem+json.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"buf.build/go/protovalidate"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
)

// ValidationProblemDetails mirrors ASP.NET Core's response for a failed model
// validation (RFC 9457 / RFC 7807 problem+json).
type ValidationProblemDetails struct {
	Type   string              `json:"type"`
	Title  string              `json:"title"`
	Status int                 `json:"status"`
	Errors map[string][]string `json:"errors"`
}

// ValidationInterceptor validates unary requests with v before the handler runs.
func ValidationInterceptor(v protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if m, ok := req.(proto.Message); ok {
			if err := v.Validate(m); err != nil {
				return nil, statusFromValidation(err).Err()
			}
		}
		return handler(ctx, req)
	}
}

// statusFromValidation converts a protovalidate error into an InvalidArgument
// status with google.rpc.BadRequest details (one per field violation).
func statusFromValidation(err error) *status.Status {
	var verr *protovalidate.ValidationError
	if !errors.As(err, &verr) {
		return status.New(codes.InvalidArgument, err.Error())
	}
	br := &errdetails.BadRequest{}
	for _, v := range verr.Violations {
		br.FieldViolations = append(br.FieldViolations, &errdetails.BadRequest_FieldViolation{
			Field:       fieldPath(v.Proto.GetField()),
			Description: v.Proto.GetMessage(),
		})
	}
	st := status.New(codes.InvalidArgument, "One or more validation errors occurred.")
	if withDetails, derr := st.WithDetails(br); derr == nil {
		return withDetails
	}
	return st
}

// fieldPath renders a violation's field path using JSON (camelCase) names, so
// the reported keys match the request body and the OpenAPI schema.
func fieldPath(fp *validate.FieldPath) string {
	if fp == nil {
		return ""
	}
	var parts []string
	for _, e := range fp.GetElements() {
		parts = append(parts, jsonName(e.GetFieldName()))
	}
	return strings.Join(parts, ".")
}

// jsonName converts a proto3 field name (snake_case) to its default JSON name
// (lowerCamelCase).
func jsonName(s string) string {
	var b strings.Builder
	upNext := false
	for i, r := range s {
		if r == '_' {
			upNext = true
			continue
		}
		if upNext && i != 0 {
			b.WriteRune(upper(r))
			upNext = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func upper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// ProblemErrorHandler renders InvalidArgument statuses that carry BadRequest
// details as problem+json; everything else falls back to the gateway default.
func ProblemErrorHandler(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	st := status.Convert(err)
	if st.Code() == codes.InvalidArgument {
		errs := map[string][]string{}
		for _, d := range st.Details() {
			br, ok := d.(*errdetails.BadRequest)
			if !ok {
				continue
			}
			for _, fv := range br.GetFieldViolations() {
				field := fv.GetField()
				if field == "" {
					field = "$"
				}
				errs[field] = append(errs[field], fv.GetDescription())
			}
		}
		if len(errs) > 0 {
			writeProblem(w, ValidationProblemDetails{
				Type:   "https://datatracker.ietf.org/doc/html/rfc9110#section-15.5.1",
				Title:  "One or more validation errors occurred.",
				Status: http.StatusBadRequest,
				Errors: errs,
			})
			return
		}
	}
	runtime.DefaultHTTPErrorHandler(ctx, mux, m, w, r, err)
}

func writeProblem(w http.ResponseWriter, pd ValidationProblemDetails) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(pd.Status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(pd)
}
