package raft

import (
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"
)

type ServerState int

const (
	CANDIDATE ServerState = iota
	FOLLOWER
	LEADER
)

const (
	// DefaultHeartbeatInterval is the interval that the leader will send
	// AppendEntriesRequests to followers to maintain leadership.
	DefaultHeartbeatInterval = 50 * time.Millisecond
	MinimumElectionTimeoutMS = 3000 * time.Millisecond
	MaximumElectionTimeoutMS = 2 * MinimumElectionTimeoutMS
)

type Config struct {
}

// electionTimeout returns a variable time.Duration, between the minimum and
// maximum election timeouts.
func electionTimeout() time.Duration {
	rand := rand.New(rand.NewSource(time.Now().UnixNano()))
	d, delta := MinimumElectionTimeoutMS, (MaximumElectionTimeoutMS - MinimumElectionTimeoutMS)
	if delta > 0 {
		d += time.Duration(rand.Int63n(int64(delta)))
	}
	return d
}

// Creates a new server with a log at the given path. transporter must
// not be nil. stateMachine can be nil if snapshotting and log
// compaction is to be disabled. context can be anything (including nil)
// and is not used by the raft package except returned by
// Server.Context(). connectionString can be anything.

func NewServer(name string, transporter *HTTPTransporter, connectionString string) (*Server, error) {
	if name == "" {
		return nil, errors.New("raft.Server: Name cannot be blank")
	}
	// if transporter == nil {
	// 	panic("raft: Transporter required")
	// }

	s := &Server{
		name:              name,
		transporter:       transporter,
		state:             FOLLOWER,
		peers:             make(map[string]*Peer),
		exitChannel:       make(chan struct{}),
		stopChan:          make(chan bool),
		HeartBeatInterval: DefaultHeartbeatInterval,
		connectionString:  connectionString,
		events:            make(chan interface{}),
	}
	s.resetElectionTimeout()

	return s, nil
}

type Server struct {
	*Config

	name string

	// Used to store logs
	path        string
	state       ServerState
	candidateID string
	mutex       sync.RWMutex

	// Peers
	peers map[string]*Peer

	// Persistent state
	currentTerm int
	votedFor    string
	log         *Log
	leader      string

	transporter *HTTPTransporter

	HeartBeatInterval time.Duration

	connectionString string
	electionClock    <-chan time.Time

	events chan interface{}

	stopChan    chan bool
	exitChannel chan struct{}
}

func (s *Server) Name() string {
	return s.name
}

func (s *Server) AddPeer(name, connectionString string) {

	p := &Peer{
		server:            s,
		connectionString:  connectionString,
		name:              name,
		heartBeatInterval: s.HeartBeatInterval,
	}
	s.peers[p.Name()] = p
}

func (s *Server) RequestVote(req *RequestVoteRequest) RequestVoteResponse {
	// log.Println("req:vote: ", req.CandidateName, req.Term)

	if req.Term < s.currentTerm {
		resp := RequestVoteResponse{}
		resp.VoteGranted = false
		resp.Term = s.currentTerm
		return resp
	}

	if req.Term > s.currentTerm {
		// log.Println("term-change: ", req.Term, s.state)
		s.updateCurrentTerm(req.Term, FOLLOWER)
		// Update current term
	} else if s.votedFor != req.CandidateName || s.votedFor != "" {
		// log.Println("self-request: ", req.Term)
		resp := RequestVoteResponse{}
		resp.Term = s.currentTerm
		resp.VoteGranted = false
		return resp
	}

	log.Println(s.name, " voted for ", req.CandidateName, " in term: ", s.currentTerm)
	s.votedFor = req.CandidateName

	resp := RequestVoteResponse{}
	resp.Term = s.currentTerm
	resp.VoteGranted = true

	return resp
}

func (s *Server) AppendEntries(req *AppendEntriesRequest) AppendEntriesResponse {
	log.Println("entry check: ", req.Term, s.currentTerm, s.state)
	if req.Term > s.currentTerm {
		s.updateCurrentTerm(req.Term, FOLLOWER)
		// log.Println("state changed: ", s.state)
	}
	s.resetElectionTimeout()

	return AppendEntriesResponse{
		Term: s.currentTerm,
	}
}

