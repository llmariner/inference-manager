// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package legacy

import (
	context "context"
	v1 "github.com/llmariner/inference-manager/api/v1"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// InferenceWorkerServiceClient is the client API for InferenceWorkerService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type InferenceWorkerServiceClient interface {
	ProcessTasks(ctx context.Context, opts ...grpc.CallOption) (InferenceWorkerService_ProcessTasksClient, error)
}

type inferenceWorkerServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewInferenceWorkerServiceClient(cc grpc.ClientConnInterface) InferenceWorkerServiceClient {
	return &inferenceWorkerServiceClient{cc}
}

func (c *inferenceWorkerServiceClient) ProcessTasks(ctx context.Context, opts ...grpc.CallOption) (InferenceWorkerService_ProcessTasksClient, error) {
	stream, err := c.cc.NewStream(ctx, &InferenceWorkerService_ServiceDesc.Streams[0], "/llmoperator.inference.server.v1.InferenceWorkerService/ProcessTasks", opts...)
	if err != nil {
		return nil, err
	}
	x := &inferenceWorkerServiceProcessTasksClient{stream}
	return x, nil
}

type InferenceWorkerService_ProcessTasksClient interface {
	Send(*v1.ProcessTasksRequest) error
	Recv() (*v1.ProcessTasksResponse, error)
	grpc.ClientStream
}

type inferenceWorkerServiceProcessTasksClient struct {
	grpc.ClientStream
}

func (x *inferenceWorkerServiceProcessTasksClient) Send(m *v1.ProcessTasksRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *inferenceWorkerServiceProcessTasksClient) Recv() (*v1.ProcessTasksResponse, error) {
	m := new(v1.ProcessTasksResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// InferenceWorkerServiceServer is the server API for InferenceWorkerService service.
// All implementations must embed UnimplementedInferenceWorkerServiceServer
// for forward compatibility
type InferenceWorkerServiceServer interface {
	ProcessTasks(InferenceWorkerService_ProcessTasksServer) error
	mustEmbedUnimplementedInferenceWorkerServiceServer()
}

// UnimplementedInferenceWorkerServiceServer must be embedded to have forward compatible implementations.
type UnimplementedInferenceWorkerServiceServer struct {
}

func (UnimplementedInferenceWorkerServiceServer) ProcessTasks(InferenceWorkerService_ProcessTasksServer) error {
	return status.Errorf(codes.Unimplemented, "method ProcessTasks not implemented")
}
func (UnimplementedInferenceWorkerServiceServer) mustEmbedUnimplementedInferenceWorkerServiceServer() {
}

// UnsafeInferenceWorkerServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to InferenceWorkerServiceServer will
// result in compilation errors.
type UnsafeInferenceWorkerServiceServer interface {
	mustEmbedUnimplementedInferenceWorkerServiceServer()
}

func RegisterInferenceWorkerServiceServer(s grpc.ServiceRegistrar, srv InferenceWorkerServiceServer) {
	s.RegisterService(&InferenceWorkerService_ServiceDesc, srv)
}

func _InferenceWorkerService_ProcessTasks_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(InferenceWorkerServiceServer).ProcessTasks(&inferenceWorkerServiceProcessTasksServer{stream})
}

type InferenceWorkerService_ProcessTasksServer interface {
	Send(*v1.ProcessTasksResponse) error
	Recv() (*v1.ProcessTasksRequest, error)
	grpc.ServerStream
}

type inferenceWorkerServiceProcessTasksServer struct {
	grpc.ServerStream
}

func (x *inferenceWorkerServiceProcessTasksServer) Send(m *v1.ProcessTasksResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *inferenceWorkerServiceProcessTasksServer) Recv() (*v1.ProcessTasksRequest, error) {
	m := new(v1.ProcessTasksRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// InferenceWorkerService_ServiceDesc is the grpc.ServiceDesc for InferenceWorkerService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var InferenceWorkerService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "llmoperator.inference.server.v1.InferenceWorkerService",
	HandlerType: (*InferenceWorkerServiceServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "ProcessTasks",
			Handler:       _InferenceWorkerService_ProcessTasks_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "api/v1/legacy/inference_server_worker.proto",
}
