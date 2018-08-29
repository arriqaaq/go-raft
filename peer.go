package raft

import (
	"log"
	"sync"
	"time"
)

type Peer struct {
	name              string
	server            *Server
	connectionString  string `json:"connectionString"`
	mutex             sync.RWMutex
	heartBeatInterval time.Duration
	stopChan          chan bool
}

func (p *Peer) Name() string {
	return p.name
}

func (p *Peer) sendVoteRequest(req *RequestVoteRequest, c chan *RequestVoteResponse) {
	// log.Println("peer.vote: ", p.Name(), "->", p.Name)

	if resp := p.server.transporter.SendVoteRequest(p.server, p, req); resp != nil {
		// log.Println("peer.vote.recv: ", p.Name(), "<-", resp)
		c <- resp
	} else {
		log.Println("peer.vote.failed: ", p.Name(), "<-", p.Name)
	}
}

func (p *Peer) startHeartBeat() {
	p.stopChan = make(chan bool)
	go p.heartbeat()
	// p.heartbeat()
}

// Stops the peer heartbeat.
func (p *Peer) stopHeartbeat() {
	p.stopChan <- true
}

func (p *Peer) heartbeat() {
	hbClock := time.Tick(p.heartBeatInterval)

	for {
		select {
		case <-hbClock:
			{
				ct := p.server.currentTerm
				newAERequest := &AppendEntriesRequest{
					Term:     ct,
					LeaderID: p.server.name,
				}
				resp := p.server.transporter.SendAppendEntriesRequest(p.server, p, newAERequest)
				if resp != nil {
					p.server.events <- resp
					if resp.Success {
						log.Println("heartbeat success")
					}
				} else {
					log.Println("heartbeat failed")
				}
			}
		case <-p.stopChan:
			return
		}
	}
}