func (s *Server) updateCurrentTerm(term int, state ServerState) {
	// log.Println("updating term")
	if s.state == LEADER {
		s.stopChan <- true
		for _, peer := range s.peers {
			peer.stopHeartbeat()
		}
	}
	s.currentTerm = term
	s.state = state
}
func (s *Server) Run() {
	// log.Println("state: ", s.state)
	for {
		select {
		case <-s.exitChannel:
			return
		default:
		}

		switch s.state {
		case CANDIDATE:
			s.runCandidate()
		case FOLLOWER:
			s.runFollower()
		case LEADER:
			s.runLeader()
		default:
			return
		}

	}

}

func (s *Server) resetElectionTimeout() {
	s.electionClock = time.NewTimer(electionTimeout()).C
}

func (s *Server) runFollower() {
	log.Println("I am a FOLLOWER!!!", s.currentTerm)

	for {
		select {
		case e := <-s.events:
			switch ev := e.(type) {
			case *AppendEntriesRequest:
				continue
			case *AppendEntriesResponse:
				if ev.Term > s.currentTerm {
					s.updateCurrentTerm(ev.Term, FOLLOWER)
				}
				s.resetElectionTimeout()
			}
		case <-s.electionClock:
			log.Println("election timeout: ", s.currentTerm)
			s.currentTerm++
			// log.Println("term-increment-1: ", s.currentTerm)
			s.votedFor = ""
			s.leader = ""
			s.state = CANDIDATE
			s.resetElectionTimeout()
			log.Println("exit")
			return
		}

	}

}

func (s *Server) Quorom() int {
	// implement locking everywhere
	if len(s.peers) > 1 {
		return len(s.peers)/2 + 1
	}
	return 2
}

func (s *Server) runCandidate() {
	log.Println("I am a CANDIDATE!!!")
	var respChan chan *RequestVoteResponse

	selfVote := true
	totalVotes := 0

	for s.state == CANDIDATE {
		if selfVote {
			s.currentTerm++
			// log.Println("term-increment-2: ", s.currentTerm)
			s.votedFor = s.name

			respChan = make(chan *RequestVoteResponse, len(s.peers))
			for _, peer := range s.peers {
				newVoteReq := &RequestVoteRequest{
					Term:          s.currentTerm,
					CandidateName: s.name,
				}
				// avoiding pointer overwrite in goroutines loop
				np := peer
				np.sendVoteRequest(newVoteReq, respChan)
				// go func(p *Peer) {
				// 	p.sendVoteRequest(newVoteReq, respChan)
				// }(peer)
			}

			totalVotes += 1
			s.resetElectionTimeout()
			selfVote = false

		}

		// log.Println("candidate: ", s.currentTerm)

		if totalVotes >= s.Quorom() {
			log.Println("got in one vote", totalVotes, s.Quorom())
			s.state = LEADER
			s.leader = s.name
			return

		}

		for {

			select {
			case <-s.electionClock:
				// log.Println("term-increment-3: ", s.currentTerm)
				s.votedFor = ""
				selfVote = true
				s.resetElectionTimeout()
				return

			case r := <-respChan:
				{
					if r.Term > s.currentTerm {
						s.leader = ""
						s.state = FOLLOWER
						return
					} else if r.Term < s.currentTerm {
						continue
					}

					if r.VoteGranted {
						totalVotes++
						log.Println("got vote: ", totalVotes, r.Term, s.currentTerm)
					}
					// log.Println("req:term: ", r.Term)
					// log.Println("curr:term: ", s.currentTerm)

					if totalVotes >= s.Quorom() {
						s.state = LEADER
						s.leader = s.name
						return
					}

				}
			case e := <-s.events:
				switch ev := e.(type) {
				case *AppendEntriesRequest:
					log.Println("unexpected rquest")
				case *AppendEntriesResponse:
					log.Println("candidate append response")
					if ev.Term > s.currentTerm {
						s.updateCurrentTerm(ev.Term, FOLLOWER)
					}
					// s.resetElectionTimeout()

				}

			}
		}

	}
}

func (s *Server) runLeader() {
	log.Println("I am a LEADER!!!", s.currentTerm)
	for _, peer := range s.peers {
		// go peer.startHeartBeat()
		peer.startHeartBeat()
	}
	for {

		select {
		case <-s.stopChan:
			s.state = FOLLOWER
			log.Println("exiting as leader")
			return
		}
	}
}
