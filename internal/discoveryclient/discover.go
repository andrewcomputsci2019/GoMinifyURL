package discoveryclient

import (
	proto "GOMinifyURL/internal/proto/admin"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	TtlLength = 45 * time.Second
)

var (
	RemovedByAdminErr = errors.New("process was removed from cluster by admin. Service should quit or at least not reconnect to the same cluster")
	LeaseExpiryErr    = errors.New("lease expired and service was removed from the cluster")
)

type QueryClient interface {
	GetServiceList(serviceName string) ([]Service, error)
	GetServiceListWithTTL(serviceName string, ttlRequirement time.Duration) ([]Service, error)
	GetServiceListAndSaveFor(serviceName string, cacheTime time.Duration) ([]Service, error)
	Close() error
}

type Service struct {
	serviceName string
	instanceId  string
	serviceAddr string
}

type ServiceWithRegInfo struct {
	serviceDisc Service
	nonce       uint64
}
type serviceCacheItem struct {
	services []Service
	// add fields in here for ttl
	// and extra information about health etc
	expires  time.Time
	inserted time.Time
}
type serviceCache struct {
	rwLock sync.RWMutex
	cache  map[string]*serviceCacheItem
}

type DiscoveryClient struct {
	conn      *grpc.ClientConn
	client    proto.DiscoveryClient
	wg        sync.WaitGroup
	cancel    context.CancelFunc
	cache     *serviceCache
	errorChan chan error
	ttl       time.Duration
	service   *ServiceWithRegInfo
	dialAddr  string
	// for register function
	cxt        context.Context
	healthChan chan proto.NodeStatus
	closeGuard sync.Once
}

type Option func(*DiscoveryClient) error

type option func(*DiscoveryClient) error

func WithSecure(certFile, keyFile, caFile string) Option {
	return func(c *DiscoveryClient) error {
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
		}

		con, err := grpc.NewClient(c.dialAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
		if err != nil {
			return fmt.Errorf("could not create discovery client: %w", err)
		}
		c.conn = con
		c.client = proto.NewDiscoveryClient(con)
		return nil
	}
}

func WithTTL(ttl time.Duration) Option {
	return func(c *DiscoveryClient) error {
		c.ttl = ttl
		return nil
	}
}

func WithErrorBuffer(bufferSize int) Option {
	return func(c *DiscoveryClient) error {
		c.errorChan = make(chan error, bufferSize)
		return nil
	}
}

