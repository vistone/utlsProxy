package taskapi

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
)

type jsonCodec struct{}

func (jsonCodec) Marshal(v interface{}) ([]byte, error) { return json.Marshal(v) }
func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
func (jsonCodec) Name() string { return "json" }

var JSONCodec encoding.Codec = &jsonCodec{}

func init() {
	encoding.RegisterCodec(JSONCodec)
}

type TaskRequest struct {
	ClientID string `json:"client_id"`
	Path     string `json:"path"`
}

type TaskResponse struct {
	ClientID     string `json:"client_id"`
	StatusCode   int32  `json:"status_code"`
	Body         []byte `json:"body"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type TaskServiceServer interface {
	Execute(context.Context, *TaskRequest) (*TaskResponse, error)
}

type UnimplementedTaskServiceServer struct{}

func (UnimplementedTaskServiceServer) Execute(context.Context, *TaskRequest) (*TaskResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Execute not implemented")
}

func RegisterTaskServiceServer(s *grpc.Server, srv TaskServiceServer) {
	s.RegisterService(&TaskService_ServiceDesc, srv)
}

func _TaskService_Execute_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(TaskRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).Execute(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/taskpb.TaskService/Execute",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).Execute(ctx, req.(*TaskRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var TaskService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "taskpb.TaskService",
	HandlerType: (*TaskServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Execute",
			Handler:    _TaskService_Execute_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/task.proto",
}

type TaskServiceClient interface {
	Execute(ctx context.Context, in *TaskRequest, opts ...grpc.CallOption) (*TaskResponse, error)
}

type taskServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTaskServiceClient(cc grpc.ClientConnInterface) TaskServiceClient {
	return &taskServiceClient{cc}
}

func (c *taskServiceClient) Execute(ctx context.Context, in *TaskRequest, opts ...grpc.CallOption) (*TaskResponse, error) {
	out := new(TaskResponse)
	err := c.cc.Invoke(ctx, "/taskpb.TaskService/Execute", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func NewServer(opts ...grpc.ServerOption) *grpc.Server {
	base := []grpc.ServerOption{grpc.ForceServerCodec(JSONCodec)}
	base = append(base, opts...)
	return grpc.NewServer(base...)
}

func Dial(address string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	base := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec)),
	}
	base = append(base, opts...)
	return grpc.Dial(address, base...)
}
