package mr

func (coord *Coordinator) NotifyIntermediateFile(args *NotifyIntermediateArgs, reply *NotifyReply) error {
	coord.rWMutexLock.Lock()
	defer coord.rWMutexLock.Unlock()
	coord.intermediateFiles[args.ReduceIndex] = append(coord.intermediateFiles[args.ReduceIndex], args.File)
	return nil
}
