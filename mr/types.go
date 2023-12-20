package mr

import (
	"sync"
	"time"
)

const (
	PENDING = iota
	STARTED
	DONE
)

const (
	MAP_EVENT    = "map_event"
	REDUCE_EVENT = "reduce_event"
)

type MapTask struct {
	fileName string
	index    int
}

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// The coordinator node gives the tasks to workers.
// It will wait for each worker 10 seconds.
// If any worker could not finish their job on time,
// the coordinator will reassign this job to another worker. So the job will be completed even workers crash etc.
type Coordinator struct {
	mapTaskChan       chan MapTask
	reduceTaskChan    chan int
	mapTaskStatus     map[string]int
	reduceTaskStatus  map[int]int
	inputFiles        []string
	nReduce           int
	intermediateFiles [][]string
	rWMutexLock       *sync.RWMutex
	mapFinished       bool
	reduceFinished    bool
	deadlineForReply  time.Duration
}

type Worker struct {
	mapf    func(string, string) []KeyValue
	reducef func(string, []string) string
}
