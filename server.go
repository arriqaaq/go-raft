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
	// DefaultHeartbeatInterval = 50 * time.Millisecond
	DefaultHeartbeatInterval = 100 * time.Millisecond
	DefaultElectionTimeout   = 150 * time.Millisecond
	MinimumElectionTimeoutMS = 1000 * time.Millisecond
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
		ElectionTimeOut:   DefaultElectionTimeout,
		HeartBeatInterval: DefaultHeartbeatInterval,
		connectionString:  connectionString,
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
	ElectionTimeOut   time.Duration

	connectionString string
	electionClock    <-chan time.Time

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
	// log.Println("req:term: ", req.CandidateName, req.Term)
	// log.Println("curr:term: ", req.CandidateName, s.currentTerm)

	if req.Term < s.currentTerm {
		resp := RequestVoteResponse{}
		resp.VoteGranted = false
		resp.Term = s.currentTerm
		return resp
	}

	if req.Term > s.currentTerm {
		s.currentTerm = req.Term
		log.Println("term-change: ", req.Term, s.state)
		// Update current term
	} else if s.votedFor != req.CandidateName || s.votedFor != "" {
		log.Println("self-request: ", req.Term)
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
	if len(req.Entries) == 0 {
		log.Println("entry check: ", req.Term, s.currentTerm)
		if req.Term > s.currentTerm {
			s.currentTerm = req.Term
			s.state = FOLLOWER
			// log.Fatal("state changed: ", s.state)
			log.Println("state changed: ", s.state)
		}
	}
	return AppendEntriesResponse{}
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
	log.Println("follower: ", s.currentTerm)
	for {
		select {
		case <-s.electionClock:
			s.currentTerm++
			log.Println("term-increment-1: ", s.currentTerm)
			s.votedFor = ""
			s.leader = ""
			s.state = CANDIDATE
			s.resetElectionTimeout()
			return
		}

	}

}

func (s *Server) Quorom() int {
	// implement locking everywhere
	return len(s.peers)/2 + 1
}

func (s *Server) runCandidate() {
	var respChan chan *RequestVoteResponse

	selfVote := true
	totalVotes := 0

	for s.state == CANDIDATE {
		if selfVote {
			s.currentTerm++
			log.Println("term-increment-2: ", s.currentTerm)
			s.votedFor = s.name

			respChan = make(chan *RequestVoteResponse, len(s.peers))
			for _, peer := range s.peers {
				newVoteReq := &RequestVoteRequest{
					Term:          s.currentTerm,
					CandidateName: s.name,
				}

				go peer.sendVoteRequest(newVoteReq, respChan)
			}

			totalVotes += 1
			s.resetElectionTimeout()
			selfVote = false

		}

		log.Println("candidate: ", s.currentTerm, s.state)

		if totalVotes >= s.Quorom() {
			log.Println("got in one vote", totalVotes, s.Quorom())
			s.state = LEADER
			s.leader = s.name
			return

		}

		for {

			select {
			case <-s.electionClock:
				s.currentTerm++
				log.Println("term-increment-3: ", s.currentTerm)
				s.votedFor = ""
				selfVote = true
				s.resetElectionTimeout()
				return

			case r := <-respChan:
				{

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

			}
		}

	}
}

func (s *Server) runLeader() {
	log.Println("leader: ", s.currentTerm, s.state)
	for _, peer := range s.peers {
		// go peer.startHeartBeat()
		peer.startHeartBeat()
	}
	for {

		select {
		case <-s.electionClock:
			s.currentTerm++
			log.Println("term-increment-4: ", s.currentTerm)
			s.votedFor = ""
			s.resetElectionTimeout()
			return
		}
	}
}
