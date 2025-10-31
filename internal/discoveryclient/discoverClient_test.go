package discoveryclient

import (
	"GOMinifyURL/internal/admin"
	proto "GOMinifyURL/internal/proto/admin"
	"context"
	"fmt"
	"log"
	"net"
	"testing"
	"time"
)

// todo add test for verifying discovery client functions correctly (only part left is QueryClientSide)
// todo add test for verifying discovery client wrappers abstract correctly

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

func TestNewDiscoveryClient(t *testing.T) {
	_, err := NewDiscoveryClient("localhost:8083", Service{
		serviceName: "test",
		instanceId:  "1",
		serviceAddr: "just-a-test",
	})
	if err != nil {
		t.Errorf("error creating new discovery client with no options: %v", err)
		return
	}

	_, err = NewDiscoveryClient("localhost:8083", Service{
		serviceName: "test",
		instanceId:  "1",
		serviceAddr: "just-a-test",
	}, WithErrorBuffer(4))
	if err != nil {
		t.Errorf("error creating new discovery client with error buffer size option: %v", err)
		return
	}

	_, err = NewDiscoveryClient("localhost:8083", Service{
		serviceName: "test",
		instanceId:  "1",
		serviceAddr: "just-a-test",
	}, WithTTL(time.Second*30))
	if err != nil {
		t.Errorf("error creating new discovery client with TTL option: %v", err)
		return
	}

}

func TestDiscoveryClient_Register(t *testing.T) {
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Minute*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	client, err := NewDiscoveryClient(addr, Service{
		serviceName: "test",
		instanceId:  "1",
		serviceAddr: "just-a-test",
	})
	if err != nil {
		t.Errorf("error creating new discovery client with no options: %v", err)
		return
	}
	defer client.Close()
	time.Sleep(time.Millisecond * 150)
	err = client.Register()
	if err != nil {
		t.Errorf("error registering new discovery client: %v", err)
		return
	}
}

func TestDiscoveryClient_HeartBeat(t *testing.T) {
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Second*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	client, err := NewDiscoveryClient(addr, Service{
		serviceName: "test",
		instanceId:  "1",
		serviceAddr: "just-a-test",
	})
	if err != nil {
		t.Errorf("error creating new discovery client with no options: %v", err)
		return
	}
	defer client.Close()
	time.Sleep(time.Millisecond * 150)
	err = client.Register()
	if err != nil {
		t.Errorf("error registering new discovery client: %v", err)
		return
	}
	// make sure lease would have expired without proper heartbeat management
	time.Sleep(time.Second * 2)
	// now get service from grpc to see if It's still there
	services, err := grpcAdmin.RequestServiceList(context.Background(), &proto.ServiceListRequest{ServiceName: "test"})
	if err != nil {
		t.Errorf("error requesting service list: %v", err)
		return
	}
	serviceList := services.GetInstances()
	if len(serviceList) != 1 {
		t.Errorf("error expected number of services to be 1, got %v", len(serviceList))
		return
	}
	service := serviceList[0]
	if service.ServiceName != "test" && service.InstanceName != "1" {
		t.Errorf("error expected service to be name 'test' and id to be '1', but got %v", service)
		return
	}
}

func TestDiscoveryClient_HealthStatusChange(t *testing.T) {
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Second*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	client, err := NewDiscoveryClient(addr, Service{
		serviceName: "test",
		instanceId:  "1",
		serviceAddr: "just-a-test",
	})
	if err != nil {
		t.Errorf("error creating new discovery client with no options: %v", err)
		return
	}
	defer client.Close()
	time.Sleep(time.Millisecond * 150)
	err = client.Register()
	if err != nil {
		t.Errorf("error registering new discovery client: %v", err)
		return
	}
	writeChan := client.HealthChan()
	writeChan <- proto.NodeStatus_SICK
	// its guaranteed that this node status will be written out by the second heartbeat this gives the
	// admin server just under 150ish ms to update status (checks for liveness)
	time.Sleep(time.Millisecond * 850)
	serviceResp, err := grpcAdmin.RequestServiceList(context.Background(), &proto.ServiceListRequest{ServiceName: "test"})
	if err != nil {
		t.Errorf("error requesting service list: %v", err)
		return
	}
	serviceList := serviceResp.GetInstances()
	if len(serviceList) != 1 {
		t.Errorf("error expected number of services to be 1, got %v", len(serviceList))
		return
	}
	service := serviceList[0]
	if service.LatestStatus != proto.NodeStatus_SICK {
		t.Errorf("expected node status to be sick but got %v", service.LatestStatus)
		return
	}
}

