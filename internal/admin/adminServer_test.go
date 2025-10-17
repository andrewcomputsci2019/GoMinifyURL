package admin

import (
	"GOMinifyURL/internal/proto"
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var httpServer *http.Server

var httpAddress string

func getFreePort() (string, error) {
	for i := 0; i < 5; i++ {
		ln, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			continue
		}
		addr := ln.Addr().String()
		ln.Close()
		return addr, nil
	}
	return "", fmt.Errorf("unable to find free port after retries")
}

func TestNewGrpcAdminServer(t *testing.T) {
	_, err := NewGrpcAdminServer("localhost:8080")
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error = %v", err)
		return
	}
	_, err = NewGrpcAdminServer("localhost:8080", WithLeaseTimes(time.Second*20))
	if err != nil {
		t.Errorf("NewGrpcAdminServer() when adding a lease duration option error = %v", err)
	}
}

func TestGrpcAdminServer_ListenAndServeAndClose(t *testing.T) {
	grpcServer, err := NewGrpcAdminServer("localhost:8080")
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error = %v", err)
		return
	}
	go func() { _ = grpcServer.ListenAndServe() }()
	time.Sleep(150 * time.Millisecond)
	// now need to dial grpc server and see if we get an error or not
	conn, err := grpc.NewClient("localhost:8080", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error, failed to create grpc Client = %v", err)
		return
	}
	defer conn.Close()
	hc := healthpb.NewHealthClient(conn)
	cxt, cFun := context.WithDeadline(context.Background(), time.Now().Add(time.Second*1))
	hr, err := hc.Check(cxt, &healthpb.HealthCheckRequest{Service: proto.Discovery_ServiceDesc.ServiceName})
	cFun()
	if err != nil {
		t.Errorf("Unlikely grpc server is running: %v", err)
		return
	}
	if hr.Status != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("Grpc server is running but not servering request: %v", hr)
		return
	}
	grpcServer.Close()
	<-time.After(time.Millisecond * 100)
	// now retry command
	cxt, cFun = context.WithDeadline(context.Background(), time.Now().Add(time.Second*1))
	defer cFun()
	_, err = hc.Check(cxt, &healthpb.HealthCheckRequest{Service: proto.Discovery_ServiceDesc.ServiceName})
	if err == nil {
		t.Errorf("Grpc server is suppose to be closed yet service post flight request")
		return
	}
}

func TestGrpcAdminServer_RegisterService(t *testing.T) {
	t.Parallel()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	grpcServer, err := NewGrpcAdminServer(addr)
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error = %v", err)
		return
	}
	go func() {
		err := grpcServer.ListenAndServe()
		if err != nil {
			t.Errorf("GRPC rpc call of RegisterService failed: %v", err)
		}
	}()
	t.Cleanup(func() {
		grpcServer.Close()
	})
	time.Sleep(150 * time.Millisecond)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error, failed to create grpc Client = %v", err)
		return
	}
	defer conn.Close()
	<-time.After(time.Millisecond * 100)
	dc := proto.NewDiscoveryClient(conn)
	cxt, cFun := context.WithDeadline(context.Background(), time.Now().Add(time.Second*2))
	defer cFun()
	res, err := dc.RegisterService(cxt, &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	if err != nil {
		t.Errorf("GRPC rpc call of RegisterService failed to register service: %v", err)
		return
	}
	if res.RequestTtl != int32(LeaseDuration.Seconds()) {
		t.Errorf("Lease duration should be %v, but %v", int32(LeaseDuration.Seconds()), res.RequestTtl)
		return
	}
}

func TestGrpcAdminServer_DeRegisterService(t *testing.T) {
	t.Parallel()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	grpcServer, err := NewGrpcAdminServer(addr)
	if err != nil {
		t.Errorf("DeRegisterService() error = %v", err)
		return
	}
	go func() {
		err := grpcServer.ListenAndServe()
		if err != nil {
			t.Errorf("GRPC rpc call of DeRegisterService failed: %v", err)
		}
	}()
	t.Cleanup(func() {
		grpcServer.Close()
	})
	time.Sleep(150 * time.Millisecond)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Errorf("DeRegisterService() error, failed to create grpc Client = %v", err)
		return
	}
	defer conn.Close()
	dc := proto.NewDiscoveryClient(conn)
	cxt, cFun := context.WithDeadline(context.Background(), time.Now().Add(time.Second*2))
	res, err := dc.RegisterService(cxt, &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	cFun()
	if err != nil {
		t.Errorf("GRPC rpc call of RegisterService failed to register service: %v", err)
		return
	}
	cxt, cFun = context.WithDeadline(context.Background(), time.Now().Add(time.Second*3))
	deRes, err := dc.DeRegisterService(cxt, &proto.DeRegistrationMessage{
		InstanceName: "1",
		Nonce:        res.Nonce,
	})
	cFun()
	if err != nil {
		t.Errorf("GRPC rpc call of DeRegisterService failed to deregister service: %v", err)
		return
	}
	if !deRes.Success {
		t.Errorf("GRPC rpc call of DeRegisterService return an unsuccessful response: %v", deRes)
		return
	}
}

