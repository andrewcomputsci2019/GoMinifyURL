package admin

import (
	"GOMinifyURL/internal/api/admin"
	"GOMinifyURL/internal/middleware/auth"
	proto "GOMinifyURL/internal/proto/admin"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
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
	server          http.Server
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

func NewHTTPAdminServerWithValidationHandlers(addr string, grpcAdminServer *GrpcAdminServer, opts []HTTPOption, validationErrHandling ...auth.ErrorRule) (*HTTPAdminServer, error) {
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
	// when testing environment is declared do not add auth
	localTestingEnv := viper.GetBool("TESTING_ENV")
	if localTestingEnv {
		if err := loadAuthValidationMiddleWare(server, validationErrHandling...); err != nil {
			return nil, err
		}
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
	// when testing environment is declared do not add auth
	localTestingEnv := viper.GetBool("TESTING_ENV")
	if !localTestingEnv {
		if err := loadAuthValidationMiddleWare(server); err != nil {
			return nil, err
		}
	}
	return server, nil
}
func (H *HTTPAdminServer) Close() error {
	cxt, cancelFunc := context.WithTimeout(context.Background(), time.Second*10)
	defer cancelFunc()
	return H.server.Shutdown(cxt)
}
func (H *HTTPAdminServer) StartAndListen() error {

	H.server = http.Server{
		Addr:    H.addr,
		Handler: H.handler,
	}
	return H.server.ListenAndServe()
}

func (H *HTTPAdminServer) GetServiceList(w http.ResponseWriter, r *http.Request, serviceClass string) {
	// this is basically a wrapper around grpc call
	list, err := H.grpcAdminServer.RequestServiceList(r.Context(), &proto.ServiceListRequest{ServiceName: serviceClass})
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	serviceList := make([]admin.Service, 0, len(list.Instances))
	for _, instance := range list.Instances {
		serviceList = append(serviceList, admin.Service{
			Id:   instance.InstanceName,
			Name: instance.ServiceName,
			Url:  instance.DialAddr,
		})
	}
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(serviceList); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return
}

func (H *HTTPAdminServer) GetServices(w http.ResponseWriter, r *http.Request) {
	allServices := H.grpcAdminServer.leaseManager.GetAllServices()
	//need to convert all of this into json
	jsonList := make([]admin.Service, 0, len(allServices))

	for _, service := range allServices {
		jsonList = append(jsonList, admin.Service{
			Id:   service.serviceId,
			Name: service.serviceName,
			Url:  service.address,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	jsonEncoder := json.NewEncoder(w)
	if err := jsonEncoder.Encode(jsonList); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return

}

func (H *HTTPAdminServer) GetServiceInfo(w http.ResponseWriter, r *http.Request, params admin.GetServiceInfoParams) {
	serviceInfo, err := H.grpcAdminServer.leaseManager.GetServiceInfo(params.Id)
	if err != nil {
		http.Error(w, fmt.Errorf("failed to get service [%s] info as service does not exist", params.Id).Error(), http.StatusNotFound)
		return
	}

	healthString := admin.Healthy
	switch serviceInfo.ServiceRegInfo.serviceHealth {
	case Healthy:
		healthString = admin.Healthy
	case Degraded:
		healthString = admin.Sick
	case Quiting:
		healthString = admin.Quiting
	}

	// convert service info into know json data type
	data := admin.ServiceHealth{
		LeaseInformation: serviceInfo.lease.leaseTime,
		Service: admin.Service{
			Id:   serviceInfo.ServiceRegInfo.serviceId,
			Name: serviceInfo.ServiceRegInfo.serviceName,
			Url:  serviceInfo.ServiceRegInfo.address,
		},
		Status: healthString,
	}
	w.Header().Set("Content-Type", "application/json")
	jsonEncoder := json.NewEncoder(w)
	if err := jsonEncoder.Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	return
}

func (H *HTTPAdminServer) RemoveService(w http.ResponseWriter, r *http.Request, params admin.RemoveServiceParams) {

	// grab local vars needed
	grpcServer := H.grpcAdminServer
	// also need to get nonce
	nonce := uint64(0)
	nonce, err := grpcServer.leaseManager.getServiceNonce(params.Id)
	if err != nil {
		http.Error(w, fmt.Errorf("service: [%s] does not exist", params.Id).Error(), http.StatusNotFound)
		return
	}

	// call into grpc layer
	if deRegMessage, err := grpcServer.DeRegisterService(r.Context(), &proto.DeRegistrationMessage{
		InstanceName: params.Id,
		Nonce:        nonce,
	}); err != nil || deRegMessage == nil || !deRegMessage.Success {
		// failed to remove service from internal registry
		http.Error(w, fmt.Errorf("failed to remove registered service: [%s]", params.Id).Error(), http.StatusInternalServerError)
		return
	} else {
		w.WriteHeader(http.StatusOK)
		return
	}
}

var _ admin.ServerInterface = (*HTTPAdminServer)(nil)

type NodeHealth = proto.NodeStatus

const (
	Healthy  = proto.NodeStatus_HEALTHY
	Degraded = proto.NodeStatus_SICK
	Quiting  = proto.NodeStatus_QUITING
)

type GrpcAdminServer struct {
	proto.UnimplementedDiscoveryServer
	grpcServer    *grpc.Server
	hs            *health.Server
	tlsConfig     *tls.Config
	address       string
	leaseManager  *LeaseManager
	leaseDuration time.Duration
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
	adminServer.leaseManager = NewLeaseManager(adminServer.leaseDuration)
	return adminServer, nil
}

func (s *GrpcAdminServer) ListenAndServe() error {

	s.hs = health.NewServer()
	healthpb.RegisterHealthServer(s.grpcServer, s.hs)
	// fyi this will change if you add a package name to the proto file
	s.hs.SetServingStatus("discovery", healthpb.HealthCheckResponse_SERVING)
	lis, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	return s.grpcServer.Serve(lis)
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

	serviceRegInfo := &ServiceWithRegInfo{
		serviceName:   regMsg.ServiceName,
		serviceId:     regMsg.InstanceName,
		address:       regMsg.DialAddr,
		nonce:         binary.LittleEndian.Uint64(nonceBuf),
		serviceHealth: Healthy,
		seqNum:        0,
	}
	err := s.leaseManager.AddService(serviceRegInfo)
	if err != nil {
		return nil, err
	}

	response := &proto.RegistrationResponse{
		Nonce:      serviceRegInfo.nonce,
		SeqStart:   serviceRegInfo.seqNum,
		RequestTtl: int32(s.leaseDuration.Seconds()),
	}

	return response, nil
}
func (s *GrpcAdminServer) Heartbeat(stream grpc.BidiStreamingServer[proto.HeartBeat, proto.HeartBeatResponse]) error {

	// need to get service name
	streamCxt := stream.Context()
	msg, err := stream.Recv()
	defer func() {
		log.Printf("[HeartBeat]: handler closed stream connection with client %v", msg.InstanceName)
	}()
	if err != nil {
		return status.Error(codes.Unknown, err.Error())
	}

	cxt, err := s.leaseManager.GetContext(msg.InstanceName)
	if err != nil {
		return status.Error(codes.NotFound, "instance not found in lease context map")
	}
	seqNumVerifier, err := s.leaseManager.getServiceSequenceStart(msg.InstanceName)
	if err != nil {
		return status.Error(codes.NotFound, "instance not found in service lookup")
	}
	prevHealth := msg.Status
	_, err = s.handleServiceHeartBeat(msg, &seqNumVerifier, &prevHealth)
	if err != nil {
		return status.Error(codes.NotFound, err.Error())
	}
	msgChan := make(chan *proto.HeartBeat)
	errChan := make(chan error, 1)
	go func() {
		defer close(msgChan)
		defer close(errChan)
		for {
			msg, err := stream.Recv()
			if err != nil {
				select {
				case errChan <- status.Errorf(codes.Unknown, "%v", err):
					return
				case <-streamCxt.Done():
					return
				}
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
			return status.Error(codes.Canceled, streamCxt.Err().Error())
		case err = <-errChan:
			return status.Error(codes.Unknown, err.Error())
		case msg, ok = <-msgChan:
			if !ok {
				return status.Error(codes.Canceled, "stream reader closed")
			}
			beat, err := s.handleServiceHeartBeat(msg, &seqNumVerifier, &prevHealth)
			if err != nil {
				err := stream.Send(&proto.HeartBeatResponse{
					Feedback: &proto.HeartBeatResponse_Error{
						Error: err.Error(),
					},
				})
				if err != nil {
					return status.Error(codes.Internal, err.Error())
				}
			}
			if !beat {
				err := stream.Send(&proto.HeartBeatResponse{
					Feedback: &proto.HeartBeatResponse_Error{
						Error: "Service heartbeat did not extend lease, service marked as sick",
					},
				})
				if err != nil {
					return status.Error(codes.Internal, err.Error())
				}
			}
		}
	}
}

func (s *GrpcAdminServer) handleServiceHeartBeat(msg *proto.HeartBeat, seqNum *uint64, status *NodeHealth) (bool, error) {
	if seqNum == nil {
		return false, nil
	}
	instanceName := msg.InstanceName
	newStatus := msg.Status
	// out of order packets something is not right
	if msg.SeqNumber < *seqNum {
		// old / duplicate packet
		return false, nil
	} else if msg.SeqNumber == *seqNum {
		*seqNum += 1
	} else { // jumped ahead
		*seqNum = msg.SeqNumber + 1
		newStatus = proto.NodeStatus_SICK
	}
	if err := s.leaseManager.ExtendLease(instanceName); err != nil {
		return false, err
	} else {
		// update status if it is different from before
		if newStatus != *status {
			s.leaseManager.updateServiceHealth(instanceName, newStatus)
			*status = newStatus
		}
		return true, nil
	}
}
func (s *GrpcAdminServer) RequestServiceList(cxt context.Context, message *proto.ServiceListRequest) (*proto.ServiceListResponse, error) {

	list, err := s.leaseManager.GetServiceList(message.ServiceName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "could not get service list: %v", err)
	}
	// don't continue if job finished during fetching of services
	select {
	case <-cxt.Done():
		return nil, status.Error(codes.Canceled, cxt.Err().Error())
	default:
	}
	ptrList := make([]*proto.ServiceInfo, 0, len(list))
	for _, service := range list {

		ptrList = append(ptrList, &proto.ServiceInfo{
			ServiceName:  service.serviceName,
			InstanceName: service.serviceId,
			DialAddr:     service.address,
			LatestStatus: service.serviceHealth,
		})
	}
	resp := &proto.ServiceListResponse{
		Instances: ptrList,
	}
	return resp, nil
}
func (s *GrpcAdminServer) DeRegisterService(_ context.Context, deregMsg *proto.DeRegistrationMessage) (*proto.DeRegistrationResponse, error) {
	if serviceNonce, err := s.leaseManager.getServiceNonce(deregMsg.InstanceName); err != nil {
		return nil, err
	} else {
		if serviceNonce != deregMsg.Nonce {
			return nil, status.Error(codes.FailedPrecondition, "nonce mismatch")
		}
		s.leaseManager.RemoveServiceNonBlock(deregMsg.InstanceName)
		return &proto.DeRegistrationResponse{
			Success: true,
		}, nil
	}
}

func (s *GrpcAdminServer) Close() {
	s.hs.Shutdown()
	s.grpcServer.GracefulStop()
}