func NewQueryClient(addr string, opt ...Option) (QueryClient, error) {
	if len(addr) == 0 {
		return nil, errors.New("discovery address is empty")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	cxt, cancel := context.WithCancel(context.Background())
	dClient := &DiscoveryClient{
		conn:      conn,
		client:    proto.NewDiscoveryClient(conn),
		wg:        sync.WaitGroup{},
		cancel:    cancel,
		errorChan: make(chan error, 1),
		cache: &serviceCache{
			rwLock: sync.RWMutex{},
			cache:  make(map[string]*serviceCacheItem),
		},
		ttl:      TtlLength,
		dialAddr: addr,
	}
	for _, o := range opt {
		err := o(dClient)
		if err != nil {
			return nil, err
		}
	}
	dClient.startExpireSweep(cxt)
	return dClient, nil
}

// NewDiscoveryClient do not use in prod, sets up a client to use weak credentials, good for testing
func NewDiscoveryClient(addr string, serviceDisc Service, opts ...Option) (*DiscoveryClient, error) {
	if len(addr) == 0 {
		return nil, errors.New("address is empty")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	cxt, cancel := context.WithCancel(context.Background())
	dClient := &DiscoveryClient{
		conn:      conn,
		client:    proto.NewDiscoveryClient(conn),
		wg:        sync.WaitGroup{},
		cancel:    cancel,
		errorChan: make(chan error, 1),
		cache: &serviceCache{
			rwLock: sync.RWMutex{},
			cache:  make(map[string]*serviceCacheItem),
		},
		ttl:      TtlLength,
		dialAddr: addr,
		service: &ServiceWithRegInfo{
			serviceDisc: Service{
				serviceName: serviceDisc.serviceName,
				instanceId:  serviceDisc.instanceId,
				serviceAddr: serviceDisc.serviceAddr,
			},
		},
	}
	dClient.cxt = cxt
	dClient.healthChan = make(chan proto.NodeStatus, 1)
	for _, opt := range opts {
		err := opt(dClient)
		if err != nil {
			return nil, err
		}
	}
	return dClient, nil
}

func (c *DiscoveryClient) Register() error {
	regMessage, err := c.registerService(c.service.serviceDisc)
	if err != nil {
		return err
	}
	heartbeat := regMessage.RequestTtl.AsDuration() / 3
	c.service.nonce = regMessage.Nonce
	c.startBackGroundTask(c.cxt, c.service.serviceDisc, heartbeat, regMessage.SeqStart)
	return nil
}
func (c *DiscoveryClient) Error() <-chan error {
	return c.errorChan
}

func (c *DiscoveryClient) HealthChan() chan<- proto.NodeStatus {
	return c.healthChan
}

func (c *DiscoveryClient) startBackGroundTask(cxt context.Context, service Service, heartbeat time.Duration, seqNum uint64) {
	c.startHeartBeatTask(cxt, service, heartbeat, seqNum)
	c.startExpireSweep(cxt)

}

func (c *DiscoveryClient) startHeartBeatTask(cxt context.Context, service Service, heartbeat time.Duration, seqNum uint64) {
	c.wg.Add(1)
	go c.heartbeatLoop(service, cxt, seqNum, heartbeat)
}

func (c *DiscoveryClient) startExpireSweep(cxt context.Context) {
	c.wg.Add(1)
	go c.cache.expireSweep(cxt, &c.wg, c.ttl)
}

func (c *DiscoveryClient) Close() error {
	var err error = nil
	c.closeGuard.Do(func() {
		log.Printf("[DiscoveryClient][Close]: closing discovery client")
		c.cancel()
		// ideally we dont use defer here since wait group is added,
		//and we should make sure the connection does close to prevent deadlock
		//defer c.conn.Close()
		defer close(c.errorChan)
		err = c.deRegisterService()
		_ = c.conn.Close()
		c.wg.Wait()
	})
	return err
}

func (c *DiscoveryClient) reportError(err error) {
	select {
	case <-c.cxt.Done():
		return
	case c.errorChan <- err:
		return
	default:
		log.Printf("[DiscoveryClient][reportError]: discovery client failed to report: %v", err)
	}
}

func (c *DiscoveryClient) deRegisterService() error {
	if c.service == nil { // not an error, query only client
		return nil
	}
	deRegMessage := &proto.DeRegistrationMessage{
		InstanceName: c.service.serviceDisc.instanceId,
		Nonce:        c.service.nonce,
	}
	log.Printf("[DiscoveryClient][deRegisterService]: discovery client de-registering service: %v", deRegMessage)
	_, err := c.client.DeRegisterService(context.Background(), deRegMessage)
	if err != nil {
		return err
	}
	return nil
}

func (c *DiscoveryClient) registerService(serviceDisc Service) (*proto.RegistrationResponse, error) {
	protoService := &proto.RegistrationMessage{
		ServiceName:  serviceDisc.serviceName,
		InstanceName: serviceDisc.instanceId,
		DialAddr:     serviceDisc.serviceAddr,
	}
	regMessage, err := c.client.RegisterService(context.Background(), protoService)
	if err != nil {
		return nil, err
	}
	return regMessage, nil
}

func (c *DiscoveryClient) heartbeatLoop(service Service, cxt context.Context, seqNumber uint64, heartbeatTime time.Duration) {
	stream, err := c.client.Heartbeat(cxt)
	defer c.wg.Done()
	// this allows for services that rely on this service to know that heartbeat loop has stopped
	defer c.cancel()
	if err != nil {
		c.reportError(err)
		return
	}
	defer stream.CloseSend()
	stopChan := make(chan struct{})
	nodeStatus := Healthy
	// go func to read messages and log and return errors to error channel
	go func() {
		c.wg.Add(1)
		defer c.wg.Done()
		defer close(stopChan)
		for {
			select {
			case <-cxt.Done():
				return
			default:
				in, err := stream.Recv()
				if err != nil {
					st, ok := status.FromError(err)
					if !ok {
						c.reportError(err)
						return
					}
					for _, detail := range st.Details() {
						switch info := detail.(type) {
						// need to know if stream was removed or just expired
						case *proto.TerminationInfo:
							switch info.Reason {
							case proto.TerminationInfo_LEASE_EXPIRED:
								c.reportError(LeaseExpiryErr)
								return
							case proto.TerminationInfo_REMOVED_BY_ADMIN:
								c.reportError(RemovedByAdminErr)
								return
							default:
								c.reportError(fmt.Errorf("unexpected termination information: %v", info))
								return
							}
						}
					}
					c.reportError(err)
					return
				}
				// heartbeat specific messages Errors should be treated as terminal
				switch fb := in.Feedback.(type) {
				case *proto.HeartBeatResponse_Info:
					log.Printf("Heartbeat info item received: %v", fb.Info)
				case *proto.HeartBeatResponse_Warning:
					log.Printf("Heartbeat warning received: %v", fb.Warning)
				case *proto.HeartBeatResponse_Error:
					log.Printf("Heartbeat err item received: %v", fb.Error)
					c.reportError(fmt.Errorf("heartbeat err item received: %v", fb.Error))
					return
				}
			}
		}
	}()
	ticker := time.NewTicker(heartbeatTime)
	defer ticker.Stop()
	// send heartbeats at allotted intervals
	for {
		select {
		case <-ticker.C:
			hb := &proto.HeartBeat{
				SeqNumber:    seqNumber,
				Status:       nodeStatus,
				InstanceName: service.instanceId,
			}
			seqNumber++
			if err := stream.Send(hb); err != nil {
				c.reportError(err)
				return
			}
		// listen for either client closing or reader closing
		case <-cxt.Done():
			return
		case nodeStatus = <-c.healthChan:
		case <-stopChan:
			return
		}
	}
}

func (c *DiscoveryClient) fetchServiceList(serviceName string) ([]Service, error) {
	msg, err := c.client.RequestServiceList(context.Background(), &proto.ServiceListRequest{
		ServiceName: serviceName,
	})
	if err != nil {
		return nil, err
	}
	serviceList := msg.Instances
	services := make([]Service, len(serviceList))
	for i, inst := range serviceList {
		services[i] = Service{
			serviceName: inst.ServiceName,
			instanceId:  inst.InstanceName,
			serviceAddr: inst.DialAddr,
		}
	}
	return services, nil
}

func (c *DiscoveryClient) fetchServiceListAndAdd(serviceName string, cacheTTL time.Duration) ([]Service, error) {
	fetchedList, err := c.fetchServiceList(serviceName)
	if err != nil {
		return nil, err
	}
	err = c.cache.addEntry(fetchedList, time.Now().Add(cacheTTL))
	if err != nil {
		return nil, err
	}
	return fetchedList, nil
}

// GetServiceListAndSaveFor is identical to GetServiceList except it enforces a custom TTL for the
// specified cache entry. If the service list is not cached, it fetches and stores the data with
// the provided cacheTime as its expiry duration. If the cached entry exists but has expired, the
// method returns the stale data immediately and asynchronously refreshes the cache using the
// new cacheTime value. Does not change the TTL length for already cached items
func (c *DiscoveryClient) GetServiceListAndSaveFor(serviceName string, cacheTime time.Duration) ([]Service, error) {
	res := c.cache.getEntry(serviceName)
	if res == nil {
		add, err := c.fetchServiceListAndAdd(serviceName, cacheTime)
		if err != nil {
			return nil, err
		}
		listCopy := make([]Service, len(add))
		copy(listCopy, add)
		return listCopy, nil
	}
	if time.Now().After(res.expires) {
		go func() {
			_, _ = c.fetchServiceListAndAdd(serviceName, cacheTime)
		}()
	}
	listCopy := make([]Service, len(res.services))
	copy(listCopy, res.services)
	return listCopy, nil
}

// GetServiceListWithTTL returns services with a freshness guarantee of inputted ttlRequirement (ttlRequirement in this context means time it has been inserted for)
// ie if ttlRequirement is 5 seconds the data return must have been in the cache for strictly no longer than 5 seconds.
func (c *DiscoveryClient) GetServiceListWithTTL(serviceName string, ttlRequirement time.Duration) ([]Service, error) {
	res := c.cache.getEntry(serviceName)
	if res == nil || time.Now().After(res.expires) || time.Since(res.inserted) > ttlRequirement {
		// fetch updated list of services
		listAndAdd, err := c.fetchServiceListAndAdd(serviceName, ttlRequirement)
		if err != nil {
			return nil, err
		}
		listCopy := make([]Service, len(listAndAdd))
		copy(listCopy, listAndAdd)
		return listCopy, nil
	}
	listCopy := make([]Service, len(res.services))
	copy(listCopy, res.services)
	return listCopy, nil
}

// GetServiceList returns a list of registered Service instances for the given serviceName.
// It first checks the local cache for an existing entry. If the cache does not contain
// the service list, it fetches fresh data from the discovery service, stores it in the cache,
// and returns a copy of the result.
//
// If a cached entry is found but has expired, the method returns the stale data immediately
// while asynchronously refreshing the cache in the background. All returned slices are copies.
// Ideal usage will would be to debounce/rate-limit calls to this to avoid heavy over head repeated copying
func (c *DiscoveryClient) GetServiceList(serviceName string) ([]Service, error) {
	res := c.cache.getEntry(serviceName)
	if res == nil {
		add, err := c.fetchServiceListAndAdd(serviceName, c.ttl)
		if err != nil {
			return nil, err
		}
		listCopy := make([]Service, len(add))
		copy(listCopy, add)
		return listCopy, nil
	}
	// if data is expired fetch new data return stale data tho
	if time.Now().After(res.expires) {
		go func() {
			// fetch update item
			_, _ = c.fetchServiceListAndAdd(serviceName, c.ttl)
		}()
	}
	copyList := make([]Service, len(res.services))
	copy(copyList, res.services)
	return copyList, nil
}

func (cache *serviceCache) getEntry(key string) *serviceCacheItem {
	cache.rwLock.RLock()
	defer cache.rwLock.RUnlock()
	return cache.cache[key]
}

func (cache *serviceCache) delete(key string) {
	cache.rwLock.Lock()
	defer cache.rwLock.Unlock()
	delete(cache.cache, key)
}

func (cache *serviceCache) addEntry(services []Service, expires time.Time) error {
	if services == nil || len(services) == 0 {
		return fmt.Errorf("empty serviceDisc list")
	}
	// obtain write lock into the cache
	cache.rwLock.Lock()
	defer cache.rwLock.Unlock()
	serviceName := services[0].serviceName
	data, ok := cache.cache[serviceName]
	if !ok {
		// insert for the first time into cache
		data = &serviceCacheItem{
			services: services,
			expires:  expires,
			inserted: time.Now(),
		}
		cache.cache[serviceName] = data
	} else {
		data.expires = expires
		data.services = services
		data.inserted = time.Now()
	}
	return nil
}

func (cache *serviceCache) expireSweep(cxt context.Context, wg *sync.WaitGroup, interval time.Duration) {
	defer wg.Done()
	interval = interval / 2
	if interval < time.Second*2 {
		interval = time.Second * 2
	} else if interval > TtlLength*2 {
		interval = TtlLength * 2
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-cxt.Done():
			return
		case <-ticker.C:
			{
				cache.rwLock.Lock()
				currTime := time.Now()
				for key, service := range cache.cache {
					if currTime.After(service.expires) {
						delete(cache.cache, key)
					}
				}
				cache.rwLock.Unlock()
			}
		}
	}
}
