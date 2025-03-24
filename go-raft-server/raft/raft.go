package raft

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"go-raft-server/kvdb"
	"go-raft-server/peer"
	"go-raft-server/util"
)

// A Go object implementing a single Raft peer.
type Raft struct {
	mu    sync.RWMutex // Lock to protect shared access to this peer's state, to use RWLock for better performance
	peers []peer.Peer  // RPC end points of all peers
	logdb *kvdb.KVDB
	me    int // this peer's index into peers[]

	// Persistent state on all servers(Updated on stable storage before responding to RPCs)
	currentTerm   int // latest term server has seen(initialized to 0 on first boot, increases monotonically)
	votedFor      int // candidateId that received vote in current term(or null if none)
	firstLogIndex int
	lastLogIndex  int

	// Volatile state on all servers
	commitIndex int // index of highest log entry known to be committed(initialized to 0, increases monotonically)
	lastApplied int // index of highest log entry applied to state machine(initialized to 0, increases monotonically)

	// Volatile state on leaders(Reinitialized after election)
	nextIndex  []int // for each server, index of the next log entry to send to that server(initialized to leader last log index + 1)
	matchIndex []int // for each server, index of highest log entry known to be replicated on server(initialized to 0, increases monotonically)

	// other properties
	state          NodeState     // current state of the server
	electionTimer  *time.Timer   // timer for election timeout
	heartbeatTimer *time.Timer   // timer for heartbeat
	applyCh        chan ApplyMsg // channel to send apply message to service
	applyCond      *sync.Cond    // condition variable for apply goroutine
	replicatorCond []*sync.Cond  // condition variable for replicator goroutine
}

func Make(peers []peer.Peer, me int, logdb *kvdb.KVDB, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		mu:             sync.RWMutex{},
		peers:          peers,
		logdb:          logdb,
		me:             me,
		currentTerm:    0,
		votedFor:       -1,
		firstLogIndex:  0,
		lastLogIndex:   0,
		commitIndex:    0,
		lastApplied:    0,
		nextIndex:      make([]int, len(peers)),
		matchIndex:     make([]int, len(peers)),
		state:          Follower,
		electionTimer:  time.NewTimer(RandomElectionTimeout()),
		heartbeatTimer: time.NewTimer(StableHeartbeatTimeout()),
		applyCh:        applyCh,
		replicatorCond: make([]*sync.Cond, len(peers)),
	}

	// initialize from state persisted before a crash
	rf.readPersist()
	if rf.lastLogIndex == 0 {
		if err := rf.storeLogEntry(&LogEntry{Index: 0, Term: 0}); err != nil {
			log.Panicln(err)
		}
	}

	// initialize nextIndex and matchIndex, and start replicator goroutine
	for peer := range peers {
		rf.matchIndex[peer], rf.nextIndex[peer] = 0, rf.lastLogIndex+1
		if peer != rf.me {
			rf.replicatorCond[peer] = sync.NewCond(&sync.Mutex{})
			// start replicator goroutine to send log entries to peer
			go rf.replicator(peer)
		}
	}

	// start ticker goroutine to start elections
	go rf.ticker()

	// should use mu to protect applyCond, avoid other goroutine to change the critical section
	rf.applyCond = sync.NewCond(&rf.mu)

	// start apply goroutine to apply log entries to state machine
	go rf.applier()

	// register rf *raft.Raft service
	if err := util.RegisterRPCService(rf); err != nil {
		panic(fmt.Sprintf("error when register Raft rpc service: %v\n", err))
	}

	return rf
}

func (rf *Raft) replicator(peer int) {
	rf.replicatorCond[peer].L.Lock()
	defer rf.replicatorCond[peer].L.Unlock()
	for {
		fmt.Println()
		for !rf.needReplicating(peer) {
			rf.replicatorCond[peer].Wait()
		}
		// send log entries to peer
		rf.replicateOnceRound(peer)
	}
}

func (rf *Raft) applier() {
	for {
		rf.mu.Lock()
		// check the commitIndex is advanced
		for rf.commitIndex <= rf.lastApplied {
			// need to wait for the commitIndex to be advanced
			rf.applyCond.Wait()
		}

		// apply log entries to state machine
		commitIndex, lastApplied := rf.commitIndex, rf.lastApplied
		entries, err := rf.getLogsInRange(lastApplied+1, commitIndex)
		if err != nil {
			log.Panicln(err)
		}
		rf.mu.Unlock()
		// send the apply message to applyCh for service/State Machine Replica
		for _, entry := range entries {
			rf.applyCh <- ApplyMsg{
				CommandValid: true,
				CommandIndex: entry.Index,
				CommandTerm:  entry.Term,
				Command:      entry.Command,
			}
		}
		rf.mu.Lock()
		util.DPrintf("{Node %v} applies log entries from index %v to %v in term %v", rf.me, lastApplied+1, commitIndex, rf.currentTerm)
		// use commitIndex rather than rf.commitIndex because rf.commitIndex may change during the Unlock() and Lock()
		rf.lastApplied = commitIndex
		rf.persist()
		rf.mu.Unlock()
	}
}

