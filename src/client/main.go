package main

import (
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/moxianfeng/langconn/src/tools"
)

const (
	ENV_SERVER_ADDRESS = "SERVER_ADDRESS"
	BACKEND_ADDRESS    = "BACKEND_ADDRESS"
	ENV_CONN_COUNT     = "CONN_COUNT"
)

var (
	serverAddress  = "113.31.145.198:9091"
	backendAddress = "127.0.0.1:80"
	connCount      = "20"

	pool []net.Conn

	connSleep int64 = 10
)

func ParseEnv() {
	serverAddress = tools.GetEnv(ENV_SERVER_ADDRESS, serverAddress)
	backendAddress = tools.GetEnv(BACKEND_ADDRESS, backendAddress)
	connCount = tools.GetEnv(ENV_CONN_COUNT, connCount)
}

type Client struct {
	id   string
	conn net.Conn
	peer net.Conn
	pool *ClientPool
}

func (c *Client) Do() {
	buf := make([]byte, 1)
	n, err := c.conn.Read(buf)
	if nil != err {
		log.Printf("Error: %v\n", err)
		c.conn.Close()
		c.pool.Remove(c.id)
		return
	}
	if n == 0 {
		log.Println("Error: Read got 0, what happened")
		c.conn.Close()
		c.pool.Remove(c.id)
		return
	}

	log.Printf("[%v->%v] active\n", c.conn.LocalAddr(), c.conn.RemoteAddr())
	// make backend connection
	backend, err := net.Dial("tcp", backendAddress)
	if nil != err {
		log.Printf("Error: %v\n", err)
		c.conn.Close()
		c.pool.Remove(c.id)
		return
	}
	c.peer = backend

	log.Printf("[%v->%v] peer to [%v->%v]\n",
		c.conn.LocalAddr(), c.conn.RemoteAddr(), c.peer.LocalAddr(), c.peer.RemoteAddr())

	n, err = c.peer.Write(buf)
	if nil != err || n != 1 {
		log.Printf("Error: write first byte to peer failed, %v, %d\n", err, n)
		c.conn.Close()
		c.peer.Close()
		c.pool.Remove(c.id)
		return
	}
	ch := make(chan int)
	// do io copy
	go func() {
		io.Copy(c.peer, c.conn)
		log.Printf("conn [%v->%v] copy finished\n", c.conn.LocalAddr(), c.conn.RemoteAddr())
		c.conn.Close()
		c.peer.Close()
		ch <- 0
	}()

	go func() {
		io.Copy(c.conn, c.peer)
		log.Printf("peer [%v->%v] copy finished\n", c.peer.LocalAddr(), c.peer.RemoteAddr())
		c.peer.Close()
		c.conn.Close()
		ch <- 0
	}()

	go func() {
		<-ch
		<-ch
		close(ch)
		// 结束后，从poll中移除连接
		c.pool.Remove(c.id)
	}()
}

type ClientPool struct {
	pool map[string]*Client
	m    sync.Mutex
}

func (cp *ClientPool) Count() int {
	cp.m.Lock()
	defer cp.m.Unlock()
	if nil == cp.pool {
		return 0
	}
	return len(cp.pool)
}

func (cp *ClientPool) Add(conn net.Conn) {
	cp.m.Lock()
	defer cp.m.Unlock()

	if nil == cp.pool {
		cp.pool = make(map[string]*Client)
	}

	client := &Client{id: uuid.New().String(), conn: conn, pool: cp}

	cp.pool[client.id] = client
	go client.Do()
}

func (cp *ClientPool) Remove(id string) {
	cp.m.Lock()
	defer cp.m.Unlock()
	delete(cp.pool, id)

	log.Printf("Info: current pool size: %d\n", len(cp.pool))
}

func makePool(poll *ClientPool, count int) {
	if nil == pool {
		pool = make([]net.Conn, 0)
	}

	for {
		// 连接池已满
		if poll.Count() >= count {
			time.Sleep(time.Duration(connSleep) * time.Millisecond)
			continue
		}

		// 新建一个连接
		conn, err := net.Dial("tcp", serverAddress)
		if nil != err {
			// 失败后增加延迟
			log.Println(err)
			time.Sleep(time.Duration(connSleep) * time.Millisecond)
			connSleep *= 2
			continue
		}
		// 成功后恢复延迟
		poll.Add(conn)
		connSleep = 10
	}
}

func main() {
	ParseEnv()
	iConnCount, err := strconv.ParseInt(connCount, 10, 32)
	if nil != err {
		log.Fatal(err)
	}
	if iConnCount <= 0 {
		log.Fatal("Error: invalid conn count")
	}

	cp := &ClientPool{}
	makePool(cp, int(iConnCount))
}

func init() {
	log.SetFlags(log.Lshortfile)
}
