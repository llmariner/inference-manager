// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package v1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// InferenceInternalServiceClient is the client API for InferenceInternalService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type InferenceInternalServiceClient interface {
	ProcessTasksInternal(ctx context.Context, opts ...grpc.CallOption) (InferenceInternalService_ProcessTasksInternalClient, error)
}

type inferenceInternalServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewInferenceInternalServiceClient(cc grpc.ClientConnInterface) InferenceInternalServiceClient {
	return &inferenceInternalServiceClient{cc}
}

func (c *inferenceInternalServiceClient) ProcessTasksInternal(ctx context.Context, opts ...grpc.CallOption) (InferenceInternalService_ProcessTasksInternalClient, error) {
	stream, err := c.cc.NewStream(ctx, &InferenceInternalService_ServiceDesc.Streams[0], "/llmariner.inference.server.v1.InferenceInternalService/ProcessTasksInternal", opts...)
	if err != nil {
		return nil, err
	}
	x := &inferenceInternalServiceProcessTasksInternalClient{stream}
	return x, nil
}

type InferenceInternalService_ProcessTasksInternalClient interface {
	Send(*ProcessTasksInternalRequest) error
	Recv() (*ProcessTasksInternalResponse, error)
	grpc.ClientStream
}

type inferenceInternalServiceProcessTasksInternalClient struct {
	grpc.ClientStream
}

func (x *inferenceInternalServiceProcessTasksInternalClient) Send(m *ProcessTasksInternalRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *inferenceInternalServiceProcessTasksInternalClient) Recv() (*ProcessTasksInternalResponse, error) {
	m := new(ProcessTasksInternalResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// InferenceInternalServiceServer is the server API for InferenceInternalService service.
// All implementations must embed UnimplementedInferenceInternalServiceServer
// for forward compatibility
type InferenceInternalServiceServer interface {
	ProcessTasksInternal(InferenceInternalService_ProcessTasksInternalServer) error
	mustEmbedUnimplementedInferenceInternalServiceServer()
}

// UnimplementedInferenceInternalServiceServer must be embedded to have forward compatible implementations.
type UnimplementedInferenceInternalServiceServer struct {
}

func (UnimplementedInferenceInternalServiceServer) ProcessTasksInternal(InferenceInternalService_ProcessTasksInternalServer) error {
	return status.Errorf(codes.Unimplemented, "method ProcessTasksInternal not implemented")
}
func (UnimplementedInferenceInternalServiceServer) mustEmbedUnimplementedInferenceInternalServiceServer() {
}

// UnsafeInferenceInternalServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to InferenceInternalServiceServer will
// result in compilation errors.
type UnsafeInferenceInternalServiceServer interface {
	mustEmbedUnimplementedInferenceInternalServiceServer()
}

func RegisterInferenceInternalServiceServer(s grpc.ServiceRegistrar, srv InferenceInternalServiceServer) {
	s.RegisterService(&InferenceInternalService_ServiceDesc, srv)
}

func _InferenceInternalService_ProcessTasksInternal_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(InferenceInternalServiceServer).ProcessTasksInternal(&inferenceInternalServiceProcessTasksInternalServer{stream})
}

type InferenceInternalService_ProcessTasksInternalServer interface {
	Send(*ProcessTasksInternalResponse) error
	Recv() (*ProcessTasksInternalRequest, error)
	grpc.ServerStream
}

type inferenceInternalServiceProcessTasksInternalServer struct {
	grpc.ServerStream
}

func (x *inferenceInternalServiceProcessTasksInternalServer) Send(m *ProcessTasksInternalResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *inferenceInternalServiceProcessTasksInternalServer) Recv() (*ProcessTasksInternalRequest, error) {
	m := new(ProcessTasksInternalRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// InferenceInternalService_ServiceDesc is the grpc.ServiceDesc for InferenceInternalService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var InferenceInternalService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "llmariner.inference.server.v1.InferenceInternalService",
	HandlerType: (*InferenceInternalServiceServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "ProcessTasksInternal",
			Handler:       _InferenceInternalService_ProcessTasksInternal_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "api/v1/inference_server_internal.proto",
}