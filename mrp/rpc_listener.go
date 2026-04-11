package mrp

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

type Listener interface {
	Listen() error
	Close() error
	Errors() <-chan error
}

type RPCListener struct {
	address string

	errChan chan error

	listener  net.Listener
	waitGroup sync.WaitGroup
	closed    atomic.Bool

	acceptHandler func(conn net.Conn)
}

func NewRPCListener(address string, acceptHandler func(conn net.Conn)) *RPCListener {
	return &RPCListener{
		address:       address,
		acceptHandler: acceptHandler,
	}
}

func (r *RPCListener) reportErr(err error) {
	if err == nil || r.errChan == nil {
		return
	}

	select {
	case r.errChan <- err:
	default:
	}
}

func (r *RPCListener) normalizeAddress() string {
	addr := strings.TrimSpace(r.address)
	if addr == "" {
		return ""
	}

	if strings.Contains(addr, ":") {
		return addr
	}

	return fmt.Sprintf(":%s", addr)
}

func (r *RPCListener) Listen() error {
	if r.closed.Load() {
		return fmt.Errorf("listener is closed")
	}

	if r.acceptHandler == nil {
		return fmt.Errorf("accept handler is required")
	}

	if r.errChan == nil {
		r.errChan = make(chan error, 16)
	}

	bindAddress := r.normalizeAddress()
	if bindAddress == "" {
		return fmt.Errorf("address is required")
	}

	listener, err := net.Listen("tcp", bindAddress)
	if err != nil {
		return err
	}
	r.listener = listener

	r.waitGroup.Add(1)
	go func() {
		defer r.waitGroup.Done()
		defer listener.Close()

		for {
			conn, err := listener.Accept()
			if err != nil {
				if r.closed.Load() || errors.Is(err, net.ErrClosed) {
					return
				}
				r.reportErr(err)
				continue
			}

			r.waitGroup.Add(1)
			go func(c net.Conn) {
				defer r.waitGroup.Done()
				defer c.Close()
				r.acceptHandler(c)
			}(conn)
		}
	}()

	return nil
}

func (r *RPCListener) Errors() <-chan error {
	if r.errChan == nil {
		r.errChan = make(chan error, 16)
	}
	return r.errChan
}

func (r *RPCListener) Close() error {
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}

	if r.listener != nil {
		if err := r.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			r.reportErr(err)
		}
	}

	r.waitGroup.Wait()
	return nil
}
