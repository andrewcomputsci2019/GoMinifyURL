package discoveryclient

import (
	"fmt"
	"net"
	"testing"
)

// todo add test for verifying discovery client functions correctly
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

// todo Discovery Client section
// todo test Register function
// todo test heartbeat code
// todo test changing health status
// todo test DeRegister/Close function

func TestNewDiscoveryClient(t *testing.T) {

}

// todo QueryClient section
// todo check getService works
// todo check getService returns the same thing after reg another service right after the first call
// todo check getServiceWithTTl works correctly by checking that it invalidates entries etc
// todo check GetServiceAndSaveFor does save data for specified time
// todo check that automatic cache eviction also works

func TestNewQueryClient(t *testing.T) {

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