func TestDiscoveryClient_Error(t *testing.T) {
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Second*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	client, err := NewDiscoveryClient(addr, Service{
		serviceName: "test",
		instanceId:  "1",
		serviceAddr: "just-a-test",
	})
	if err != nil {
		t.Errorf("error creating new discovery client with no options: %v", err)
		return
	}
	defer client.Close()
	time.Sleep(time.Millisecond * 150)
	err = client.Register()
	if err != nil {
		t.Errorf("error registering new discovery client: %v", err)
		return
	}
	errChan := client.Error()
	go func() {
		time.Sleep(time.Second)
		err := client.deRegisterService()
		if err != nil {
			fmt.Printf("failed to de-register service: %v", err)
		}
	}()
	err = <-errChan
	if err == nil {
		t.Errorf("error should have been returned")
		return
	}
	fmt.Printf("received correct error of: %v\n", err)
}

func TestDiscoveryClient_Close(t *testing.T) {
	// this test mainly deReg than close func
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Second*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	client, err := NewDiscoveryClient(addr, Service{
		serviceName: "test",
		instanceId:  "1",
		serviceAddr: "just-a-test",
	})
	if err != nil {
		t.Errorf("error creating new discovery client with no options: %v", err)
		return
	}
	defer client.Close()
	time.Sleep(time.Millisecond * 150)
	err = client.Register()
	if err != nil {
		t.Errorf("error registering new discovery client: %v", err)
		return
	}
	client.Close()
	time.Sleep(time.Millisecond * 10)
	_, err = grpcAdmin.RequestServiceList(context.Background(), &proto.ServiceListRequest{ServiceName: "test"})
	if err == nil {
		t.Errorf("error should have been returned, about service not exists")
		return
	}
	log.Printf("received correct error of: %v\n", err)
}

// todo QueryClient section
// todo check getService works
// todo check getService returns the same thing after reg another service right after the first call
// todo check getServiceWithTTl works correctly by checking that it invalidates entries etc
// todo check GetServiceAndSaveFor does save data for specified time
// todo check that automatic cache eviction also works

func TestNewQueryClient(t *testing.T) {
	_, err := NewQueryClient("localhost:8083")
	if err != nil {
		t.Errorf("error creating new query client: %v", err)
		return
	}
	_, err = NewQueryClient("localhost:8083", WithTTL(time.Second*1))
	if err != nil {
		t.Errorf("error creating new query client: %v", err)
		return
	}
	_, err = NewQueryClient("localhost:8083", WithErrorBuffer(1))
	if err != nil {
		t.Errorf("error creating new query client: %v", err)
		return
	}
}