func (rf *Raft) ticker() {
	for {
		select {
		case <-rf.electionTimer.C:
			rf.mu.Lock()
			rf.ChangeState(Candidate)
			rf.currentTerm += 1
			rf.persist()
			// start election
			rf.StartElection()
			rf.electionTimer.Reset(RandomElectionTimeout()) // reset election timer in case of split vote
			rf.mu.Unlock()
		case <-rf.heartbeatTimer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				// should send heartbeat
				rf.BroadcastHeartbeat(true)
				// should send heartbeat again
				rf.heartbeatTimer.Reset(StableHeartbeatTimeout())
			}
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) replicateOnceRound(peer int) {
	rf.mu.RLock()
	if rf.state != Leader {
		rf.mu.RUnlock()
		return
	}
	prevLogIndex := rf.nextIndex[peer] - 1

	args, err := rf.genAppendEntriesArgs(prevLogIndex)
	if err != nil {
		panic(err)
	}
	rf.mu.RUnlock()
	reply := new(AppendEntriesReply)
	util.DPrintf("{Node %v} sends AppendEntriesArgs %v to {Node %v}", rf.me, args, peer)
	if err := rf.sendAppendEntries(peer, args, reply); err == nil {
		util.DPrintf("{Node %v} receives AppendEntriesReply %v from {Node %v}", rf.me, reply, peer)
		rf.mu.Lock()
		if args.Term == rf.currentTerm && rf.state == Leader {
			if !reply.Success {
				if reply.Term > rf.currentTerm {
					// indicate current server is not the leader
					rf.ChangeState(Follower)
					rf.currentTerm, rf.votedFor = reply.Term, -1
					rf.persist()
				} else if reply.Term == rf.currentTerm {
					// decrease nextIndex and retry
					rf.nextIndex[peer] = reply.ConflictIndex
				}
			} else {
				if len(args.Entries) != 0 {
					rf.matchIndex[peer] = args.PrevLogIndex + len(args.Entries)
					rf.nextIndex[peer] = rf.matchIndex[peer] + 1
					// advance commitIndex if possible
					rf.advanceCommitIndexForLeader()
				}
			}
		}
		rf.mu.Unlock()
	} else {
		util.DPrintf("%v", err)
		rf.mu.Lock()
		// reset offline peer's matchindex in oder to send a lot of useless logs
		rf.nextIndex[peer] = rf.lastLogIndex + 1
		// rf.replicatorCond[peer].Wait()
		rf.mu.Unlock()
	}
}

func (rf *Raft) needReplicating(peer int) bool {
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	// check the logs of peer is behind the leader
	return rf.state == Leader && rf.matchIndex[peer] < rf.lastLogIndex
}

//
//
//
// === below no lock
//
//
//

// === persist and readPersist

func (rf *Raft) persist() {
	bytes := []byte(strconv.Itoa(rf.currentTerm))
	if err := rf.logdb.Set([]byte("currentTerm"), bytes); err != nil {
		log.Panicln(err)
	}
	bytes = []byte(strconv.Itoa(rf.votedFor))
	if err := rf.logdb.Set([]byte("votedFor"), bytes); err != nil {
		log.Panicln(err)
	}
	bytes = []byte(strconv.Itoa(rf.firstLogIndex))
	if err := rf.logdb.Set([]byte("firstLogIndex"), bytes); err != nil {
		log.Panicln(err)
	}
	bytes = []byte(strconv.Itoa(rf.lastLogIndex))
	if err := rf.logdb.Set([]byte("lastLogIndex"), bytes); err != nil {
		log.Panicln(err)
	}
	bytes = []byte(strconv.Itoa(rf.commitIndex))
	if err := rf.logdb.Set([]byte("commitIndex"), bytes); err != nil {
		log.Panicln(err)
	}
	bytes = []byte(strconv.Itoa(rf.lastApplied))
	if err := rf.logdb.Set([]byte("lastApplied"), bytes); err != nil {
		log.Panicln(err)
	}
}

// restore previously persisted state.
func (rf *Raft) readPersist() {
	if currentTermBytes, err := rf.logdb.Get([]byte("currentTerm")); err == nil {
		s := string(currentTermBytes)
		rf.currentTerm, err = strconv.Atoi(s)
		if err != nil {
			log.Panicln(err)
		}
	}
	if votedForBytes, err := rf.logdb.Get([]byte("votedFor")); err == nil {
		s := string(votedForBytes)
		rf.votedFor, err = strconv.Atoi(s)
		if err != nil {
			log.Panicln(err)
		}
	}
	if lastLogIndexBytes, err := rf.logdb.Get([]byte("lastLogIndex")); err == nil {
		s := string(lastLogIndexBytes)
		rf.lastLogIndex, err = strconv.Atoi(s)
		if err != nil {
			log.Panicln(err)
		}
	}
	if commitIndexBytes, err := rf.logdb.Get([]byte("commitIndex")); err == nil {
		s := string(commitIndexBytes)
		rf.commitIndex, err = strconv.Atoi(s)
		if err != nil {
			log.Panicln(err)
		}
	}
	if lastAppliedBytes, err := rf.logdb.Get([]byte("lastApplied")); err == nil {
		s := string(lastAppliedBytes)
		rf.lastApplied, err = strconv.Atoi(s)
		if err != nil {
			log.Panicln(err)
		}
	}
}

// === StartElection
func (rf *Raft) StartElection() {
	rf.votedFor = rf.me
	rf.persist()
	args, err := rf.genRequestVoteArgs()
	if err != nil {
		log.Panicln(err)
	}
	grantedVotes := 1
	util.DPrintf("{Node %v} starts election with RequestVoteArgs %v", rf.me, args)
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		go func(peer int) {
			reply := new(RequestVoteReply)
			// util.DPrintf("{Node %v} sends RequestVoteArgs %v to {Node %v}", rf.me, args, peer)
			if rf.sendRequestVote(peer, args, reply) == nil {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				util.DPrintf("{Node %v} receives RequestVoteReply %v from {Node %v} after sending RequestVoteArgs %v", rf.me, reply, peer, args)
				if args.Term == rf.currentTerm && rf.state == Candidate {
					if reply.VoteGranted {
						grantedVotes += 1
						// check over half of the votes
						if grantedVotes > len(rf.peers)/2 {
							util.DPrintf("{Node %v} receives over half of the votes", rf.me)
							rf.ChangeState(Leader)
							rf.BroadcastHeartbeat(true)
						}
					} else if reply.Term > rf.currentTerm {
						rf.ChangeState(Follower)
						rf.currentTerm, rf.votedFor = reply.Term, -1
						rf.persist()
					}
				}
			}
		}(peer)
	}
}

