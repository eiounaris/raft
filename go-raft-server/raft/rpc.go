package raft

import (
	"fmt"

	"go-raft-server/util"
)

// --- RequestVote

type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

func (rf *Raft) genRequestVoteArgs() (*RequestVoteArgs, error) {
	lastLog, err := rf.getLastLog()
	if err != nil {
		return nil, err
	}
	args := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLog.Index,
		LastLogTerm:  lastLog.Term,
	}
	return args, nil
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Reply false if term < currentTerm(§5.1)
	// if the term is same as currentTerm, and the votedFor is not null and not the candidateId, then reject the vote(§5.2)
	if args.Term < rf.currentTerm || (args.Term == rf.currentTerm && rf.votedFor != -1 && rf.votedFor != args.CandidateId) {
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return nil
	}

	if args.Term > rf.currentTerm {
		rf.ChangeState(Follower)
		rf.currentTerm, rf.votedFor = args.Term, -1
		rf.persist()
	}

	// if candidate's log is not up-to-date, reject the vote(§5.4)
	if !rf.isLogUpToDate(args.LastLogIndex, args.LastLogTerm) {
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return nil
	}

	rf.votedFor = args.CandidateId
	rf.persist()
	rf.electionTimer.Reset(RandomElectionTimeout())
	reply.Term, reply.VoteGranted = rf.currentTerm, true
	util.DPrintf("{Node %v}'s state is {state %v, term %v}} after processing RequestVote,  RequestVoteArgs %v and RequestVoteReply %v ", rf.me, rf.state, rf.currentTerm, args, reply)
	return nil
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) error {
	return rf.peers[server].Call("Raft", "RequestVote", args, reply)
}

// --- AppendEntries

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	LeaderCommit int
	Entries      []LogEntry
}

type AppendEntriesReply struct {
	Term          int
	Success       bool
	ConflictIndex int
}

func (rf *Raft) genAppendEntriesArgs(prevLogIndex int) (*AppendEntriesArgs, error) {
	entries, err := rf.getLogsInRange(prevLogIndex+1, Min(rf.lastLogIndex, prevLogIndex+1+10)) // 10: max logEntry nums once transmited
	if err != nil {
		return nil, err
	}
	prevLog, err := rf.getLogByIndex(prevLogIndex)
	if err != nil {
		return nil, err
	}
	args := &AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLog.Term,
		LeaderCommit: rf.commitIndex,
		Entries:      entries,
	}
	return args, nil
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Reply false if term < currentTerm(§5.1)
	if args.Term < rf.currentTerm {
		reply.Term, reply.Success = rf.currentTerm, false
		return nil
	}

	// indicate the peer is the leader
	if args.Term > rf.currentTerm {
		rf.currentTerm, rf.votedFor = args.Term, -1
		rf.persist()
	}

	rf.ChangeState(Follower)
	rf.electionTimer.Reset(RandomElectionTimeout())

	// check the log is matched, if not, return the conflict index and term
	// if an existing entry conflicts with a new one (same index but different terms), delete the existing entry and all that follow it(§5.3)
	if !rf.isLogMatched(args.PrevLogIndex, args.PrevLogTerm) {
		reply.Term, reply.Success = rf.currentTerm, false
		lastLog, err := rf.getLastLog()
		if err != nil {
			panic(err)
		}
		// find the first index of the conflicting term
		if lastLog.Index < args.PrevLogIndex {
			// the last log index is smaller than the prevLogIndex, then the conflict index is the last log index
			reply.ConflictIndex = lastLog.Index + 1
		} else {
			// find the first index of the conflicting term
			index := args.PrevLogIndex
			indexLog, err := rf.getLogByIndex(index)
			if err != nil {
				panic(err)
			}
			for index >= rf.commitIndex+1 && indexLog.Term == lastLog.Term {
				index--
			}
			reply.ConflictIndex = index + 1
		}
		return nil
	}

	// append any new entries not already in the log
	for i := range args.Entries {
		if err := rf.storeLogEntry(&args.Entries[i]); err != nil {
			panic(err)
		}
	}
	rf.lastLogIndex = args.PrevLogIndex + len(args.Entries)
	rf.persist()

	// If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry) (paper)
	newCommitIndex := Min(args.LeaderCommit, rf.lastLogIndex)
	if newCommitIndex > rf.commitIndex {
		util.DPrintf("{Node %v} advances commitIndex from %v to %vin term %v", rf.me, rf.commitIndex, newCommitIndex, rf.currentTerm)
		rf.commitIndex = newCommitIndex
		rf.persist()
		rf.applyCond.Signal()
	}
	reply.Term, reply.Success = rf.currentTerm, true
	util.DPrintf("{Node %v}'s state is {state %v, term %v} after processing AppendEntries,  AppendEntriesArgs %v and AppendEntriesReply %v ", rf.me, rf.state, rf.currentTerm, args, reply)
	return nil
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	return rf.peers[server].Call("Raft", "AppendEntries", args, reply)
}

// --- Print Args And Reply gracefully

func (args RequestVoteArgs) String() string {
	return fmt.Sprintf("{Term:%v, CandidateId:%v, LastLogIndex:%v, LastLogTerm:%v}", args.Term, args.CandidateId, args.LastLogIndex, args.LastLogTerm)
}

func (reply RequestVoteReply) String() string {
	return fmt.Sprintf("{Term:%v, VoteGranted:%v}", reply.Term, reply.VoteGranted)
}

func (args AppendEntriesArgs) String() string {
	return fmt.Sprintf("{Term:%v, LeaderId:%v, PrevLogIndex:%v, PrevLogTerm:%v, LeaderCommit:%v, Entries:%v}", args.Term, args.LeaderId, args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommit, args.Entries)
}

func (reply AppendEntriesReply) String() string {
	return fmt.Sprintf("{Term:%v, Success:%v, ConflictIndex:%v}", reply.Term, reply.Success, reply.ConflictIndex)
}

// === Hello (rpc help test process, now it is helpless)

// func (rf *Raft) Hello(args *string, reply *string) error {
// 	*reply = "Hello, " + *args
// 	return nil
// }
