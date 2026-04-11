package mrp

import (
	"net/rpc"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMasterHeartbeatRPCIntegration(t *testing.T) {
	master, err := NewMaster("127.0.0.1:0")
	require.NoError(t, err)

	require.NoError(t, master.Start())
	defer func() {
		require.NoError(t, master.Close())
	}()

	rpcListener, ok := master.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, rpcListener.listener)

	client, err := rpc.Dial("tcp", rpcListener.listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	request := HeartbeatRequest{WorkerID: "worker-1", Message: "alive"}
	var reply HeartbeatReply
	err = client.Call("Master.Heartbeat", request, &reply)
	require.NoError(t, err)

	assert.Equal(t, "ok", reply.Status)
	assert.Equal(t, "heartbeat received from worker-1: alive", reply.Message)
	assert.NotEmpty(t, reply.ServerUTC)
}

func TestMasterStartMapReduceRPCIntegration(t *testing.T) {
	master, err := NewMaster("127.0.0.1:0")
	require.NoError(t, err)

	require.NoError(t, master.Start())
	defer func() {
		require.NoError(t, master.Close())
	}()

	rpcListener, ok := master.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, rpcListener.listener)

	client, err := rpc.Dial("tcp", rpcListener.listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	request := StartMapReduceRequest{Handle: Handle{Id: 42, TimeStamp: 123456789}}
	var reply StartMapReduceReply
	err = client.Call("Master.RPCStartMapReduce", request, &reply)
	require.NoError(t, err)

	// Since DFS is not available in test, expect error status
	// In production with DFS configured, this would return "accepted"
	assert.Equal(t, "error", reply.Status)
	assert.NotEmpty(t, reply.ErrorMessage)
}
