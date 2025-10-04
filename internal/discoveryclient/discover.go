package discoveryclient

import (
	"GOMinifyURL/internal/proto"
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
)

const (
	TtlLength = 2 * time.Minute
)

type Service struct {
	serviceName string
	instanceId  string
	serviceAddr string
}

type ServiceWithRegInfo struct {
	serviceDisc Service
	nonce       int64
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
}

type option func(*DiscoveryClient) error

func withSecure(certFile, keyFile, caFile string) option {
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

func withTTL(ttl time.Duration) option {
	return func(c *DiscoveryClient) error {
		c.ttl = ttl
		return nil
	}
}

func withErrorBuffer(bufferSize int) option {
	return func(c *DiscoveryClient) error {
		c.errorChan = make(chan error, bufferSize)
		return nil
	}
}

func NewQueryClient(addr string, opt ...option) (*DiscoveryClient, error) {
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
func NewDiscoveryClient(addr string, serviceDisc Service, opts ...option) (*DiscoveryClient, error) {
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

	for _, opt := range opts {
		err := opt(dClient)
		if err != nil {
			return nil, err
		}
	}

	regMessage, err := dClient.registerService(serviceDisc)
	if err != nil {
		return nil, err
	}
	heartbeat := regMessage.RequestTtl / 3
	dClient.service.nonce = regMessage.Nonce
	dClient.startBackGroundTask(cxt, serviceDisc, int(heartbeat), regMessage.SeqStart)
	return dClient, nil
}

func (c *DiscoveryClient) startBackGroundTask(cxt context.Context, service Service, heartbeat int, seqNum int64) {
	c.startHeartBeatTask(cxt, service, heartbeat, seqNum)
	c.startExpireSweep(cxt)

}

func (c *DiscoveryClient) startHeartBeatTask(cxt context.Context, service Service, heartbeat int, seqNum int64) {
	c.wg.Add(1)
	go c.heartbeatLoop(service, cxt, seqNum, heartbeat)
}

func (c *DiscoveryClient) startExpireSweep(cxt context.Context) {
	c.wg.Add(1)
	go c.cache.expireSweep(cxt, &c.wg)
}

func (c *DiscoveryClient) Close() error {
	c.cancel()
	close(c.errorChan)
	if err := c.deRegisterService(); err != nil {
		return err
	}
	c.wg.Wait()
	return c.conn.Close()
}

func (c *DiscoveryClient) reportError(err error) {
	select {
	case c.errorChan <- err:
		return
	default:
		log.Printf("discovery client failed to report: %v", err)
	}
}

func (c *DiscoveryClient) deRegisterService() error {
	if c.service == nil { // not an error, query only client
		return nil
	}
	deRegMessage := &proto.DeRegistrationMessage{
		InstanceName: c.service.serviceDisc.serviceName,
		Nonce:        c.service.nonce,
	}
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

func (c *DiscoveryClient) heartbeatLoop(service Service, cxt context.Context, seqNumber int64, heartbeatTime int) {
	// this code should run in its own go func
	// is responsible for making sure clients receive
	stream, err := c.client.Heartbeat(cxt)
	defer c.wg.Done()
	if err != nil {
		c.reportError(err)
		return
	}
	defer stream.CloseSend()
	stopChan := make(chan struct{})
	go func() {
		defer close(stopChan)
		for {
			select {
			case <-cxt.Done():
				return
			default:
				in, err := stream.Recv()
				if err != nil {
					c.reportError(err)
					return
				}
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
	// right now assume heartbeats occur on 30 second intervals
	ticker := time.NewTicker(time.Duration(heartbeatTime) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			hb := &proto.HeartBeat{
				SeqNumber:    seqNumber,
				Status:       proto.NodeStatus_HEALTHY,
				InstanceName: service.instanceId,
			}
			seqNumber++
			if err := stream.Send(hb); err != nil {
				c.reportError(err)
				return
			}
		case <-cxt.Done():
			return
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

func (c *DiscoveryClient) getServiceListAndSaveFor(serviceName string, cacheTime time.Duration) ([]Service, error) {
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

func (c *DiscoveryClient) getServiceListWithTTL(serviceName string, ttlRequirement time.Duration) ([]Service, error) {
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

func (c *DiscoveryClient) getServiceList(serviceName string) ([]Service, error) {
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

func (cache *serviceCache) expireSweep(cxt context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(2 * time.Minute)
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
