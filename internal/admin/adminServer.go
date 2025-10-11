package admin

import (
	"GOMinifyURL/internal/api/admin"
	"GOMinifyURL/internal/proto"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type MiddlewareFunc func(http.Handler) http.Handler
type HTTPAdminServer struct {
	addr            string
	grpcAdminServer *GrpcAdminServer
	mws             []MiddlewareFunc
	handler         http.Handler
}

func WithMiddleWares(middleWares []MiddlewareFunc) HTTPOption {
	return func(server *HTTPAdminServer) error {
		server.mws = append([]MiddlewareFunc(nil), middleWares...)
		return nil
	}
}

func NewHTTPAdminServer(addr string, grpcAdminServer *GrpcAdminServer, opts ...HTTPOption) (*HTTPAdminServer, error) {

	if grpcAdminServer == nil {
		return nil, errors.New("grpcAdminServer is nil")
	}

	if addr == "" {
		return nil, errors.New("addr is empty")
	}

	server := &HTTPAdminServer{addr: addr, grpcAdminServer: grpcAdminServer}

	for _, opt := range opts {
		err := opt(server)
		if err != nil {
			return nil, err
		}
	}

	if len(server.mws) == 0 {
		server.handler = admin.HandlerFromMuxWithBaseURL(server, http.NewServeMux(), "/api_admin")
	} else {
		server.handler = admin.HandlerWithOptions(server, admin.StdHTTPServerOptions{
			BaseURL:    "/api_admin",
			BaseRouter: http.NewServeMux(),
		})
	}

	return server, nil
}

func (H *HTTPAdminServer) StartAndListen() error {

	return nil
}

func (H *HTTPAdminServer) GetServices(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	panic("implement me")
	// todo invoke a call of the getService rpc call
}

func (H *HTTPAdminServer) CheckHealth(w http.ResponseWriter, r *http.Request, params admin.CheckHealthParams) {
	//TODO implement me
	panic("implement me")
}

func (H *HTTPAdminServer) RemoveService(w http.ResponseWriter, r *http.Request, params admin.RemoveServiceParams) {
	//TODO implement me
	panic("implement me")
}

var _ admin.ServerInterface = (*HTTPAdminServer)(nil)

type NodeHealth = proto.NodeStatus

const (
	Healthy  = proto.NodeStatus_HEALTHY
	Degraded = proto.NodeStatus_SICK
	Quiting  = proto.NodeStatus_QUITING
)

type serviceWithRegInfo struct {
	serviceName   string
	serviceId     string
	serviceHealth NodeHealth
}

type GrpcAdminServer struct {
	proto.UnimplementedDiscoveryServer
	grpcServer *grpc.Server
	tlsConfig  *tls.Config
	address    string
	contextMap map[string]context.CancelFunc
	rw         sync.RWMutex
	wg         sync.WaitGroup
}

type HTTPOption func(*HTTPAdminServer) error

type httpOption func(*HTTPAdminServer) error

type Option func(*GrpcAdminServer) error

type option func(*GrpcAdminServer) error

func WithSecure(certFile, keyFile, caFile string) Option {
	return func(s *GrpcAdminServer) error {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("could not load client certificate: %w", err)
		}

		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return fmt.Errorf("could not read ca certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
			return errors.New("failed to append ca certificate")
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		}
		s.tlsConfig = tlsConfig
		return nil
	}
}

func NewGrpcAdminServer(addr string, opts ...Option) (*GrpcAdminServer, error) {
	adminServer := &GrpcAdminServer{}
	for _, opt := range opts {
		if err := opt(adminServer); err != nil {
			return nil, err
		}
	}
	var grpcOpts []grpc.ServerOption
	if adminServer.tlsConfig != nil {
		creds := credentials.NewTLS(adminServer.tlsConfig)
		grpcOpts = append(grpcOpts, grpc.Creds(creds))
	}
	adminServer.grpcServer = grpc.NewServer(grpcOpts...)
	proto.RegisterDiscoveryServer(adminServer.grpcServer, adminServer)
	adminServer.address = addr
	adminServer.contextMap = make(map[string]context.CancelFunc)
	adminServer.wg = sync.WaitGroup{}
	adminServer.rw = sync.RWMutex{}
	return adminServer, nil
}

func (s *GrpcAdminServer) ListenAndServe() error {
	lis, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	return s.grpcServer.Serve(lis)
}

func (s *GrpcAdminServer) RegisterService(context.Context, *proto.RegistrationMessage) (*proto.RegistrationResponse, error) {

	return nil, status.Errorf(codes.Unimplemented, "method RegisterService not implemented")
}
func (s *GrpcAdminServer) Heartbeat(grpc.BidiStreamingServer[proto.HeartBeat, proto.HeartBeatResponse]) error {
	return status.Errorf(codes.Unimplemented, "method Heartbeat not implemented")
}
func (s *GrpcAdminServer) RequestServiceList(context.Context, *proto.ServiceListRequest) (*proto.ServiceListResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RequestServiceList not implemented")
}
func (s *GrpcAdminServer) DeRegisterService(context.Context, *proto.DeRegistrationMessage) (*proto.DeRegistrationResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeRegisterService not implemented")
}
