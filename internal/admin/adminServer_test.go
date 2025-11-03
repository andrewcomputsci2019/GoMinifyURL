package admin

import (
	"GOMinifyURL/internal/api/admin"
	"GOMinifyURL/internal/middleware/logger"
	proto "GOMinifyURL/internal/proto/admin"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

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

func TestMain(m *testing.M) {
	err := os.Setenv("TESTING_ENV", "true")
	viper.MustBindEnv("TESTING_ENV")
	if err != nil {
		return
	}
	os.Exit(m.Run())
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
	if res.RequestTtl.AsDuration() != LeaseDuration {
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

func addServiceHelper(t *testing.T, grpcServer *GrpcAdminServer, message *proto.RegistrationMessage) {
	t.Helper()
	cxt, cFun := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
	_, err := grpcServer.RegisterService(cxt, message)
	cFun()
	if err != nil {
		t.Errorf("GRPC rpc call of RegisterService failed: %v", err)
		return
	}
}

func createGrpcAdminServerAndServe(t *testing.T) *GrpcAdminServer {
	t.Helper()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return nil
	}
	adminServer, err := NewGrpcAdminServer(addr, WithLeaseTimes(time.Second*60))
	if err != nil {
		t.Errorf("NewGrpcAdminServer() error = %v", err)
		return nil
	}
	go func() {
		_ = adminServer.ListenAndServe()
	}()
	return adminServer
}

func TestNewHTTPAdminServer(t *testing.T) {
	test, err := NewGrpcAdminServer("localhost:7070")
	if err != nil {
		t.Errorf("NewHTTPAdminServer() error = %v", err)
	}
	_, err = NewHTTPAdminServer("localhost:8080", test)
	if err != nil {
		t.Errorf("NewHTTPAdminServer() error = %v", err)
	}
}

func TestHttpAdminServer_StartAndListenAndClose(t *testing.T) {
	grpcServer := createGrpcAdminServerAndServe(t)
	if grpcServer == nil {
		t.Errorf("GrpcAdminServer is nil")
		return
	}
	defer grpcServer.Close()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	httpServer, err := NewHTTPAdminServer(addr, grpcServer, WithMiddleWares([]MiddlewareFunc{logger.LoggingMiddleware()}))
	if err != nil {
		t.Errorf("NewHTTPAdminServer() error = %v", err)
	}
	defer func(httpServer *HTTPAdminServer) {
		err := httpServer.Close()
		if err != nil {
			t.Errorf("http server close() error = %v", err)
		}
	}(httpServer)
	go func() {
		_ = httpServer.StartAndListen()
	}()
	time.Sleep(150 * time.Millisecond)
	res, err := http.Get("http://" + addr + "/api_admin/getServices")
	if err != nil {
		t.Errorf("Http Server may not be serving content")
		return
	}
	err = res.Body.Close()
	if err != nil {
		return
	}
}

func TestHTTPAdminServer_GetServices(t *testing.T) {
	t.Parallel()
	grpcServer := createGrpcAdminServerAndServe(t)
	if grpcServer == nil {
		t.Errorf("GrpcAdminServer is nil")
		return
	}
	defer grpcServer.Close()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	defer grpcServer.Close()
	httpServer, err := NewHTTPAdminServer(addr, grpcServer, WithMiddleWares([]MiddlewareFunc{logger.LoggingMiddleware()}))
	if err != nil {
		t.Errorf("NewHTTPAdminServer() error = %v", err)
	}
	defer httpServer.Close()
	go func() {
		_ = httpServer.StartAndListen()
	}()
	time.Sleep(150 * time.Millisecond)
	addServiceHelper(t, grpcServer, &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	if t.Failed() {
		return
	}
	resp, err := http.Get("http://" + addr + "/api_admin/getServices")
	if err != nil {
		t.Errorf("Http Server may not be serving content")
		return
	}
	if resp.StatusCode != 200 {
		t.Errorf("Error fetching content from getServices endpoint: status code %v", resp.StatusCode)
		return
	}
	var jsonData []admin.Service
	err = json.NewDecoder(resp.Body).Decode(&jsonData)
	if err != nil {
		t.Errorf("Error decoding response from getServices endpoint")
		return
	}
	if len(jsonData) != 1 {
		t.Errorf("Expected 1 service, got %d in json response %v", len(jsonData), jsonData)
	}
	if jsonData[0].Id != "1" || jsonData[0].Name != "test-service" || jsonData[0].Url != "just-a-test" {
		t.Errorf("Data sent by server in json repsonse does not match service def {ID: 1, Name: 'test-service', URL: 'just-a-test': got %v", jsonData[0])
		return
	}
}

func TestHTTPAdminServer_GetServiceList(t *testing.T) {
	t.Parallel()
	grpcServer := createGrpcAdminServerAndServe(t)
	if grpcServer == nil {
		t.Errorf("GrpcAdminServer is nil")
		return
	}
	defer grpcServer.Close()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	defer grpcServer.Close()
	httpServer, err := NewHTTPAdminServer(addr, grpcServer, WithMiddleWares([]MiddlewareFunc{logger.LoggingMiddleware()}))
	if err != nil {
		t.Errorf("NewHTTPAdminServer() error = %v", err)
	}
	defer httpServer.Close()
	go func() {
		_ = httpServer.StartAndListen()
	}()
	time.Sleep(150 * time.Millisecond)
	addServiceHelper(t, grpcServer, &proto.RegistrationMessage{ServiceName: "test-service", InstanceName: "1", DialAddr: "just-a-test"})
	if t.Failed() {
		return
	}
	addServiceHelper(t, grpcServer, &proto.RegistrationMessage{ServiceName: "not-test-service", InstanceName: "2", DialAddr: "just-a-test"})
	if t.Failed() {
		return
	}
	resp, err := http.Get("http://" + addr + "/api_admin/getServiceList/test-service")
	if err != nil {
		t.Errorf("Http Server may not be serving content")
		return
	}
	if resp.StatusCode != 200 {
		t.Errorf("Error fetching content from getServiceList endpoint: status code %v", resp.StatusCode)
		return
	}
	var jsonData []admin.Service
	err = json.NewDecoder(resp.Body).Decode(&jsonData)
	if err != nil {
		t.Errorf("Error decoding response from getServiceList endpoint")
		return
	}
	if len(jsonData) != 1 {
		t.Errorf("Expected 1 service, got %d in json response %v", len(jsonData), jsonData)
	}
	if jsonData[0].Id != "1" {
		t.Errorf("Expected service id '1', got %v", jsonData[0])
		return
	}
	if jsonData[0].Name != "test-service" {
		t.Errorf("Expected service name 'test-service', got %v", jsonData[0])
		return
	}
}

func TestHTTPAdminServer_GetServiceInfo(t *testing.T) {
	t.Parallel()
	grpcServer := createGrpcAdminServerAndServe(t)
	if grpcServer == nil {
		t.Errorf("GrpcAdminServer is nil")
		return
	}
	defer grpcServer.Close()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	defer grpcServer.Close()
	httpServer, err := NewHTTPAdminServer(addr, grpcServer, WithMiddleWares([]MiddlewareFunc{logger.LoggingMiddleware()}))
	if err != nil {
		t.Errorf("NewHTTPAdminServer() error = %v", err)
	}
	defer httpServer.Close()
	go func() {
		_ = httpServer.StartAndListen()
	}()
	time.Sleep(150 * time.Millisecond)
	addServiceHelper(t, grpcServer, &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	if t.Failed() {
		return
	}
	serviceINFO, err := grpcServer.leaseManager.GetServiceInfo("1")
	if err != nil {
		t.Errorf("GetServiceInfo() error = %v", err)
		return
	}
	resp, err := http.Get("http://" + addr + "/api_admin/serviceInfo?id=1")
	if err != nil {
		t.Errorf("Http Server may not be serving content")
		return
	}
	if resp.StatusCode != 200 {
		t.Errorf("Error fetching content from getServiceInfo endpoint: status code %v", resp.StatusCode)
		return
	}
	var jsonData admin.ServiceHealth
	err = json.NewDecoder(resp.Body).Decode(&jsonData)
	if err != nil {
		t.Errorf("Error decoding response from getServiceInfo endpoint: %v", err)
		return
	}
	if jsonData.Service.Name != "test-service" || jsonData.Service.Id != "1" || !jsonData.LeaseInformation.Equal(serviceINFO.lease.leaseTime) {
		t.Errorf("data returned by getServiceInfo does not match service info expcted %v but got %v", serviceINFO, jsonData)
		return
	}
}

func TestHTTPAdminServer_RemoveService(t *testing.T) {
	t.Parallel()
	grpcServer := createGrpcAdminServerAndServe(t)
	if grpcServer == nil {
		t.Errorf("GrpcAdminServer is nil")
		return
	}
	defer grpcServer.Close()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("GetFreePort() error = %v", err)
		return
	}
	defer grpcServer.Close()
	httpServer, err := NewHTTPAdminServer(addr, grpcServer, WithMiddleWares([]MiddlewareFunc{logger.LoggingMiddleware()}))
	if err != nil {
		t.Errorf("NewHTTPAdminServer() error = %v", err)
	}
	defer httpServer.Close()
	go func() {
		_ = httpServer.StartAndListen()
	}()
	time.Sleep(150 * time.Millisecond)
	addServiceHelper(t, grpcServer, &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	if t.Failed() {
		return
	}
	req, err := http.NewRequest(http.MethodDelete, "http://"+addr+"/api_admin/removeService?id=1", nil)
	if err != nil {
		t.Errorf("Could not construct request: %v", err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("Http Server may not be serving content: %v", err)
		return
	}
	if resp.StatusCode != 200 {
		t.Errorf("Error remvoing content from removeService endpoint: status code %v", resp.StatusCode)
		return
	}
	// make sure item is gone
	_, err = grpcServer.leaseManager.GetServiceInfo("1")
	if err == nil {
		t.Errorf("GetServiceInfo() should have failed when removing a service that should not exist")
		return
	}
}
