package main

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/moxianfeng/langconn/src/tools"
)

const (
	ENV_SERVER_PORT = "SERVER_PORT"
	ENV_PEER_PORT   = "PEER_PORT"
)

var (
	serverPort = ":9090"
	peerPort   = ":9091"
)

func ParseEnv() {
	serverPort = tools.GetEnv(ENV_SERVER_PORT, serverPort)
	peerPort = tools.GetEnv(ENV_PEER_PORT, peerPort)
}

func serverRoutine(ln net.Listener, ch chan net.Conn) {
	for {
		conn, err := ln.Accept()
		if nil != err {
			log.Println("serverRoutine:", err)
			continue
		}

		// 通知管理进程有连接
		log.Printf("Info: new connection[%v->%v] put it to main goroutine\n",
			conn.RemoteAddr(), conn.LocalAddr())
		ch <- conn
	}
}

func peerRoutine(ln net.Listener, peerPool *PeerPool) {
	for {
		conn, err := ln.Accept()
		if nil != err {
			log.Println("peerRoutine:", err)
			continue
		}

		log.Printf("Info: new peer connection[%v->%v]\n", conn.RemoteAddr(), conn.LocalAddr())
		// 添加到空闲连接池中
		peerPool.Add(conn)
	}
}

type PeerPool struct {
	conns []net.Conn
	m     sync.Mutex
}

func (pp *PeerPool) Add(conn net.Conn) {
	pp.m.Lock()
	defer pp.m.Unlock()

	pp.conns = append(pp.conns, conn)
}

func (pp *PeerPool) Get() (net.Conn, bool) {
	pp.m.Lock()
	defer pp.m.Unlock()

	l := len(pp.conns)

	if l == 0 {
		return nil, false
	}

	ret := pp.conns[l-1]
	pp.conns = pp.conns[:l-1]
	return ret, true
}

func (pp *PeerPool) TestRoutine() {
	for {
		pp.m.Lock()
		closed := []int{}
		// 检测连接是否中断
		for i, c := range pp.conns {
			c.SetReadDeadline(time.Now().Add(time.Microsecond))
			one := make([]byte, 1)
			if _, err := c.Read(one); err == io.EOF {
				log.Printf("Info: [%v->%v] detected closed LAN connection\n", c.RemoteAddr(), c.LocalAddr())
				closed = append(closed, i)
			} else {
				c.SetReadDeadline(time.Time{})
			}
		}

		// 从大向小删除
		for i := len(closed) - 1; i >= 0; i-- {
			pp.conns[i].Close()
			pp.conns = append(pp.conns[:i], pp.conns[i+1:]...)
		}

		time.Sleep(time.Millisecond)
		pp.m.Unlock()
	}
}

func main() {
	serverLn, err := net.Listen("tcp", serverPort)
	if nil != err {
		log.Fatal(err)
	}

	frontChan := make(chan net.Conn)

	peerLn, err := net.Listen("tcp", peerPort)
	if nil != err {
		log.Fatal(err)
	}

	peerPool := &PeerPool{}
	go serverRoutine(serverLn, frontChan)
	go peerRoutine(peerLn, peerPool)
	go peerPool.TestRoutine()

	for {
		{ // let defer occur
			frontConn, ok := <-frontChan
			if !ok {
				break
			}

			backendConn, ok := peerPool.Get()
			if !ok {
				log.Printf("Error: no free backend connection, disconnect frontend connection[%v->%v]\n",
					frontConn.RemoteAddr(), frontConn.LocalAddr())
				frontConn.Close()
				continue
			}

			log.Printf("Info: match front connection[%v->%v] to backend[%v->%v]\n",
				frontConn.RemoteAddr(), frontConn.LocalAddr(), backendConn.RemoteAddr(), backendConn.LocalAddr())

			// let main goroutine continue
			go func() {
				io.Copy(backendConn, frontConn)
				frontConn.Close()
				backendConn.Close()
			}()

			go func() {
				io.Copy(frontConn, backendConn)
				backendConn.Close()
				frontConn.Close()
			}()
		}
	}
}

func init() {
	log.SetFlags(log.Lshortfile)
}
