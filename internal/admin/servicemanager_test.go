package admin

import (
	"fmt"
	"log"
	"testing"
	"time"
)

// make sure can construct lease manager
func TestNewLeaseManager(t *testing.T) {
	_ = NewLeaseManager(time.Second * 30)
}

func TestLeaseManager_AddService(t *testing.T) {
	lm := NewLeaseManager(time.Second * 30)
	err := lm.AddService(&ServiceWithRegInfo{
		serviceName:    "test-service",
		serviceId:      "1",
		serviceVersion: 1,
		address:        "",
		serviceHealth:  Healthy,
		nonce:          0,
		seqNum:         0,
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
		return
	}

	err = lm.AddService(&ServiceWithRegInfo{
		serviceName:    "test-service",
		serviceId:      "1",
		serviceVersion: 1,
		address:        "",
		serviceHealth:  Healthy,
		nonce:          0,
		seqNum:         0,
	})
	if err == nil {
		t.Error(fmt.Errorf("should be an error adding duplicate service but error was nil"))
		return
	}
}

func TestLeaseManager_RemoveService(t *testing.T) {
	lm := NewLeaseManager(time.Second * 30)
	err := lm.AddService(&ServiceWithRegInfo{
		serviceName: "test-service",
		serviceId:   "1",
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
		return
	}
	lm.RemoveService("1")
	services := lm.GetAllServices()
	if len(services) != 0 {
		t.Error(fmt.Errorf("expected 0 services but got %v", len(services)))
		return
	}
}

func TestLeaseManager_GetServiceInfo(t *testing.T) {
	lm := NewLeaseManager(time.Second * 30)
	err := lm.AddService(&ServiceWithRegInfo{
		serviceName:    "test-service",
		serviceId:      "1",
		serviceVersion: 1,
		address:        "",
		serviceHealth:  Healthy,
		nonce:          0,
		seqNum:         0,
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
		return
	}
	data, err := lm.GetServiceInfo("1")
	if err != nil {
		t.Error(fmt.Errorf("GetServiceInfo failed: %v", err))
		return
	}
	if data.ServiceRegInfo.serviceId != "1" || data.ServiceRegInfo.serviceVersion != 1 || data.ServiceRegInfo.serviceName != "test-service" {
		t.Error(fmt.Errorf("data loss inside of LeaseManager: %v", err))
		return
	}
}

func TestLeaseManager_GetAllServices(t *testing.T) {
	lm := NewLeaseManager(time.Second * 30)
	err := lm.AddService(&ServiceWithRegInfo{
		serviceName: "test-service",
		serviceId:   "1",
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
		return
	}
	err = lm.AddService(&ServiceWithRegInfo{
		serviceName: "test-service",
		serviceId:   "2",
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
	}

	services := lm.GetAllServices()
	if len(services) != 2 {
		t.Error(fmt.Errorf("expected 2 services but got %v", len(services)))
	}
	servicesMap := make(map[string]bool)
	servicesMap["1"] = false
	servicesMap["2"] = false
	for _, service := range services {
		servicesMap[service.serviceId] = true
	}
	if !servicesMap["1"] || !servicesMap["2"] {
		t.Error(fmt.Errorf("expected 1 or 2 services id but got %v", services))
		return
	}
}

func TestLeaseManager_LeaseAutoRemoval(t *testing.T) {
	lm := NewLeaseManager(time.Second * 10)
	err := lm.AddService(&ServiceWithRegInfo{serviceName: "test-service",
		serviceId: "1"})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
	}
	go func() {
		<-time.After(time.Second * 5)
		err := lm.AddService(&ServiceWithRegInfo{serviceName: "test-service",
			serviceId: "2"})
		if err != nil {
			log.Println(fmt.Errorf("AddService failed in go routine: %v", err))
			return
		}
	}()
	info, err := lm.GetServiceInfo("1")
	if err != nil {
		t.Error(fmt.Errorf("GetServiceInfo failed when fetching id 1: %v", err))
	}
	leaseInfo := info.lease.leaseTime
	// a bit of a buffer but something realistic (want service to be removed before we grab the read lock)
	<-time.After(time.Until(leaseInfo) + time.Millisecond*10)
	_, err = lm.GetServiceInfo("1")
	if err == nil {
		t.Error(fmt.Errorf("service with id 1 was suppose to be removed yet was still found"))
		return
	}
	services := lm.GetAllServices()
	if len(services) != 1 {
		t.Error(fmt.Errorf("expected 1 service to still be inside lease manager but got: %v", len(services)))
		return
	}
}

func TestLeaseManager_GetContext(t *testing.T) {
	lm := NewLeaseManager(time.Second * 30)
	err := lm.AddService(&ServiceWithRegInfo{
		serviceName: "test-service",
		serviceId:   "1",
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
		return
	}
	cxt, err := lm.GetContext("1")
	if err != nil {
		t.Error(fmt.Errorf("GetContext failed to fetch context of service of id 1: %v", err))
	}
	go func() {
		<-time.After(time.Second * 2)
		lm.RemoveServiceNonBlock("1")
	}()
	select {
	case <-cxt.Done():
		break
	case <-time.After(time.Second * 5):
		t.Error(fmt.Errorf("context was not cancled within aloted time"))
	}
	return
}

func TestLeaseManager_GetServiceList(t *testing.T) {
	lm := NewLeaseManager(time.Second * 30)
	err := lm.AddService(&ServiceWithRegInfo{
		serviceName: "test-service",
		serviceId:   "1",
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
		return
	}
	err = lm.AddService(&ServiceWithRegInfo{serviceName: "test-service",
		serviceId: "2"})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
		return
	}
	err = lm.AddService(&ServiceWithRegInfo{serviceName: "not-test-service",
		serviceId: "3"})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
		return
	}

	serviceList, err := lm.GetServiceList("test-service")
	if err != nil {
		t.Error(fmt.Errorf("GetServiceList failed when fetching services under service-name 'test-service': %v", err))
		return
	}
	if len(serviceList) != 2 {
		t.Error(fmt.Errorf("expected 2 services but got %v", len(serviceList)))
		return
	}
	serviceListMap := map[string]bool{"1": false, "2": false}
	for _, service := range serviceList {
		serviceListMap[service.serviceId] = true
	}
	if !serviceListMap["1"] || !serviceListMap["2"] {
		t.Error(fmt.Errorf("expected services with id 1 and 2 to be present but got: %v", serviceListMap))
		return
	}
}

func TestLeaseManager_ExtendLease(t *testing.T) {
	lm := NewLeaseManager(time.Second * 10)
	err := lm.AddService(&ServiceWithRegInfo{
		serviceName: "test-service",
		serviceId:   "1",
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
	}
	sInfo, err := lm.GetServiceInfo("1")
	if err != nil {
		t.Error(fmt.Errorf("GetServiceInfo failed when fetching id 1: %v", err))
	}
	leaseInfo := sInfo.lease.leaseTime
	go func() {
		<-time.After(time.Second * 5)
		err := lm.ExtendLease("1")
		if err != nil {
			t.Error(fmt.Errorf("ExtendLease failed: %v", err))
			return
		}
	}()
	<-time.After(time.Until(leaseInfo))
	_, err = lm.GetServiceInfo("1")
	if err != nil {
		t.Error(fmt.Errorf("service with id 1 was not supposed to be removed yet, lease failed to be extended?: %v", err))
	}
	return
}

func TestLeaseManager_Close(t *testing.T) {
	lm := NewLeaseManager(time.Second * 30)
	err := lm.AddService(&ServiceWithRegInfo{
		serviceName: "test-service",
		serviceId:   "1",
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
	}
	err = lm.AddService(&ServiceWithRegInfo{
		serviceName: "test-service",
		serviceId:   "2",
	})
	if err != nil {
		t.Error(fmt.Errorf("AddService failed: %v", err))
	}
	lm.Close()
	if len(lm.GetAllServices()) != 0 {
		t.Error(fmt.Errorf("expected 0 services but got %v", len(lm.GetAllServices())))
	}
	return
}
