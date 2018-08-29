package main

import (
	raft "github.com/arriqaaq/go-raft"
	"log"
	"net"
	"net/http"
)

func main() {
	// runnerCmd := flag.String("peers", "", "url string")
	// flag.Parse()

	// p := strings.Split(*runnerCmd, ",")
	addr := ":8001"

	t := raft.NewHTTPTransporter()
	s, serr := raft.NewServer("s2", t, addr)
	if serr != nil {
		log.Println(serr)
	}
	s.AddPeer("s1", "http://0.0.0.0:8000")
	// s.AddPeer("s3", "http://0.0.0.0:8002")

	// Create listener for HTTP server and start it.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	mux := http.NewServeMux()
	t.Install(s, mux)
	httpServer := &http.Server{Addr: addr, Handler: mux}

	go func() { httpServer.Serve(listener) }()
	s.Run()

}
