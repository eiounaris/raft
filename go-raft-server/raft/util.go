package raft

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go-raft-server/util"
	"math/rand"
	"strconv"
	"sync"
	"time"
)

// === Timeout

var ElectionTimeout = 150
var HeartbeatTimeout = 10

type LockedRand struct {
	mu   sync.Mutex
	rand *rand.Rand
}

func (r *LockedRand) Intn(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Intn(n)
}

var GlobalRand = &LockedRand{
	rand: rand.New(rand.NewSource(time.Now().UnixNano())),
}

func RandomElectionTimeout() time.Duration {
	return time.Duration(ElectionTimeout+GlobalRand.Intn(ElectionTimeout)) * time.Millisecond
}

func StableHeartbeatTimeout() time.Duration {
	return time.Duration(HeartbeatTimeout) * time.Millisecond
}

// === NodeState

type NodeState uint8

const (
	Follower NodeState = iota
	Candidate
	Leader
)

func (state NodeState) String() string {
	switch state {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	}
	panic("unexpected NodeState")
}

// === ChangeState

func (rf *Raft) ChangeState(state NodeState) {
	if rf.state == state {
		return
	}
	util.DPrintf("{Node %v} changes state from %v to %v", rf.me, rf.state, state)
	rf.state = state
	switch state {
	case Follower:
		rf.electionTimer.Reset(RandomElectionTimeout())
		rf.heartbeatTimer.Stop() // stop heartbeat
	case Candidate:
	case Leader:
		rf.electionTimer.Stop() // stop election
		rf.heartbeatTimer.Reset(StableHeartbeatTimeout())
		for peer := range rf.peers {
			if peer != rf.me {
				rf.matchIndex[peer], rf.nextIndex[peer] = 0, rf.lastLogIndex+1
			} else {
				rf.matchIndex[peer], rf.nextIndex[peer] = rf.lastLogIndex, rf.lastLogIndex+1
			}
		}
	}
}

// === LogEntry

type LogEntry struct {
	Index   int
	Term    int
	Command any
}

// === rf.logdb *kvdb.KVDB

func (rf *Raft) getLogByIndex(index int) (*LogEntry, error) {
	indexedLogBytes, err := rf.logdb.Get([]byte(strconv.Itoa(index)))
	if err != nil {
		return nil, err
	}
	var indexedLog LogEntry
	r := bytes.NewBuffer(indexedLogBytes)
	d := gob.NewDecoder(r)
	if err := d.Decode(&indexedLog); err != nil {
		panic(err)
	}
	return &indexedLog, nil
}

func (rf *Raft) storeLogEntry(logentry *LogEntry) error {
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	err := e.Encode(logentry)
	if err != nil {
		panic(err)
	}
	err = rf.logdb.Set([]byte(strconv.Itoa(logentry.Index)), w.Bytes())
	if err != nil {
		return err
	}
	return nil
}

func (rf *Raft) getLogsInRange(fromIndex, toIndex int) ([]LogEntry, error) {
	var logs []LogEntry
	if toIndex < fromIndex {
		return logs, nil
	}
	logs = make([]LogEntry, 0, toIndex-fromIndex+1)
	for index := fromIndex; index <= toIndex; index++ {
		indexedlog, err := rf.getLogByIndex(index)
		if err != nil {
			return nil, err
		}
		logs = append(logs, *indexedlog)
	}
	return logs, nil
}

func (rf *Raft) getLastLog() (*LogEntry, error) {
	lastLogBytes, err := rf.logdb.Get([]byte(strconv.Itoa(rf.lastLogIndex)))
	if err != nil {
		return nil, err
	}
	var lastLog LogEntry
	r := bytes.NewBuffer(lastLogBytes)
	d := gob.NewDecoder(r)
	if err = d.Decode(&lastLog); err != nil {
		panic(err)
	}
	return &lastLog, nil
}

// --- ApplyMsg

type ApplyMsg struct {
	CommandValid bool
	CommandIndex int
	CommandTerm  int
	Command      any
}

func (applymsg ApplyMsg) String() string {
	return fmt.Sprintf("{CommandValid:%v, CommandIndex:%v, CommandTerm:%v, Command:%v}",
		applymsg.CommandValid, applymsg.CommandIndex, applymsg.CommandTerm, applymsg.Command)
}

// --- util

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
