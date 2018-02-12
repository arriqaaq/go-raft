package raft

type AppendEntriesRequest struct {
	Term         int
	LeaderID     string
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []*LogEntry
	CommitIndex  int
}

type AppendEntriesResponse struct {
	Term    int
	Success bool
}
