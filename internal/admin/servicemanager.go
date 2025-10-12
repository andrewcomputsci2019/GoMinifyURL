package admin

import (
	"container/heap"
	"context"
	"fmt"
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

// todo implement way for admin service to register services to the lease manager
// todo implement guaranteed cleanup of services on deregister or expiry
// todo implement way for grpc server to get list of services for a service type
// todo implement way for service to be deregistered likely going to be tied to #2 todo
// todo implement way for http server to get more information about a single service instance

type LeaseManager struct {
	mu                 sync.Mutex
	leases             map[string]*Lease
	leaseHeap          LeaseTimerHeap
	cancelFunc         context.CancelFunc
	serviceListMap     map[string][]*serviceWithRegInfo
	serviceLookup      map[string]*serviceWithRegInfo
	contextMap         map[string]context.Context
	rwServiceList      sync.RWMutex
	rwServiceLookupMap sync.RWMutex
	rwContextMap       sync.RWMutex
	leaseDuration      time.Duration
}

type serviceWithRegInfo struct {
	serviceName   string
	serviceId     string
	serviceHealth NodeHealth
	nonce         uint64
	seqNum        uint64
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
	go lm.runLeaseExpirySweeps(cxt, leaseTTL)
	return lm
}

// Close deletes remaining leases and calls their cancel functions. Blocking!!!
func (lm *LeaseManager) Close() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	for _, l := range lm.leases {
		l.cancel(fmt.Errorf("lease manager closed"))
		delete(lm.leases, l.serviceId)
	}
	lm.cancelFunc()
}

func (lm *LeaseManager) removeLeaseWithLock(serviceId string, haveLock bool) {
	if !haveLock {
		lm.mu.Lock()
		defer lm.mu.Unlock()

	}
	lease, ok := lm.leases[serviceId]
	if !ok {
		return
	}
	lease.mutex.Lock()
	defer lease.mutex.Unlock()
	delete(lm.leases, serviceId)
	if haveLock {
		lease.cancel(fmt.Errorf("lease manager: detected a lapse of lease renewal: %s", serviceId))
	} else {
		lease.cancel(fmt.Errorf("lease removal invoke from external caller: %s", serviceId))
	}

}

func (lm *LeaseManager) addLease(lease *Lease) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.leases[lease.serviceId] = lease
	heap.Push(&lm.leaseHeap, &LeaseTimer{
		serviceId: lease.serviceId,
		version:   lease.version,
		expiry:    lease.lease,
	})
}

func (lm *LeaseManager) RemoveLease(serviceId string) {
	lm.removeLeaseWithLock(serviceId, false)
}

func (lm *LeaseManager) RemoveLeaseNonBlock(serviceId string) {
	go func() {
		lm.removeLeaseWithLock(serviceId, false)
	}()
}

func (lm *LeaseManager) runLeaseExpirySweeps(cxt context.Context, leaseDuration time.Duration) {
	for {
		lm.mu.Lock()
		if len(lm.leases) == 0 {
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
		if next.expiry.After(time.Now()) {
			wait := next.expiry.Sub(time.Now())
			lm.mu.Unlock()
			select {
			case <-cxt.Done():
				return
			case <-time.After(wait):
				continue
			}
		}
		// todo is not guaranteed that service will still exist in datastructures
		heap.Pop(&lm.leaseHeap)
		lease := lm.leases[next.serviceId]
		lease.mutex.Lock()
		if lease.version == next.version {
			lease.mutex.Unlock()
			lm.removeLeaseWithLock(lease.serviceId, true)
		} else {
			lease.mutex.Unlock()
		}
		lm.mu.Unlock()
	}
}
