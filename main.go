package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Tnze/go-mc/net/packet"
	"io"
	"log"
	"net"
	"os"
	"time"
)

const DefaultListen = "0.0.0.0:25565"
const DefaultForward = "127.0.0.1:25575"

const DefaultFilterHost = "localhost"
const DefaultFilterPort = 25565

const DefaultProxyProtocol = false

var (
	Listen        string
	Forward       string
	FilterHost    string
	FilterPort    uint
	ProxyProtocol bool
	PrintHelp     bool
)

type Message struct {
	Text string `json:"text"`
}

func init() {
	flag.StringVar(&Listen, "bind", DefaultListen, "Binding address")
	flag.StringVar(&Forward, "upstream", DefaultForward, "Upstream")
	flag.StringVar(&FilterHost, "host", DefaultFilterHost, "The host from handshake packet which allowed for connection")
	flag.UintVar(&FilterPort, "port", DefaultFilterPort, "The port from handshake packet which allowed for connection")
	flag.BoolVar(&ProxyProtocol, "proxy", DefaultProxyProtocol, "Send proxy protocol header to upstream")
	flag.BoolVar(&PrintHelp, "help", false, "Print help")
}

func main() {
	flag.Parse()

	if PrintHelp {
		_, _ = fmt.Fprintln(os.Stderr, "Usage:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	listen, err := net.Listen("tcp", Listen)
	if err != nil {
		log.Fatalln("Unable to listen on " + Listen)
	}
	log.Println("Listening on " + Listen)
	defer func(listen net.Listener) {
		_ = listen.Close()
	}(listen)
	for {
		conn, err := listen.Accept()
		if err != nil {
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(downstream net.Conn) {
	flag.Parse()

	p := &packet.Packet{}

	err := p.UnPack(downstream, -1)

	if err != nil {
		_ = downstream.Close()
		return
	}

	if p.ID != 0 {
		_ = downstream.Close()
		return
	}

	var (
		Version  packet.VarInt
		Host     packet.Identifier
		Port     packet.UnsignedShort
		NextStep packet.VarInt
	)

	err = p.Scan(&Version, &Host, &Port, &NextStep)

	if err != nil {
		log.Printf("Failed to parsed packet from %s: %s", downstream.RemoteAddr().String(), err.Error())
		_ = downstream.Close()
		return
	}

	if p.ID != 0 {
		log.Printf(
			"Non handshake packet received from %s",
			downstream.RemoteAddr().String(),
		)
		_ = downstream.Close()
		return
	}

	if Host != packet.String(FilterHost) || Port != packet.UnsignedShort(FilterPort) {
		action := "PING"
		if NextStep == 2 {
			action = "LOGIN"
		}
		log.Printf(
			"Invalid handshake from %s trying to invsetigate on %s:%d (Protocol %d, %s)",
			downstream.RemoteAddr().String(),
			Host, Port, Version, action,
		)
		_ = downstream.Close()
		return
	}

	upstream, err := net.DialTimeout("tcp", Forward, time.Second*5)

	if err != nil {
		if NextStep == 2 {
			kick(downstream, "Failed to connect upstream")
		}
		_ = downstream.Close()
		log.Println("Failed to connect to upstream " + Forward)
		return
	}

	if ProxyProtocol {
		localHost, localPort, _ := net.SplitHostPort(downstream.LocalAddr().String())
		remoteHost, remotePort, _ := net.SplitHostPort(downstream.RemoteAddr().String())

		remoteIp := net.ParseIP(remoteHost)
		localIp := net.ParseIP(localHost)

		connType := "TCP4"
		if remoteIp.To4() == nil || localIp.To4() == nil {
			connType = "TCP6"
			localHost = localIp.To16().String()
			remoteHost = remoteIp.To16().String()
		} else {
			localHost = localIp.To4().String()
			remoteHost = remoteIp.To4().String()
		}

		header := fmt.Sprintf("PROXY %s %s %s %s %s\r\n", connType, remoteHost, localHost, remotePort, localPort)

		_, err = upstream.Write([]byte(header))

		if err != nil {
			log.Println("Failed to write proxy header to upstream")
			if NextStep == 2 {
				kick(downstream, "Failed to connect upstream")
			}
			_ = downstream.Close()
			_ = upstream.Close()
			return
		}
	}

	// Forward handshake packet
	_ = p.Pack(upstream, -1)

	closed := make(chan bool, 2)
	go pipe(closed, downstream, upstream)
	go pipe(closed, upstream, downstream)
	<-closed
	_ = downstream.Close()
	_ = upstream.Close()
}

func pipe(closed chan bool, from net.Conn, to net.Conn) {
	_, _ = io.Copy(from, to)
	closed <- true
}

func kick(conn net.Conn, message string) {
	m := Message{Text: message}
	bytes, _ := json.Marshal(m)
	pack := packet.Marshal(0x00, packet.String(bytes))
	_ = pack.Pack(conn, -1)
	time.Sleep(10 * time.Millisecond)
}
