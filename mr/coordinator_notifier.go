package mr

func (coord *Coordinator) NotifyReduceSuccess(args *NotifyReduceSuccessArgs, reply *NotifyReply) error {
	coord.rWMutexLock.Lock()
	defer coord.rWMutexLock.Unlock()
	coord.reduceTaskStatus[args.ReduceIndex] = DONE

	finished := true
	for _, v := range coord.reduceTaskStatus {
		if v != DONE {
			finished = false
			break
		}
	}
	coord.reduceFinished = finished
	return nil
}

func (coord *Coordinator) NotifyMapSuccess(args *NotifyMapSuccessArgs, reply *NotifyReply) error {
	coord.rWMutexLock.Lock()
	defer coord.rWMutexLock.Unlock()
	coord.mapTaskStatus[args.File] = DONE
	finished := true
	for _, v := range coord.mapTaskStatus {
		if v != DONE {
			finished = false
			break
		}
	}
	coord.mapFinished = finished
	if coord.mapFinished {
		for i := 0; i < coord.nReduce; i++ {
			coord.reduceTaskStatus[i] = PENDING
			coord.reduceTaskChan <- i
		}
	}
	return nil
}
