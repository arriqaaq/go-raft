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
	// addr := "http://0.0.0.0:8000"
	addr := ":8000"

	t := raft.NewHTTPTransporter()
	s, serr := raft.NewServer("s1", t, addr)
	if serr != nil {
		log.Println(serr)
	}
	s.AddPeer("s2", "http://0.0.0.0:8001")
	// s.AddPeer("s3", "http://0.0.0.0:8002")

	// Create listener for HTTP server and start it.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Println("eylo", addr)
		panic(err)
	}
	defer listener.Close()

	mux := http.NewServeMux()

	t.Install(s, mux)
	httpServer := &http.Server{Addr: addr, Handler: mux}

	go func() { httpServer.Serve(listener) }()
	s.Run()

}
