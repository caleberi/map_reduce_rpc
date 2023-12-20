package mr

import (
	"os"
	"strconv"
)

type MrArgs struct {
}

type Reply struct {
	MapFileName string
	TaskType    string
	Index       int
	NReduce     int
	Files       []string
}

type NotifyIntermediateArgs struct {
	ReduceIndex int
	File        string
}

type NotifyMapSuccessArgs struct {
	File string
}

type NotifyReduceSuccessArgs struct {
	ReduceIndex int
}

type NotifyReply struct {
}

// Add your RPC definitions here.

// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the coordinator.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func coordinatorSock() string {
	s := "/var/tmp/5840-mr-"
	s += strconv.Itoa(os.Getuid())
	println("socket name : " + s)
	return s
}