func TestQueryClient_GetServiceList(t *testing.T) {
	t.Parallel()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Minute*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	QC, err := NewQueryClient(addr, WithTTL(time.Second*15))
	if err != nil {
		t.Errorf("error creating new query client: %v", err)
	}
	defer QC.Close()
	if err != nil {
		t.Errorf("error creating new query client: %v", err)
		return
	}
	<-time.After(time.Millisecond * 150)
	res, err := grpcAdmin.RegisterService(context.Background(), &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	if err != nil {
		t.Errorf("error registering new discovery service item: %v", err)
	}
	listRes, err := QC.GetServiceList("test-service")
	if err != nil {
		t.Errorf("error getting service list: %v", err)
		return
	}
	if len(listRes) != 1 {
		t.Errorf("error expected number of services to be 1, got %v", len(listRes))
	}
	if listRes[0].instanceId != "1" || listRes[0].serviceName != "test-service" {
		t.Errorf("error expected service to be (\"test-service\", \"1\"). got (%v,%v)", listRes[0].serviceName, listRes[0].instanceId)
		return
	}
	service, err := grpcAdmin.DeRegisterService(context.Background(), &proto.DeRegistrationMessage{
		InstanceName: "1",
		Nonce:        res.Nonce,
	})
	if err != nil || !service.GetSuccess() {
		t.Errorf("error removing old service: %v", err)
	}
	listRes, err = QC.GetServiceList("test-service")
	if err != nil {
		t.Errorf("error getting service list: %v", err)
		return
	}
	if len(listRes) != 1 {
		t.Errorf("error expected number of services to be 1, got %v", len(listRes))
		return
	}
	if listRes[0].instanceId != "1" || listRes[0].serviceName != "test-service" {
		t.Errorf("error expected service to be (\"test-service\", \"1\"). got (%v,%v)", listRes[0].serviceName, listRes[0].instanceId)
		return
	}
}

func TestQueryClient_CacheEviction(t *testing.T) {
	t.Parallel()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Minute*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	QC, err := NewQueryClient(addr, WithTTL(time.Second*2))
	if err != nil {
		t.Errorf("error creating new query client: %v", err)
		return
	}
	defer QC.Close()
	<-time.After(time.Millisecond * 150)
	res, err := grpcAdmin.RegisterService(context.Background(), &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	if err != nil {
		t.Errorf("error registering new discovery service item: %v", err)
	}
	_, err = QC.GetServiceList("test-service")
	_, err = grpcAdmin.DeRegisterService(context.Background(), &proto.DeRegistrationMessage{
		InstanceName: "1",
		Nonce:        res.Nonce,
	})
	<-time.After(time.Second * 5)
	if err != nil {
		t.Errorf("error removing old service: %v", err)
		return
	}
	backingInstance := QC.(*DiscoveryClient)
	_, ok := backingInstance.cache.cache["test-service"]
	if ok {
		t.Errorf("backing instance should have been removed from cache")
		return
	}
}

func TestQueryClient_GetServiceListWithTTL(t *testing.T) {
	t.Parallel()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Minute*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	QC, err := NewQueryClient(addr, WithTTL(time.Second*15))
	if err != nil {
		t.Errorf("error creating new query client: %v", err)
		return
	}
	defer QC.Close()
	<-time.After(time.Millisecond * 150)
	res, err := grpcAdmin.RegisterService(context.Background(), &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})

	if err != nil {
		t.Errorf("error registering new discovery service item: %v", err)
	}

	_, err = QC.GetServiceList("test-service")
	if err != nil {
		t.Errorf("error getting service list: %v", err)
		return
	}

	ok, err := grpcAdmin.DeRegisterService(context.Background(), &proto.DeRegistrationMessage{
		InstanceName: "1",
		Nonce:        res.Nonce,
	})
	if err != nil || !ok.GetSuccess() {
		t.Errorf("error removing old service: %v", err)
	}
	// now call with force refresh should get an error
	_, err = QC.GetServiceListWithTTL("test-service", time.Duration(0))
	if err == nil {
		t.Errorf("error should have been returned, about service does not exist")
		return
	}
}

func TestQueryClient_GetServiceAndSaveFor(t *testing.T) {
	t.Parallel()
	addr, err := getFreePort()
	if err != nil {
		t.Errorf("error getting free port: %v", err)
		return
	}
	grpcAdmin, err := admin.NewGrpcAdminServer(addr, admin.WithLeaseTimes(time.Minute*1))
	if err != nil {
		t.Errorf("error creating grpc admin server: %v", err)
		return
	}
	go grpcAdmin.ListenAndServe()
	defer grpcAdmin.Close()
	QC, err := NewQueryClient(addr, WithTTL(time.Second*2))
	if err != nil {
		t.Errorf("error creating new query client: %v", err)
		return
	}
	defer QC.Close()
	<-time.After(time.Millisecond * 150)
	_, err = grpcAdmin.RegisterService(context.Background(), &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "1",
		DialAddr:     "just-a-test",
	})
	if err != nil {
		t.Errorf("error registering new discovery service item: %v", err)
		return
	}
	resList, err := QC.GetServiceListAndSaveFor("test-service", time.Second*4)
	if err != nil {
		t.Errorf("error getting service list: %v", err)
		return
	}
	if len(resList) != 1 {
		t.Errorf("error expected number of services to be 1, got %v", len(resList))
		return
	}
	if resList[0].instanceId != "1" || resList[0].serviceName != "test-service" {
		t.Errorf("error expected service to be (\"test-service\", \"1\"). got (%v,%v)", resList[0].serviceName, resList[0].instanceId)
		return
	}
	startTime := time.Now()
	_, err = grpcAdmin.RegisterService(context.Background(), &proto.RegistrationMessage{
		ServiceName:  "test-service",
		InstanceName: "2",
		DialAddr:     "just-a-test",
	})
	if err != nil {
		t.Errorf("error registering new discovery service item: %v", err)
		return
	}
	<-time.After(max(time.Second*2-time.Since(startTime), 0))
	resList, err = QC.GetServiceList("test-service")
	if err != nil {
		t.Errorf("error getting service list: %v", err)
		return
	}
	if len(resList) != 1 {
		t.Errorf("error expected number of services to be 1, got %v", len(resList))
	}
	<-time.After(5 * time.Second)
	resList, err = QC.GetServiceList("test-service")
	if err != nil {
		t.Errorf("error getting service list: %v", err)
		return
	}
	if len(resList) != 2 {
		t.Errorf("error expected number of services to be 2, got %v", len(resList))
		return
	}
	return
}

// todo RegWrapper
// todo Verify RegWrapper handles name space collision
// todo Verify RegWrapper allows for health update
// todo Verify RegWrapper handles reconnection
// todo Verify RegWrapper handles reconnection correctly (maybe this be a mock just invoke default func)
// todo in general verify callbacks work

// todo QueryClientWrapper
// todo Verify RegWrapper can fetch service data across all methods
// todo verify RegWrapper does rate limit / debounce request
// todo verify bypass works
// todo verify opts work
