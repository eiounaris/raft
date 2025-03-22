package kvraft

import (
	"encoding/gob"
	"fmt"
	"log"
	"sync"
	"time"

	"go-raft-server/kvdb"
	"go-raft-server/peer"
	"go-raft-server/raft"
	"go-raft-server/util"
)

type bufferedCommand struct {
	args *CommandArgs
	ch   chan *CommandReply
}

type KVServer struct {
	mu           sync.RWMutex
	me           int
	rf           *raft.Raft
	applyCh      chan raft.ApplyMsg
	stateMachine *KVVDB
	notifyChs    map[int][]chan *CommandReply

	buffer         []bufferedCommand
	bufferLock     sync.Mutex
	executeTimeout time.Duration
	batchSize      int
	batchTimeout   time.Duration
}

func (kv *KVServer) ExecuteCommand(args *CommandArgs, reply *CommandReply) error {
	if _, ok := kv.rf.GetState(); !ok {
		reply.Err = ErrWrongLeader
		return nil
	}
	ch := make(chan *CommandReply, 1)
	kv.bufferLock.Lock()
	kv.buffer = append(kv.buffer, bufferedCommand{args: args, ch: ch})

	if len(kv.buffer) >= kv.batchSize {
		batch := make([]bufferedCommand, len(kv.buffer))
		copy(batch, kv.buffer)
		kv.buffer = kv.buffer[:0]
		kv.bufferLock.Unlock()
		go kv.submitBatch(batch)
	} else {
		kv.bufferLock.Unlock()
	}

	select {
	case result := <-ch:
		reply.Value, reply.Version, reply.Err = result.Value, result.Version, result.Err
	case <-time.After(kv.executeTimeout):
		reply.Err = ErrTimeout
	}
	return nil
}

func (kv *KVServer) submitBatch(batch []bufferedCommand) {
	if len(batch) == 0 {
		return
	}

	commands := make([]Command, len(batch))
	for i, bc := range batch {
		commands[i] = Command{&CommandArgs{Op: bc.args.Op, Key: bc.args.Key, Value: bc.args.Value, Version: bc.args.Version}}
	}

	index, _, isLeader := kv.rf.Start(commands)
	if !isLeader {
		for _, bc := range batch {
			bc.ch <- &CommandReply{Err: ErrWrongLeader}
		}
		return
	}

	kv.mu.Lock()
	defer kv.mu.Unlock()
	chs := make([]chan *CommandReply, len(batch))
	for i, bc := range batch {
		chs[i] = bc.ch
	}
	kv.notifyChs[index] = chs
}

func (kv *KVServer) applyLogToStateMachine(command Command) *CommandReply {
	reply := new(CommandReply)
	switch command.Op {
	case OpGet:
		reply.Value, reply.Version, reply.Err = kv.stateMachine.Get(command.Key)
	case OpSet:
		reply.Err = kv.stateMachine.Set(command.Key, command.Value, command.Version)
	case OpDelete:
		reply.Err = kv.stateMachine.Delete(command.Key, command.Version)
	}
	return reply
}

func (kv *KVServer) applier() {
	for message := range kv.applyCh {
		log.Printf("{Node %v} tries to apply message %v\n", kv.rf.GetId(), message)
		if message.CommandValid {
			kv.mu.Lock()
			switch cmd := message.Command.(type) {
			case []Command:
				var replies []*CommandReply
				for _, c := range cmd {
					reply := kv.applyLogToStateMachine(c)
					replies = append(replies, reply)
				}

				if currentTerm, isLeader := kv.rf.GetState(); isLeader && message.CommandTerm == currentTerm {
					if chs, ok := kv.notifyChs[message.CommandIndex]; ok {
						for i, ch := range chs {
							if i < len(replies) {
								ch <- replies[i]
							} else {
								ch <- &CommandReply{Err: ErrTimeout}
							}
						}
					}
				}
				delete(kv.notifyChs, message.CommandIndex)
			default:
				log.Fatalf("Unkown cmd type: %T", cmd)
			}
			kv.mu.Unlock()
		} else {
			log.Fatalf("Invalid ApplyMsg %v", message)
		}
	}
}

func StartKVServer(servers []peer.Peer, me int, logdb *kvdb.KVDB, kvvdb *KVVDB, executeTimeout, batchSize, batchTimeout int) *KVServer {
	gob.Register([]Command{})
	applyCh := make(chan raft.ApplyMsg)

	kv := &KVServer{
		mu:             sync.RWMutex{},
		me:             me,
		rf:             raft.Make(servers, me, logdb, applyCh),
		applyCh:        applyCh,
		stateMachine:   kvvdb,
		notifyChs:      make(map[int][]chan *CommandReply),
		buffer:         make([]bufferedCommand, 0),
		executeTimeout: time.Duration(executeTimeout) * time.Millisecond,
		batchSize:      batchSize,
		batchTimeout:   time.Duration(batchTimeout) * time.Millisecond,
	}

	go kv.applier()
	go kv.periodicBatchSubmit()

	if err := util.RegisterRPCService(kv); err != nil {
		panic(fmt.Sprintf("error when register KVraft rpc service: %v\n", err))
	}
	return kv
}

func (kv *KVServer) periodicBatchSubmit() {
	for {
		time.Sleep(kv.batchTimeout)
		kv.bufferLock.Lock()
		if len(kv.buffer) > 0 {
			batch := make([]bufferedCommand, len(kv.buffer))
			copy(batch, kv.buffer)
			kv.buffer = kv.buffer[:0]
			kv.bufferLock.Unlock()
			kv.submitBatch(batch)
		} else {
			kv.bufferLock.Unlock()
		}
	}
}
