package raft

import (
	"os"
	"sync"
)

type Log struct {
	file    *os.File
	entries []*LogEntry
	mutex   sync.Mutex

	// Volatile state of server
	commitIndex int
	lastApplied int
}

func (l *Log) CurrentIndex() int {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	noEntries := len(l.entries)

	if noEntries == 0 {
		return 0
	}

	return (noEntries - 1)

}

func (l *Log) NextIndex() int {
	return l.CurrentIndex() + 1
}

func (l *Log) MatchIndex() {
	// To be implemented
}

type LogEntry struct {
	log   *Log
	Index int `json:"index"`
	Term  int `json:"term"`
}
