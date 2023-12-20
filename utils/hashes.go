package utils

import "hash/fnv"

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func Ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}
