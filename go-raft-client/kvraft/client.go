package kvraft

import (
	"go-raft-client/peer"
)

type Clerk struct {
	servers  []peer.Peer
	leaderId int
}

func MakeClerk(servers []peer.Peer) *Clerk {
	return &Clerk{
		servers:  servers,
		leaderId: 0,
	}
}

func (ck *Clerk) Get(key []byte) *CommandReply {
	return ck.ExecuteCommand(&CommandArgs{Key: key, Op: OpGet})
}

func (ck *Clerk) Set(key []byte, value []byte, version int) *CommandReply {
	return ck.ExecuteCommand(&CommandArgs{Key: key, Value: value, Version: version, Op: OpSet})
}

func (ck *Clerk) Delete(key []byte, version int) *CommandReply {
	return ck.ExecuteCommand(&CommandArgs{Key: key, Version: version, Op: OpDelete})
}

func (ck *Clerk) ExecuteCommand(args *CommandArgs) *CommandReply {
	reply := new(CommandReply)
	for {
		err := ck.servers[ck.leaderId].Call("KVServer", "ExecuteCommand", args, reply)
		if err != nil || reply.Err == ErrWrongLeader || reply.Err == ErrTimeout {
			// fmt.Printf("recive ok?: %v, reply.Err: %v from %v\n", ok, reply.Err, ck.leaderId)
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
			continue
		}
		return reply
	}
}
