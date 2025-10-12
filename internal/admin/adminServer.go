package admin

import (
	"GOMinifyURL/internal/api/admin"
	"GOMinifyURL/internal/middleware/auth"
	"GOMinifyURL/internal/proto"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	LeaseDuration = 30 * time.Second
)

type MiddlewareFunc = admin.MiddlewareFunc

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

func loadAuthValidationMiddleWare(H *HTTPAdminServer, validationErrHandling ...auth.ErrorRule) error {

	authValidationMiddleWare, err := getAuthHandler(validationErrHandling...)
	if err != nil {
		return err
	}
	H.handler = authValidationMiddleWare(H.handler)
	return nil
}

func NewHHTTPAdminServerWithValidationHandlers(addr string, grpcAdminServer *GrpcAdminServer, opts []HTTPOption, validationErrHandling ...auth.ErrorRule) (*HTTPAdminServer, error) {
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
			BaseURL:     "/api_admin",
			BaseRouter:  http.NewServeMux(),
			Middlewares: append([]MiddlewareFunc(nil), server.mws...),
		})
	}
	if err := loadAuthValidationMiddleWare(server, validationErrHandling...); err != nil {
		return nil, err
	}
	return server, nil
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
			BaseURL:     "/api_admin",
			BaseRouter:  http.NewServeMux(),
			Middlewares: append([]MiddlewareFunc(nil), server.mws...),
		})
	}
	if err := loadAuthValidationMiddleWare(server); err != nil {
		return nil, err
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
	nonce         uint64
	lease         *Lease
	seqNum        uint64
}

type GrpcAdminServer struct {
	proto.UnimplementedDiscoveryServer
	grpcServer         *grpc.Server
	tlsConfig          *tls.Config
	address            string
	serviceListMap     map[string][]*serviceWithRegInfo
	serviceLookup      map[string]*serviceWithRegInfo
	contextMap         map[string]context.Context
	rwServiceList      sync.RWMutex
	rwServiceLookupMap sync.RWMutex
	rwContextMap       sync.RWMutex
	leaseManager       *LeaseManager
	leaseDuration      time.Duration
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

func WithLeaseTimes(leaseLength time.Duration) Option {
	return func(s *GrpcAdminServer) error {
		s.leaseDuration = leaseLength
		return nil
	}
}

func NewGrpcAdminServer(addr string, opts ...Option) (*GrpcAdminServer, error) {
	adminServer := &GrpcAdminServer{}
	adminServer.leaseDuration = LeaseDuration
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
	adminServer.serviceListMap = make(map[string][]*serviceWithRegInfo)
	adminServer.serviceLookup = make(map[string]*serviceWithRegInfo)
	adminServer.contextMap = make(map[string]context.Context)
	adminServer.rwServiceList = sync.RWMutex{}
	adminServer.rwServiceLookupMap = sync.RWMutex{}
	adminServer.rwContextMap = sync.RWMutex{}
	adminServer.leaseManager = NewLeaseManager(adminServer.leaseDuration)
	return adminServer, nil
}

func (s *GrpcAdminServer) ListenAndServe() error {
	lis, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	return s.grpcServer.Serve(lis)
}

func (s *GrpcAdminServer) getLeaseOfService(instanceId string) *Lease {
	s.rwServiceLookupMap.RLock()
	defer s.rwServiceLookupMap.RUnlock()
	if data, ok := s.serviceLookup[instanceId]; ok {
		return data.lease
	}
	return nil
}

func (s *GrpcAdminServer) cleanUpAfterDeRegistration() {}

func (s *GrpcAdminServer) RegisterService(cxt context.Context, regMsg *proto.RegistrationMessage) (*proto.RegistrationResponse, error) {
	select {
	case <-cxt.Done():
		return nil, status.Error(codes.Canceled, "context canceled")
	default:
		break
	}
	nonceBuf := make([]byte, 8)
	if _, err := rand.Read(nonceBuf); err != nil {
		return nil, status.Errorf(codes.Unknown, "failed to generate nonce: %v", err)
	}

	cxt, cancelFunc := context.WithCancelCause(context.Background())

	serviceRegInfo := &serviceWithRegInfo{
		serviceName:   regMsg.ServiceName,
		serviceId:     regMsg.InstanceName,
		nonce:         binary.LittleEndian.Uint64(nonceBuf),
		serviceHealth: Healthy,
		seqNum:        0,
		lease: &Lease{
			mutex:     sync.Mutex{},
			serviceId: regMsg.InstanceName,
			lease:     time.Now().Add(s.leaseDuration),
			ttl:       s.leaseDuration,
			version:   0,
			cancel:    cancelFunc,
		},
	}
	s.rwServiceList.Lock()
	s.serviceListMap[regMsg.ServiceName] = append(s.serviceListMap[regMsg.ServiceName], serviceRegInfo)
	s.rwServiceList.Unlock()

	s.rwServiceLookupMap.Lock()
	s.serviceLookup[regMsg.InstanceName] = serviceRegInfo
	s.rwServiceLookupMap.Unlock()

	s.rwContextMap.Lock()
	s.contextMap[regMsg.InstanceName] = cxt
	s.rwContextMap.Unlock()

	return nil, status.Errorf(codes.Unimplemented, "method RegisterService not implemented")
}
func (s *GrpcAdminServer) Heartbeat(stream grpc.BidiStreamingServer[proto.HeartBeat, proto.HeartBeatResponse]) error {
	/*
		todo add a defer statement to clean up lease info after this func closes
			or at least invoke a clean up from exit paths depending on what context closed
	*/
	// need to get service name
	streamCxt := stream.Context()
	msg, err := stream.Recv()
	if err != nil {
		return status.Error(codes.Unknown, err.Error())
	}
	s.rwContextMap.RLock()
	cxt, ok := s.contextMap[msg.InstanceName]
	s.rwContextMap.RUnlock()
	if !ok {
		return status.Error(codes.NotFound, "instance not found")
	}
	s.rwServiceLookupMap.RLock()
	service, ok := s.serviceLookup[msg.InstanceName]
	s.rwServiceLookupMap.RUnlock()
	if !ok {
		return status.Error(codes.NotFound, "instance not found")
	}
	seqNumVerrifer := service.seqNum
	msgChan := make(chan *proto.HeartBeat)
	errChan := make(chan error, 1)
	go func() {
		defer close(msgChan)
		defer close(errChan)
		for {
			msg, err := stream.Recv()
			if err != nil {
				errChan <- status.Errorf(codes.Unknown, err.Error())
				return
			}
			select {
			case <-streamCxt.Done():
				return
			case msgChan <- msg:
			}
		}
	}()
	for {
		var (
			ok  bool
			msg *proto.HeartBeat
		)
		select {
		case <-cxt.Done():
			return status.Error(codes.Canceled, cxt.Err().Error())
		case <-streamCxt.Done():
			return status.Error(codes.Canceled, cxt.Err().Error())
		case msg, ok = <-msgChan:
			if !ok {
				return status.Error(codes.Internal, "heartbeat failed")
			}
		}
	}
}
func (s *GrpcAdminServer) RequestServiceList(context.Context, *proto.ServiceListRequest) (*proto.ServiceListResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RequestServiceList not implemented")
}
func (s *GrpcAdminServer) DeRegisterService(context.Context, *proto.DeRegistrationMessage) (*proto.DeRegistrationResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeRegisterService not implemented")
}