func TestGrpcAdminServer_Heartbeat(t *testing.T) {
	t.Parallel()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	grpcServer, err := NewGrpcAdminServer(addr, WithLeaseTimes(time.Second*6))
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error = %v", err)
		return
	}
	go func() {
		err := grpcServer.ListenAndServe()
		if err != nil {
			t.Errorf("GRPC rpc call of RequestServiceList failed: %v", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error, failed to create grpc Client = %v", err)
		return
	}
	defer conn.Close()
	dc := proto.NewDiscoveryClient(conn)
	cxt, cFun := context.WithDeadline(context.Background(), time.Now().Add(time.Second*2))
	_, err = dc.RegisterService(cxt, &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	cFun()
	if err != nil {
		t.Errorf("GRPC rpc call of RegisterService failed to register service: %v", err)
		return
	}
	// now we need to send a heartbeat to the server
	go func() {
		stream, err := dc.Heartbeat(context.Background())
		if err != nil {
			t.Errorf("GRPC Could not construct rpc stream: %v", err)
			return
		}
		defer stream.CloseSend()
		<-time.After(time.Second * 2)
		err = stream.Send(&proto.HeartBeat{
			SeqNumber:    0,
			InstanceName: "1",
			Status:       proto.NodeStatus_HEALTHY,
		})
		if err != nil {
			t.Errorf("GRPC rpc call of HeartBeat failed: %v", err)
			return
		}
		<-time.After(time.Second * 2)
		err = stream.Send(&proto.HeartBeat{
			SeqNumber:    0,
			InstanceName: "1",
			Status:       proto.NodeStatus_HEALTHY,
		})
		if err != nil {
			t.Errorf("GRPC rpc call of HeartBeat failed: %v", err)
			return
		}
	}()
	<-time.After(time.Second * 5) // for sure lease should not be expired
	if t.Failed() {
		return
	}
	_, err = grpcServer.leaseManager.GetServiceInfo("1")
	if err != nil {
		t.Errorf("Grpc heartbeat did not exitend lease: %v", err)
		return
	}
}

func TestGrpcAdminServer_RequestServiceList(t *testing.T) {
	t.Parallel()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	grpcServer, err := NewGrpcAdminServer(addr, WithLeaseTimes(time.Second*10))
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error = %v", err)
		return
	}
	go func() {
		err := grpcServer.ListenAndServe()
		if err != nil {
			t.Errorf("GRPC rpc call of RequestServiceList failed: %v", err)
		}
	}()
	t.Cleanup(func() {
		grpcServer.Close()
	})
	time.Sleep(150 * time.Millisecond)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error, failed to create grpc Client = %v", err)
		return
	}
	defer conn.Close()
	dc := proto.NewDiscoveryClient(conn)
	cxt, cFun := context.WithDeadline(context.Background(), time.Now().Add(time.Second*2))
	_, err = dc.RegisterService(cxt, &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	cFun()
	if err != nil {
		t.Errorf("GRPC rpc call of RegisterService failed to register service: %v", err)
		return
	}
	cxt, cFun = context.WithDeadline(context.Background(), time.Now().Add(time.Second*2))
	_, err = dc.RegisterService(cxt, &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "2",
		DialAddr:     "just-a-test",
	})
	cFun()
	if err != nil {
		t.Errorf("GRPC rpc call of RegisterService failed to register service: %v", err)
		return
	}
	cxt, cFun = context.WithDeadline(context.Background(), time.Now().Add(time.Second*2))
	_, err = dc.RegisterService(cxt, &proto.RegistrationMessage{
		ServiceName:  "not-test-service",
		InstanceName: "3",
		DialAddr:     "just-a-test",
	})
	cFun()
	if err != nil {
		t.Errorf("GRPC rpc call of RegisterService failed to register service: %v", err)
		return
	}

	serviceMap := map[string]bool{"1": false, "2": false}
	cxt, cFun = context.WithDeadline(context.Background(), time.Now().Add(time.Second*2))
	serviceList, err := dc.RequestServiceList(cxt, &proto.ServiceListRequest{ServiceName: "test-service"})
	cFun()
	if err != nil {
		t.Errorf("GRPC rpc call of RequestServiceList failed: %v", err)
		return
	}
	for _, service := range serviceList.Instances {
		serviceMap[service.InstanceName] = true
	}
	if len(serviceList.Instances) != 2 || !serviceMap["1"] || !serviceMap["2"] {
		t.Fatalf("expected instances 1 and 2 only, got %v", serviceList.Instances)
	}
}

// todo admin http server test
