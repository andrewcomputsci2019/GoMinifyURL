package admin

import (
	"container/heap"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLeaseTimerHeap_Less(t *testing.T) {
	now := time.Now()
	h := &LeaseTimerHeap{
		&LeaseTimer{expiry: now.Add(30 * time.Second)},
		&LeaseTimer{expiry: now.Add(10 * time.Second)},
	}

	require.False(t, h.Less(0, 1), "expected index 0 to be later (larger expiry)")
	require.True(t, h.Less(1, 0), "expected index 1 to be earlier (smaller expiry)")
}

func TestLeaseTimerHeap_Push(t *testing.T) {
	leaseTimerHeap := &LeaseTimerHeap{}
	heap.Init(leaseTimerHeap)
	itemA := &LeaseTimer{expiry: time.Now().Add(10 * time.Second), serviceId: "1"}
	heap.Push(leaseTimerHeap, itemA)
	itemB := &LeaseTimer{expiry: time.Now().Add(15 * time.Second), serviceId: "2"}
	heap.Push(leaseTimerHeap, itemB)
	itemC := &LeaseTimer{expiry: time.Now().Add(20 * time.Second), serviceId: "3"}
	heap.Push(leaseTimerHeap, itemC)

	// check presence of each serviceId (unordered)
	wantIDs := map[string]bool{"1": false, "2": false, "3": false}

	for _, lt := range *leaseTimerHeap {
		if _, ok := wantIDs[lt.serviceId]; ok {
			wantIDs[lt.serviceId] = true
		} else {
			t.Fatalf("unexpected serviceId found in heap: %v", lt.serviceId)
		}
	}

	for id, seen := range wantIDs {
		if !seen {
			t.Fatalf("missing serviceId %v in heap", id)
		}
	}

}

func TestLeaseTimerHeap_Pop(t *testing.T) {
	leaseTimerHeap := &LeaseTimerHeap{}
	heap.Init(leaseTimerHeap)
	itemA := &LeaseTimer{expiry: time.Now().Add(10 * time.Second), serviceId: "1"}
	heap.Push(leaseTimerHeap, itemA)
	itemB := &LeaseTimer{expiry: time.Now().Add(15 * time.Second), serviceId: "2"}
	heap.Push(leaseTimerHeap, itemB)
	itemC := &LeaseTimer{expiry: time.Now().Add(20 * time.Second), serviceId: "3"}
	heap.Push(leaseTimerHeap, itemC)

	correctOrder := []string{"1", "2", "3"}
	for i, wantID := range correctOrder {
		got := heap.Pop(leaseTimerHeap).(*LeaseTimer)
		require.Equalf(t, wantID, got.serviceId, "unexpected pop order at index %d", i)
	}
	require.Equal(t, 0, leaseTimerHeap.Len())

}

func TestLeaseTimerHeap_Peek(t *testing.T) {
	leaseTimerHeap := &LeaseTimerHeap{}
	heap.Init(leaseTimerHeap)
	itemA := &LeaseTimer{expiry: time.Now().Add(10 * time.Second), serviceId: "1"}
	heap.Push(leaseTimerHeap, itemA)
	itemB := &LeaseTimer{expiry: time.Now().Add(15 * time.Second), serviceId: "2"}
	heap.Push(leaseTimerHeap, itemB)
	itemC := &LeaseTimer{expiry: time.Now().Add(20 * time.Second), serviceId: "3"}
	heap.Push(leaseTimerHeap, itemC)
	item0 := (*leaseTimerHeap)[0]
	require.Equal(t, itemA.serviceId, item0.serviceId)
}

func TestLeaseTimerHeap_Len(t *testing.T) {
	h := &LeaseTimerHeap{}
	heap.Init(h)

	heap.Push(h, &LeaseTimer{expiry: time.Now().Add(10 * time.Second)})
	require.Equal(t, 1, h.Len(), "expected len=1 after first push")

	heap.Push(h, &LeaseTimer{expiry: time.Now().Add(12 * time.Second)})
	require.Equal(t, 2, h.Len(), "expected len=2 after second push")

	heap.Pop(h)
	require.Equal(t, 1, h.Len(), "expected len=1 after one pop")
}
