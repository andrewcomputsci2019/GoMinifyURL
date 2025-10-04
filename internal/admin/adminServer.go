package admin

import (
	"GOMinifyURL/internal/proto"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type GrpcAdminServer struct {
	proto.UnimplementedDiscoveryServer
	grpcServer *grpc.Server
	tlsConfig  *tls.Config
	address    string
}

type option func(*GrpcAdminServer) error

func WithSecure(certFile, keyFile, caFile string) option {
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

func NewGrpcAdminServer(addr string, opts ...option) (*GrpcAdminServer, error) {
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
