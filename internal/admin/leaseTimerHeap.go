package admin

import (
	"container/heap"
	"time"
)

type LeaseTimer struct {
	expiry    time.Time
	version   uint
	serviceId string
}

type LeaseTimerHeap []*LeaseTimer

func (l *LeaseTimerHeap) Len() int {
	return len(*l)
}

func (l *LeaseTimerHeap) Less(i, j int) bool {
	return (*l)[i].expiry.Before((*l)[j].expiry)
}

func (l *LeaseTimerHeap) Swap(i, j int) {
	(*l)[i], (*l)[j] = (*l)[j], (*l)[i]
}

func (l *LeaseTimerHeap) Push(x any) {
	*l = append(*l, x.(*LeaseTimer))
}

func (l *LeaseTimerHeap) Pop() any {
	old := *l
	n := len(old)
	x := old[n-1]
	*l = old[0 : n-1]
	return x
}

var _ heap.Interface = (*LeaseTimerHeap)(nil)
