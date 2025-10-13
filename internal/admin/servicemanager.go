package admin

import (
	"container/heap"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

type Lease struct {
	mutex     sync.Mutex
	serviceId string
	lease     time.Time
	ttl       time.Duration
	version   uint
	cancel    context.CancelCauseFunc
}

type LeaseManager struct {
	mu                 sync.Mutex
	leases             map[string]*Lease
	leaseHeap          LeaseTimerHeap
	cancelFunc         context.CancelFunc
	serviceListMap     map[string][]*ServiceWithRegInfo
	serviceLookup      map[string]*ServiceWithRegInfo
	contextMap         map[string]context.Context
	rwServiceLookupMap sync.RWMutex
	rwServiceList      sync.RWMutex
	rwContextMap       sync.RWMutex
	rwLeaseMap         sync.RWMutex
	leaseDuration      time.Duration
}

type ServiceWithRegInfo struct {
	serviceName    string
	serviceId      string
	serviceVersion uint
	address        string
	serviceHealth  NodeHealth
	nonce          uint64
	seqNum         uint64
}

type LeaseInfo struct {
	leaseTime     time.Time
	leaseDuration time.Duration
	version       uint
}
type ServiceInstanceSpecificData struct {
	ServiceRegInfo ServiceWithRegInfo
	lease          LeaseInfo
}

func NewLeaseManager(leaseTTL time.Duration) *LeaseManager {
	lm := &LeaseManager{
		leases:    make(map[string]*Lease),
		leaseHeap: make(LeaseTimerHeap, 0),
		mu:        sync.Mutex{},
	}
	heap.Init(&lm.leaseHeap)
	cxt, cancel := context.WithCancel(context.Background())
	lm.cancelFunc = cancel
	lm.leaseDuration = leaseTTL
	lm.serviceListMap = make(map[string][]*ServiceWithRegInfo)
	lm.serviceLookup = make(map[string]*ServiceWithRegInfo)
	lm.contextMap = make(map[string]context.Context)
	lm.rwServiceList = sync.RWMutex{}
	lm.rwServiceLookupMap = sync.RWMutex{}
	lm.rwContextMap = sync.RWMutex{}
	lm.rwLeaseMap = sync.RWMutex{}
	go lm.runLeaseExpirySweeps(cxt, leaseTTL)
	return lm
}

func (lm *LeaseManager) AddService(service *ServiceWithRegInfo) error {
	lm.rwServiceLookupMap.Lock()
	_, ok := lm.serviceLookup[service.serviceId]
	if ok {
		lm.rwServiceLookupMap.Unlock()
		return fmt.Errorf("service %s already exists", service.serviceId)
	}

	copyService := &ServiceWithRegInfo{
		serviceName:    service.serviceName,
		serviceId:      service.serviceId,
		serviceVersion: service.serviceVersion,
		address:        service.address,
		serviceHealth:  service.serviceHealth,
		nonce:          service.nonce,
		seqNum:         service.seqNum,
	}

	lm.serviceLookup[copyService.serviceId] = copyService
	lm.rwServiceLookupMap.Unlock()

	lm.rwServiceList.Lock()
	lm.serviceListMap[copyService.serviceName] = append(lm.serviceListMap[copyService.serviceName], copyService)
	lm.rwServiceList.Unlock()

	cxt, cancel := context.WithCancelCause(context.Background())

	lm.rwContextMap.Lock()
	lm.contextMap[copyService.serviceId] = cxt
	lm.rwContextMap.Unlock()

	lease := &Lease{
		serviceId: copyService.serviceId,
		lease:     time.Now().Add(lm.leaseDuration),
		ttl:       lm.leaseDuration,
		version:   0,
		cancel:    cancel,
		mutex:     sync.Mutex{},
	}
	lm.rwLeaseMap.Lock()
	lm.leases[copyService.serviceId] = lease
	lm.rwLeaseMap.Unlock()

	lm.addLease(lease)
	return nil
}

func (lm *LeaseManager) ExtendLease(serviceId string) error {
	lm.rwLeaseMap.RLock()
	defer lm.rwLeaseMap.RUnlock()
	lease, ok := lm.leases[serviceId]
	if !ok {
		return fmt.Errorf("unable to extend lease, service %s does not exist", serviceId)
	}
	lease.mutex.Lock()
	defer lease.mutex.Unlock()
	lease.lease = time.Now().Add(lm.leaseDuration)
	lease.version++
	go lm.insertNewTimeoutTimer(&LeaseTimer{
		serviceId: serviceId,
		version:   lease.version,
		expiry:    lease.lease,
	})
	return nil
}

// can be invoked by a couple of different ways
// one way is through lease expiry
// another is by grpc all will be routed Through RemoveService
func (lm *LeaseManager) cleanUpService(serviceId string) {

	lm.rwServiceLookupMap.Lock()
	service, ok := lm.serviceLookup[serviceId]
	if !ok {
		lm.rwServiceLookupMap.Unlock()
		return
	}
	delete(lm.serviceLookup, serviceId)
	lm.rwServiceLookupMap.Unlock()

	lm.rwServiceList.Lock()
	if regList, ok := lm.serviceListMap[service.serviceName]; ok {
		lm.serviceListMap[service.serviceName] = slices.DeleteFunc(regList, func(item *ServiceWithRegInfo) bool {
			return item.serviceId == serviceId
		})
		if len(lm.serviceListMap[service.serviceName]) == 0 {
			delete(lm.serviceListMap, service.serviceName)
		}
	}
	lm.rwServiceList.Unlock()

	lm.rwContextMap.Lock()
	delete(lm.contextMap, serviceId)
	lm.rwContextMap.Unlock()

	lm.rwLeaseMap.Lock()
	lease, ok := lm.leases[serviceId]
	if ok {
		lease.mutex.Lock()
		lease.cancel(fmt.Errorf("service was removed [%s]", serviceId))
		lease.mutex.Unlock()
		delete(lm.leases, serviceId)
	}
	lm.rwLeaseMap.Unlock()
}

func (lm *LeaseManager) GetServiceInfo(serviceId string) (ServiceInstanceSpecificData, error) {

	lm.rwServiceLookupMap.RLock()
	service, ok := lm.serviceLookup[serviceId]
	lm.rwServiceLookupMap.RUnlock()
	if !ok {
		return ServiceInstanceSpecificData{}, fmt.Errorf("service [%s] does not exist", serviceId)
	}
	lm.rwLeaseMap.RLock()
	lease, ok := lm.leases[serviceId]
	if !ok || lease == nil {
		lm.rwLeaseMap.RUnlock()
		return ServiceInstanceSpecificData{}, fmt.Errorf("lease for service [%s] does not exist or expired", serviceId)
	}
	lm.rwLeaseMap.RUnlock()
	lease.mutex.Lock()
	defer lease.mutex.Unlock()
	return ServiceInstanceSpecificData{
		ServiceRegInfo: *service,
		lease: LeaseInfo{
			leaseTime:     lease.lease,
			leaseDuration: lease.ttl,
			version:       lease.version,
		},
	}, nil
}

func (lm *LeaseManager) GetServiceList(serviceName string) ([]ServiceWithRegInfo, error) {
	lm.rwServiceList.RLock()
	defer lm.rwServiceList.RUnlock()
	services, ok := lm.serviceListMap[serviceName]
	if !ok {
		return nil, fmt.Errorf("service class [%s] does not exist", serviceName)
	}
	copyServices := make([]ServiceWithRegInfo, 0, len(services))
	for _, service := range services {
		copyServices = append(copyServices, *service)
	}
	return copyServices, nil
}

func (lm *LeaseManager) GetContext(serviceId string) (context.Context, error) {
	lm.rwContextMap.RLock()
	defer lm.rwContextMap.RUnlock()
	cxt, ok := lm.contextMap[serviceId]
	if !ok {
		return nil, fmt.Errorf("service %s does not exist", serviceId)
	}
	return cxt, nil
}

// Close deletes remaining leases and calls their cancel functions. Blocking!!!
func (lm *LeaseManager) Close() {
	lm.cancelFunc()
	lm.rwServiceLookupMap.Lock()
	ids := make([]string, 0, len(lm.serviceLookup))
	for id := range lm.serviceLookup {
		ids = append(ids, id)
	}
	lm.rwServiceLookupMap.Unlock()
	for _, id := range ids {
		lm.cleanUpService(id)
	}
}

func (lm *LeaseManager) addLease(lease *Lease) {
	lease.mutex.Lock()
	serviceId := lease.serviceId
	version := lease.version
	expiry := lease.lease
	lease.mutex.Unlock()
	lm.insertNewTimeoutTimer(&LeaseTimer{
		serviceId: serviceId,
		version:   version,
		expiry:    expiry,
	})
}

func (lm *LeaseManager) RemoveService(serviceId string) {
	lm.cleanUpService(serviceId)
}

func (lm *LeaseManager) RemoveServiceNonBlock(serviceId string) {
	go func() {
		lm.cleanUpService(serviceId)
	}()
}

func (lm *LeaseManager) insertNewTimeoutTimer(leaseTimer *LeaseTimer) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	heap.Push(&lm.leaseHeap, leaseTimer)
}

func (lm *LeaseManager) runLeaseExpirySweeps(cxt context.Context, leaseDuration time.Duration) {
	for {
		lm.mu.Lock()
		if len(lm.leaseHeap) == 0 {
			lm.mu.Unlock()
			select {
			case <-cxt.Done():
				return
				// wait avg time heartbeat responds by
			case <-time.After(leaseDuration / 3):
				continue
			}
		}
		next := lm.leaseHeap[0]
		wait := time.Until(next.expiry)
		if wait > 0 {
			lm.mu.Unlock()
			select {
			case <-cxt.Done():
				return
			case <-time.After(wait):
				continue
			}
		}
		heap.Pop(&lm.leaseHeap)
		lm.mu.Unlock()
		lm.rwLeaseMap.RLock()
		lease, ok := lm.leases[next.serviceId]
		lm.rwLeaseMap.RUnlock()
		// if lease is still valid ie wasn't removed by admin
		if ok {
			lease.mutex.Lock()
			if lease.version == next.version {
				lease.cancel(fmt.Errorf("lease exipred, service being removed %s", next.serviceId))
				lease.mutex.Unlock()
				lm.RemoveServiceNonBlock(next.serviceId)
			} else {
				lease.mutex.Unlock()
			}
		}
	}
}
