package taskapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

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
	FilePath     string `json:"file_path,omitempty"` // 大响应体的文件路径，如果设置则优先使用文件而不是body
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

// formatAddress 格式化地址，确保IPv6地址使用方括号包裹
func formatAddress(address string) string {
	// 如果地址已经包含方括号，直接返回
	if strings.Contains(address, "[") && strings.Contains(address, "]") {
		return address
	}
	
	// 尝试解析地址，检查是否是IPv6地址
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		// 如果解析失败，尝试手动处理IPv6地址格式（如 "2607:8700:5500:2943::2:9091"）
		// 查找最后一个冒号，尝试将其作为端口分隔符
		lastColonIndex := strings.LastIndex(address, ":")
		if lastColonIndex > 0 && lastColonIndex < len(address)-1 {
			// 检查最后一个冒号后面的部分是否是数字（端口）
			possiblePort := address[lastColonIndex+1:]
			possibleHost := address[:lastColonIndex]
			
			// 尝试解析端口号
			var portNum int
			if _, err := fmt.Sscanf(possiblePort, "%d", &portNum); err == nil && portNum > 0 && portNum <= 65535 {
				// 可能是端口号，检查前面的部分是否是IPv6地址
				ip := net.ParseIP(possibleHost)
				if ip != nil && ip.To4() == nil && ip.To16() != nil {
					// 是IPv6地址，使用方括号包裹
					return fmt.Sprintf("[%s]:%s", possibleHost, possiblePort)
				}
			}
		}
		// 如果无法解析，直接返回原地址
		return address
	}
	
	// 解析IP地址
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() == nil && ip.To16() != nil {
		// 是IPv6地址，使用方括号包裹
		return fmt.Sprintf("[%s]:%s", host, port)
	}
	
	// IPv4地址或域名，直接返回
	return address
}

func Dial(address string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	// 格式化地址，确保IPv6地址使用方括号
	formattedAddr := formatAddress(address)
	
	base := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec)),
	}
	base = append(base, opts...)
	return grpc.Dial(formattedAddr, base...)
}