// === BroadcastHeartbeat

func (rf *Raft) BroadcastHeartbeat(isHeartbeat bool) {
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		if isHeartbeat {
			// should send heartbeat to all peers immediately
			go rf.replicateOnceRound(peer)
		} else {
			// just need to signal replicator to send log entries to peer
			rf.replicatorCond[peer].Signal()
		}
	}
}

// === isLogUpToDate

func (rf *Raft) isLogUpToDate(index, term int) bool {
	lastLog, err := rf.getLastLog()
	if err != nil {
		log.Panicln(err)
	}
	return term > lastLog.Term || (term == lastLog.Term && index >= lastLog.Index)
}

// === isLogMatched

func (rf *Raft) isLogMatched(index, term int) bool {
	lastLog, err := rf.getLastLog()
	if err != nil {
		log.Panicln(err)
	}
	return index <= lastLog.Index && term == lastLog.Term
}

// === advanceCommitIndexForLeader

func (rf *Raft) advanceCommitIndexForLeader() {
	n := len(rf.matchIndex)
	sortMatchIndex := make([]int, n)
	copy(sortMatchIndex, rf.matchIndex)
	sort.Ints(sortMatchIndex)
	// get the index of the log entry with the highest index that is known to be replicated on a majority of servers
	newCommitIndex := sortMatchIndex[n-(n/2+1)]
	if newCommitIndex > rf.commitIndex {
		if rf.isLogMatched(newCommitIndex, rf.currentTerm) {
			util.DPrintf("{Node %v} advances commitIndex from %v to %v in term %v", rf.me, rf.commitIndex, newCommitIndex, rf.currentTerm)
			rf.commitIndex = newCommitIndex
			rf.persist()
			rf.applyCond.Signal()
		}
	}
}

//
//
//
// bellow are used by KVServer
//
//
//

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	return rf.currentTerm, rf.state == Leader
}

func (rf *Raft) GetId() int {
	return rf.me
}

func (rf *Raft) Start(command any) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != Leader {
		return -1, -1, false
	}

	// first append log entry for itself
	nextLogIndex := rf.lastLogIndex + 1
	newLogEntry := LogEntry{
		Index:   nextLogIndex,
		Term:    rf.currentTerm,
		Command: command,
	}
	rf.storeLogEntry(&newLogEntry)
	rf.lastLogIndex = nextLogIndex
	rf.persist()

	rf.matchIndex[rf.me], rf.nextIndex[rf.me] = nextLogIndex, nextLogIndex+1
	util.DPrintf("{Node %v} starts agreement on a new log entry with command in term %v", rf.me, rf.currentTerm)
	// then broadcast to all peers to append log entry
	rf.BroadcastHeartbeat(false)
	// return the new log index and term, and whether this server is the leader
	return nextLogIndex, rf.currentTerm, true
}
