package discoveryclient

import (
	proto "GOMinifyURL/internal/proto/admin"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type nodeHealth = proto.NodeStatus

const (
	Healthy  = proto.NodeStatus_HEALTHY
	Degraded = proto.NodeStatus_SICK
	Quiting  = proto.NodeStatus_QUITING
)
const (
	defaultRefillTime = 5 * time.Second
	defaultBurstLimit = 1
)

var (
	ErrStaleData = errors.New("error getting services from cache returned last know state of services")
)

type QueryWrapperOptions func(*QueryWrapper) error

type QueryWrapperRateLimitOption struct {
	burstLimit int
	reFillTime time.Duration
}

func QueryWrapperRateLimitPerService(limitMap map[string]QueryWrapperRateLimitOption) QueryWrapperOptions {
	return func(q *QueryWrapper) error {
		for serviceClass, opt := range limitMap {
			q.rateLimitMap[serviceClass] = rate.NewLimiter(rate.Every(opt.reFillTime), opt.burstLimit)
		}
		return nil
	}
}

func QueryWrapperRateLimitDefaults(defaults QueryWrapperRateLimitOption) QueryWrapperOptions {
	return func(q *QueryWrapper) error {
		q.defaultBurstLimit = defaults.burstLimit
		q.defaultReFillTime = defaults.reFillTime
		return nil
	}
}

type QueryWrapper struct {
	qc                QueryClient
	rwRateLimit       sync.RWMutex
	rateLimitMap      map[string]*rate.Limiter
	rwCache           sync.RWMutex
	subCache          map[string][]Service
	defaultBurstLimit int
	defaultReFillTime time.Duration
}

func NewQueryWrapper(qc QueryClient, opts ...QueryWrapperOptions) (*QueryWrapper, error) {
	qw := &QueryWrapper{
		qc: qc,
	}
	qw.rateLimitMap = make(map[string]*rate.Limiter)
	qw.subCache = make(map[string][]Service)
	qw.defaultBurstLimit = defaultBurstLimit
	qw.defaultReFillTime = defaultRefillTime
	for _, opt := range opts {
		if err := opt(qw); err != nil {
			log.Printf("Error configuring QueryWrapper: %v", err)
			return nil, err
		}
	}
	return qw, nil
}

func (qw *QueryWrapper) getLimiter(serviceClass string) *rate.Limiter {
	qw.rwRateLimit.RLock()
	if limiter, ok := qw.rateLimitMap[serviceClass]; ok {
		qw.rwRateLimit.RUnlock()
		return limiter
	}
	qw.rwRateLimit.RUnlock()
	qw.rwRateLimit.Lock()
	l := rate.NewLimiter(rate.Every(qw.defaultReFillTime), qw.defaultBurstLimit)
	qw.rateLimitMap[serviceClass] = l
	qw.rwRateLimit.Unlock()
	return l
}

func (qw *QueryWrapper) GetServiceList(serviceName string) ([]Service, error) {
	if serviceName == "" {
		return nil, errors.New("empty service name")
	}
	helper := func() ([]Service, error) {
		return qw.qc.GetServiceList(serviceName)
	}
	limiter := qw.getLimiter(serviceName)
	qw.rwCache.RLock()
	cached, hasCache := qw.subCache[serviceName]
	qw.rwCache.RUnlock()
	if limiter.AllowN(time.Now(), 1) || !hasCache {
		return qw.fetchServicesFromCache(serviceName, helper)
	}
	// return cache service
	return cached, nil
}

func (qw *QueryWrapper) GetServiceListWithTTL(serviceName string, ttlRequirement time.Duration) ([]Service, error) {
	if serviceName == "" {
		return nil, errors.New("empty service name")
	}
	helper := func() ([]Service, error) {
		return qw.qc.GetServiceListWithTTL(serviceName, ttlRequirement)
	}
	limiter := qw.getLimiter(serviceName)
	qw.rwCache.RLock()
	cached, hasCache := qw.subCache[serviceName]
	qw.rwCache.RUnlock()
	if limiter.AllowN(time.Now(), 1) || !hasCache {
		return qw.fetchServicesFromCache(serviceName, helper)
	}
	// return cache service
	return cached, nil
}

func (qw *QueryWrapper) GetServiceListAndSaveFor(serviceName string, cacheTime time.Duration) ([]Service, error) {

	if serviceName == "" {
		return nil, errors.New("empty service name")
	}
	helper := func() ([]Service, error) { return qw.qc.GetServiceListAndSaveFor(serviceName, cacheTime) }
	limiter := qw.getLimiter(serviceName)
	qw.rwCache.RLock()
	cached, hasCache := qw.subCache[serviceName]
	qw.rwCache.RUnlock()
	if limiter.AllowN(time.Now(), 1) || !hasCache {
		return qw.fetchServicesFromCache(serviceName, helper)
	}
	// return cache service
	return cached, nil

}

// GetServiceListBypassRateLimit is a useful function in case an urgent refresh needs to occur (detected potential topo change)
// without regard to the rate limit to the current service being requested. Think of host unreachable errors etc.
func (qw *QueryWrapper) GetServiceListBypassRateLimit(serviceName string) ([]Service, error) {
	// call get service with ttl requirement of 0 forcing a refresh of the cache
	// this maybe helpful to speed up cluster convergence on startup or node failure
	return qw.fetchServicesFromCache(serviceName, func() ([]Service, error) { return qw.qc.GetServiceListWithTTL(serviceName, 0) })
}

func (qw *QueryWrapper) fetchServicesFromCache(serviceName string, f func() ([]Service, error)) ([]Service, error) {
	services, err := f()
	if err != nil {
		qw.rwCache.RLock()
		oldServices, ok := qw.subCache[serviceName]
		qw.rwCache.RUnlock()
		if ok {
			return oldServices, ErrStaleData
		}
		return nil, err
	}
	qw.rwCache.Lock()
	qw.subCache[serviceName] = services
	qw.rwCache.Unlock()
	return services, nil
}

func (qw *QueryWrapper) Close() error {
	return qw.qc.Close()
}

type DiscoverRegWrapper struct {
	dc                  *DiscoveryClient
	ConstructionOptions []Option
	onDisconnect        func(*DiscoverRegWrapper, error)
	onLeaseExpired      func(*DiscoverRegWrapper)
	onRemoval           func()
	healthChan          chan nodeHealth
	QW                  QueryWrapper
	originalInstanceId  string
}

func (drw *DiscoverRegWrapper) GetDiscoverClient() *DiscoveryClient {
	return drw.dc
}

func (drw *DiscoverRegWrapper) SetDiscoverClient(dc *DiscoveryClient) {
	drw.dc = dc
}

func defaultOnRemoval() {
	log.Printf("[DiscoverClientWrapper]: Service was removed by admin and will not be registered.")
	return
}

func defaultOnLeaseExpired(drw *DiscoverRegWrapper) {
	serviceItem := drw.dc.service.serviceDisc
	serviceItem.instanceId = drw.originalInstanceId
	dc, err := NewDiscoveryClient(drw.dc.dialAddr, serviceItem, drw.ConstructionOptions...)
	if err != nil {
		log.Printf("[DiscoverClientWrapper][defaultOnLeaseExpired]: Error creating discover client: %v", err)
		return
	}
	drw.SetDiscoverClient(dc)
	drw.Register()
}

func defaultOnDisconnect(drw *DiscoverRegWrapper, err error) {
	if err != nil {
		log.Printf("[DiscoverClientWrapper][defaultOnDisconnect]: Error that caused disconnect: %v", err)
	}
	serviceItem := drw.dc.service.serviceDisc
	serviceItem.instanceId = drw.originalInstanceId
	dc, err := NewDiscoveryClient(drw.dc.dialAddr, serviceItem, drw.ConstructionOptions...)
	if err != nil {
		log.Printf("[DiscoverClientWrapper][defaultOnDisconnect]: Error creating discover client: %v", err)
		return
	}
	drw.SetDiscoverClient(dc)
	drw.Register()
}

func NewDiscoverRegWrapper(dialAddr string, serviceDisc Service, opts ...Option) (*DiscoverRegWrapper, error) {
	dc, err := NewDiscoveryClient(dialAddr, serviceDisc, opts...)
	if err != nil {
		return nil, err
	}
	drw := &DiscoverRegWrapper{dc: dc, QW: QueryWrapper{qc: dc}}
	drw.healthChan = make(chan nodeHealth)
	drw.ConstructionOptions = opts
	drw.onDisconnect = defaultOnDisconnect
	drw.onRemoval = defaultOnRemoval
	drw.onLeaseExpired = defaultOnLeaseExpired
	drw.originalInstanceId = serviceDisc.instanceId
	return drw, nil
}

// HealthChan returns a send only channel to allow for the updating of service status.
// Do not close this channel
func (drw *DiscoverRegWrapper) HealthChan() chan<- nodeHealth {
	return drw.healthChan
}

func (drw *DiscoverRegWrapper) AddOnDisconnectCallBack(f func(*DiscoverRegWrapper, error)) {
	if f == nil {
		return
	}
	drw.onDisconnect = f
}

func (drw *DiscoverRegWrapper) AddOnLeaseExpiredCallBack(f func(*DiscoverRegWrapper)) {
	if f == nil {
		return
	}
	drw.onLeaseExpired = f
}

func (drw *DiscoverRegWrapper) AddOnRemovalCallBack(f func()) {
	if f == nil {
		return
	}
	drw.onRemoval = f
}

// Register will register the service and set up the necessary error handling
// to enable reconnection etc
func (drw *DiscoverRegWrapper) Register() {
	for {
		err := drw.dc.Register()
		if err == nil {
			break
		}
		if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
			err = drw.resolveRegNameConflict()
			if err != nil {
				log.Printf("[DiscoverClientWrapper][Register]: Error resolving service name: %v", err)
				return
			}
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	go func() {
		errChan := drw.dc.Error()
		for {
			select {
			case err, ok := <-errChan:
				if !ok {
					return
				}
				if errors.Is(err, RemovedByAdminErr) {
					go drw.onRemoval()
					return
				} else if drw.onLeaseExpired != nil && errors.Is(err, LeaseExpiryErr) {
					go drw.onLeaseExpired(drw)
					return
				}
				go drw.onDisconnect(drw, err)
				return
			}
		}
	}()
	go func() {
		defer close(drw.healthChan)
		for {
			select {
			case health := <-drw.healthChan:
				drw.dc.healthChan <- health
			case <-drw.dc.cxt.Done():
				return
			}
		}
	}()
}

func (drw *DiscoverRegWrapper) resolveRegNameConflict() error {
	for i := 1; i <= 10; i++ {
		drw.dc.service.serviceDisc.instanceId = fmt.Sprintf("%s-%d", drw.originalInstanceId, i)
		err := drw.dc.Register()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("could not resolve serviceId naming conflict for serviceId %v", drw.originalInstanceId)
}

func (drw *DiscoverRegWrapper) Close() error {
	return drw.dc.Close()
}
