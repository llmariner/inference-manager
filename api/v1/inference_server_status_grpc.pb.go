// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.5.1
// - protoc             (unknown)
// source: api/v1/inference_server_status.proto

package v1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.64.0 or later.
const _ = grpc.SupportPackageIsVersion9

const (
	InferenceService_ListInferenceStatus_FullMethodName = "/llmariner.inference.server.v1.InferenceService/ListInferenceStatus"
)

// InferenceServiceClient is the client API for InferenceService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type InferenceServiceClient interface {
	ListInferenceStatus(ctx context.Context, in *ListInferenceStatusRequest, opts ...grpc.CallOption) (*InferenceStatus, error)
}

type inferenceServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewInferenceServiceClient(cc grpc.ClientConnInterface) InferenceServiceClient {
	return &inferenceServiceClient{cc}
}

func (c *inferenceServiceClient) ListInferenceStatus(ctx context.Context, in *ListInferenceStatusRequest, opts ...grpc.CallOption) (*InferenceStatus, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(InferenceStatus)
	err := c.cc.Invoke(ctx, InferenceService_ListInferenceStatus_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// InferenceServiceServer is the server API for InferenceService service.
// All implementations must embed UnimplementedInferenceServiceServer
// for forward compatibility.
type InferenceServiceServer interface {
	ListInferenceStatus(context.Context, *ListInferenceStatusRequest) (*InferenceStatus, error)
	mustEmbedUnimplementedInferenceServiceServer()
}

// UnimplementedInferenceServiceServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedInferenceServiceServer struct{}

func (UnimplementedInferenceServiceServer) ListInferenceStatus(context.Context, *ListInferenceStatusRequest) (*InferenceStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListInferenceStatus not implemented")
}
func (UnimplementedInferenceServiceServer) mustEmbedUnimplementedInferenceServiceServer() {}
func (UnimplementedInferenceServiceServer) testEmbeddedByValue()                          {}

// UnsafeInferenceServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to InferenceServiceServer will
// result in compilation errors.
type UnsafeInferenceServiceServer interface {
	mustEmbedUnimplementedInferenceServiceServer()
}

func RegisterInferenceServiceServer(s grpc.ServiceRegistrar, srv InferenceServiceServer) {
	// If the following call pancis, it indicates UnimplementedInferenceServiceServer was
	// embedded by pointer and is nil.  This will cause panics if an
	// unimplemented method is ever invoked, so we test this at initialization
	// time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&InferenceService_ServiceDesc, srv)
}

func _InferenceService_ListInferenceStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListInferenceStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(InferenceServiceServer).ListInferenceStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: InferenceService_ListInferenceStatus_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(InferenceServiceServer).ListInferenceStatus(ctx, req.(*ListInferenceStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// InferenceService_ServiceDesc is the grpc.ServiceDesc for InferenceService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var InferenceService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "llmariner.inference.server.v1.InferenceService",
	HandlerType: (*InferenceServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ListInferenceStatus",
			Handler:    _InferenceService_ListInferenceStatus_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "api/v1/inference_server_status.proto",
}
