package mr

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/rpc"
	"os"
	"sort"
	"strconv"

	"github.com/caleberi/map_reduce_rpc/utils"
)

func NewWorker(mapf func(string, string) []KeyValue, reducef func(string, []string) string) *Worker {
	return &Worker{
		mapf:    mapf,
		reducef: reducef,
	}
}

func (w *Worker) Do() {
	for {
		reply := CallCoordinator()
		switch reply.TaskType {
		case MAP_EVENT:
			executeMap(w.mapf, reply)
			continue
		case REDUCE_EVENT:
			executeReduce(w.reducef, reply)
			continue
		default:
			return
		}
	}
}

func executeReduce(reducefn func(string, []string) string, reply Reply) {
	intermediate := []KeyValue{}

	for _, v := range reply.Files {
		file, err := os.Open(v)
		if err != nil {
			log.Fatalf("cannot open %v", v)
		}
		dec := json.NewDecoder(file)
		for {
			var kv KeyValue
			if err := dec.Decode(&kv); err != nil {
				break
			}
			intermediate = append(intermediate, kv)
		}
		file.Close()
	}

	sort.Sort(ByKey(intermediate))

	oname := "mr-out-" + strconv.Itoa(reply.Index)
	ofile, _ := os.Create(oname)

	i := 0
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{} // group them together
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducefn(intermediate[i].Key, values)
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)
		i = j
	}

	NotifyReduceSuccess(reply.Index)
}

func executeMap(mapfn func(string, string) []KeyValue, reply Reply) {
	filename := reply.MapFileName

	file, err := os.Open(filename)

	if err != nil {
		log.Fatalf("cannot open %v", filename)
	}

	content, err := io.ReadAll(file)

	if err != nil {
		log.Fatalf("cannot read %v", filename)
	}

	// release the file
	file.Close()

	rawKVArray := mapfn(filename, string(content))
	sortedKVArray := arrangeImmediate(rawKVArray, reply.NReduce)

	files := []string{}

	for i := range sortedKVArray {
		values := sortedKVArray[i]
		filename := "mr-" + strconv.Itoa(reply.Index) + "-" + strconv.Itoa(i)
		ofile, _ := os.Create(filename)

		enc := json.NewEncoder(ofile) // intermediate json file
		for _, kv := range values {
			err := enc.Encode(&kv)
			if err != nil {
				log.Fatal("error : ", err)
			}
		}

		files = append(files, filename)
		NotifyCoordinator(i, filename)
		ofile.Close()
	}

	_ = files
	NotifyMapSuccess(filename)
}

func arrangeImmediate(kvs []KeyValue, nReduce int) [][]KeyValue {
	kvap := make([][]KeyValue, nReduce)
	for _, kv := range kvs {
		v := utils.Ihash(kv.Key) % nReduce
		kvap[v] = append(kvap[v], kv)
	}
	return kvap
}

/// RPC methods

func NotifyMapSuccess(filename string) {
	args := NotifyMapSuccessArgs{}
	args.File = filename
	reply := NotifyReply{}
	call("Coordinator.NotifyMapSuccess", &args, &reply)
}

func NotifyReduceSuccess(reduceIndex int) {
	args := NotifyReduceSuccessArgs{}
	args.ReduceIndex = reduceIndex
	reply := NotifyReply{}
	call("Coordinator.NotifyReduceSuccess", &args, &reply)
}

func NotifyCoordinator(reduceIndex int, file string) {
	args := NotifyIntermediateArgs{}
	args.ReduceIndex = reduceIndex
	args.File = file
	reply := NotifyReply{}
	call("Coordinator.NotifyIntermediateFile", &args, &reply)
}

func CallCoordinator() Reply {
	args := MrArgs{}
	reply := Reply{}
	call("Coordinator.DistributeTask", &args, &reply)
	return reply
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	log.Printf("dialed %s successfully", sockname)
	defer c.Close()

	log.Printf("calling coordinator.%s", rpcname)
	err = c.Call(rpcname, args, reply)
	return err != nil
}
