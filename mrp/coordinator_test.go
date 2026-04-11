package mrp

import (
	"net/rpc"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoordinatorPingRPCIntegration(t *testing.T) {
	coord, err := NewCoordinator("127.0.0.1:0", "")
	require.NoError(t, err)

	require.NoError(t, coord.Start())
	defer func() {
		require.NoError(t, coord.Close())
	}()

	rpcListener, ok := coord.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, rpcListener.listener)

	client, err := rpc.Dial("tcp", rpcListener.listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	var reply string
	err = client.Call("Coordinator.RPCPing", "hello", &reply)
	require.NoError(t, err)
	assert.Equal(t, "pong: hello", reply)
}

func TestCoordinatorForwardDownloadFlow(t *testing.T) {
	coord, err := NewCoordinator("127.0.0.1:0", "")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, coord.Close())
	}()

	var handleReply HandleReply
	err = coord.RPCGenerateDownloadHandle(HandleRequest{}, &handleReply)
	require.NoError(t, err)
	require.Equal(t, "success", handleReply.Status)
	require.NotZero(t, handleReply.Handle.Id)

	var downloadReply DownloadReply
	err = coord.RPCForwardDownload(DownloadRequest{
		Handle: handleReply.Handle,
		Data:   []byte("chunk-data"),
		Eof:    true,
	}, &downloadReply)
	require.NoError(t, err)
	assert.Equal(t, "success", downloadReply.Status)
}

func TestCoordinatorForwardDownloadRPCIntegration(t *testing.T) {
	coord, err := NewCoordinator("127.0.0.1:0", "")
	require.NoError(t, err)

	require.NoError(t, coord.Start())
	defer func() {
		require.NoError(t, coord.Close())
	}()

	rpcListener, ok := coord.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, rpcListener.listener)

	client, err := rpc.Dial("tcp", rpcListener.listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	var handleReply HandleReply
	err = client.Call("Coordinator.RPCGenerateDownloadHandle", struct{}{}, &handleReply)
	require.NoError(t, err)
	require.Equal(t, "success", handleReply.Status)
	require.NotZero(t, handleReply.Handle.Id)

	request := DownloadRequest{
		Handle: handleReply.Handle,
		Data:   []byte("rpc-chunk"),
		Eof:    true,
	}
	var downloadReply DownloadReply
	err = client.Call("Coordinator.RPCForwardDownload", request, &downloadReply)
	require.NoError(t, err)
	assert.Equal(t, "success", downloadReply.Status)
	assert.Equal(t, 0, downloadReply.ErrorCode)
	assert.Equal(t, "", downloadReply.ErrorMessage)
}
