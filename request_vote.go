package raft

type RequestVoteRequest struct {
	Term          int
	CandidateName string
	LastLogIndex  int
	LastLogTerm   int
}

type RequestVoteResponse struct {
	Term        int
	VoteGranted bool
}
