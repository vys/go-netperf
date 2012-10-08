package main

import (
	"flag"
	"github.com/vys/go-humanize"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"syscall"
	"time"
)

type Server struct {
	proto   string
	addr    string
	handler func(c *net.TCPConn) error
}

type Client struct {
	proto       string
	addr        string
	handler     func(c *net.TCPConn) error
	concurrency int
	size        int
	nflight     int
	reqres      bool
	saddr       string
}

func (s *Server) ListenAndGo() error {
	tcpaddr, err := net.ResolveTCPAddr(s.proto, s.addr)
	if err != nil {
		log.Println("Failed to resolve ", s.addr, " with error: ", err)
		return err
	}
	ln, err := net.ListenTCP(s.proto, tcpaddr)
	if err != nil {
		log.Println("Failed to listen for tcp connections on address ", s.addr, " with error: ", err)
		return err
	}

	for {
		conn, err := ln.AcceptTCP()
		if err != nil {
			log.Println("Failed to accept connection ", conn, " with error ", err)
			continue
		}
		log.Println("Client ", conn.RemoteAddr(), " connected")
		go s.handler(conn)
	}
	return nil
}

func (c *Client) NewConnection() (*net.TCPConn, error) {
	srcTcpAddr, err := net.ResolveTCPAddr(c.proto, c.saddr)
	if err != nil {
		log.Println("Failed to resolve ", c.saddr)
		return nil, err
	}
	dstTcpAddr, err := net.ResolveTCPAddr(c.proto, c.addr)
	if err != nil {
		log.Println("Failed to resolve ", c.addr)
		return nil, err
	}
	return net.DialTCP(c.proto, srcTcpAddr, dstTcpAddr)
}

func (c *Client) ConnectAndGo() error {
	conns := make([]*net.TCPConn, c.concurrency)
	for i := 0; i < c.concurrency; i++ {
		conn, err := c.NewConnection()
		if err != nil {
			log.Println("Failed to connect to tcp server on address ", c.addr, " from source address: ", c.saddr, " Error: ", err)
			conn.Close()
			return err
		}
		log.Println("Client ", conn.LocalAddr(), " connected to ", conn.RemoteAddr())
		conns[i] = conn
	}
	for i := 0; i < c.concurrency; i++ {
		go c.handler(conns[i])
	}
	return nil
}

func TCPConnRead(c *net.TCPConn) error {
	c.SetReadBuffer(*packetsize)
	for {
		_, err := c.Read(b)
		if err != nil {
			if err == io.EOF {
				log.Println("Client ", c.RemoteAddr(), " disconnected")
				c.Close()
				return nil
			} else {
				log.Println("Failed reading bytes from conn: ", c, " with error ", err)
				c.Close()
				return err
			}
		}
	}
	return nil
}

var b []byte

func TCPConnWrite(c *net.TCPConn) error {
	c.SetWriteBuffer(*packetsize)

	for {
		_, err := c.Write(b)
		if err != nil {
			if err == io.EOF {
				log.Println("Client ", c.RemoteAddr(), " disconnected")
				c.Close()
				return nil
			} else {
				log.Println("Failed writing bytes to conn: ", c, " with error ", err)
				c.Close()
				return err
			}
		}
	}
	return nil
}

var packetsize *int

func main() {

	host := flag.String("host", "127.0.0.1", "Host IP address")
	port := flag.String("port", "12345", "Port")
	shost := flag.String("shost", "127.0.0.1", "Host IP address")
	sport := flag.String("sport", "0", "Port")
	listen := flag.Bool("listen", false, "Listen")
	packetsize = flag.Int("size", 1500, "Size of packets to send")
	nconn := flag.Int("nconn", 254, "Number of concurrent connections")
	reqres := flag.Bool("reqres", false, "Request/Response protocol")
	nflight := flag.Int("nflight", 1024, "Number of requests in flight before waiting for response")
	profile := flag.String("profile", "", "write profile to file with following prefix")
	flag.Parse()

	if *profile != "" {
		go doprofile(*profile)
	}

	if flag.NArg() != 0 {
		log.Println("Usage:")
		flag.PrintDefaults()
		return
	}

	b = make([]byte, *packetsize)

	go GoRuntimeStats()

	if *listen {

		s := &Server{proto: "tcp", addr: net.JoinHostPort(*host, *port), handler: TCPConnWrite}
		s.ListenAndGo()

	} else {
		c := &Client{proto: "tcp", addr: net.JoinHostPort(*host, *port), handler: TCPConnRead,
			size: *packetsize, concurrency: *nconn, nflight: *nflight, reqres: *reqres,
			saddr: net.JoinHostPort(*shost, *sport)}

		c.ConnectAndGo()

		SigIntHandler()
	}

	log.Println("Finished execution!")
}

func SigIntHandler() {
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT)
	<-ch
	log.Println("CTRL-C; exiting")
	os.Exit(0)
}

func GoRuntimeStats() {
	m := new(runtime.MemStats)
	for {
		time.Sleep(5 * time.Second)
		log.Println("# goroutines: ", runtime.NumGoroutine())
		runtime.ReadMemStats(m)
		log.Println("Memory Acquired: ", humanize.Bytes(m.Sys))
		log.Println("Memory Used    : ", humanize.Bytes(m.Alloc))
		log.Println("# malloc       : ", m.Mallocs)
		log.Println("# free         : ", m.Frees)
		log.Println("GC enabled     : ", m.EnableGC)
		log.Println("# GC           : ", m.NumGC)
		log.Println("Last GC time   : ", m.LastGC)
		log.Println("Next GC        : ", humanize.Bytes(m.NextGC))
		//runtime.GC()
	}
}

func doprofile(fn string) {
	var err error
	var fc, fh, ft *os.File
	for i := 1; i > 0; i++ {
		fc, err = os.Create(fn + "-cpu-" + strconv.Itoa(i) + ".prof")
		if err != nil {
			log.Fatal(err)
		}

		pprof.StartCPUProfile(fc)
		time.Sleep(300 * time.Second)
		pprof.StopCPUProfile()
		fc.Close()

		fh, err = os.Create(fn + "-heap-" + strconv.Itoa(i) + ".prof")
		if err != nil {
			log.Fatal(err)
		}
		pprof.WriteHeapProfile(fh)
		fh.Close()

		ft, err = os.Create(fn + "-threadcreate-" + strconv.Itoa(i) + ".prof")
		if err != nil {
			log.Fatal(err)
		}
		pprof.Lookup("threadcreate").WriteTo(ft, 0)
		ft.Close()
		log.Println("Created CPU, heap and threadcreate profile of 300 seconds")
	}
}
