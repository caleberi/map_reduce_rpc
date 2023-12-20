package mr

import "time"

func (coord *Coordinator) watchMapWorker(task MapTask) {
	ticker := time.NewTicker(coord.deadlineForReply)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			coord.rWMutexLock.Lock()
			coord.mapTaskStatus[task.fileName] = PENDING
			coord.rWMutexLock.Unlock()
			// push back into channel to ensure retry
			coord.mapTaskChan <- task
		default:
			// lock for reading for syncronization
			if func() bool {
				coord.rWMutexLock.RLock()
				defer coord.rWMutexLock.RUnlock()
				return coord.mapTaskStatus[task.fileName] == DONE
			}() {
				return
			}

		}
	}
}

func (coord *Coordinator) watchWorkerReduce(reduceNumber int) {
	ticker := time.NewTicker(coord.deadlineForReply)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			coord.rWMutexLock.Lock()
			coord.reduceTaskStatus[reduceNumber] = PENDING // reset on dead line
			coord.rWMutexLock.Unlock()
			// push back into channel to ensure retry
			coord.reduceTaskChan <- reduceNumber
		default:
			// lock for reading for syncronization
			if func() bool {
				coord.rWMutexLock.RLock()
				defer coord.rWMutexLock.RUnlock()
				return coord.reduceTaskStatus[reduceNumber] == DONE
			}() {
				return
			}

		}
	}
}
