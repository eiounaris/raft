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

type KVServer struct {
	mu           sync.RWMutex
	rf           *raft.Raft
	applyCh      chan raft.ApplyMsg
	stateMachine *KVVDB
	notifyChs    map[int][]chan *CommandReply

	bufferLock      sync.Mutex
	bufferedCommand []Command
	bufferedChs     []chan *CommandReply

	executeTimeout time.Duration
	batchSize      int
	batchTimeout   time.Duration
}

func (kv *KVServer) ExecuteCommand(args *CommandArgs, reply *CommandReply) error {
	if _, isLeader := kv.rf.GetState(); !isLeader {
		reply.Err = ErrWrongLeader
		return nil
	}
	kv.bufferLock.Lock()
	kv.bufferedCommand = append(kv.bufferedCommand, Command{args})
	if len(kv.bufferedCommand) >= kv.batchSize {
		batchCommand := kv.bufferedCommand
		kv.bufferedCommand = kv.bufferedCommand[:0]
		kv.bufferLock.Unlock()
		go kv.submitBatch(batchCommand)
	} else {
		kv.bufferLock.Unlock()
	}

	reply.Err = Ok
	return nil
}

func (kv *KVServer) submitBatch(batchCommand []Command) {
	if len(batchCommand) == 0 {
		return
	}

	kv.rf.Start(batchCommand)
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
				for _, c := range cmd {
					kv.applyLogToStateMachine(c)
				}
				kv.mu.Unlock()
			default:
				kv.mu.Unlock()
				log.Fatalf("Unknown cmd type: %T", cmd)
			}
		} else {
			log.Fatalf("Invalid ApplyMsg %v", message)
		}
	}
}

func StartKVServer(servers []peer.Peer, me int, logdb *kvdb.KVDB, kvvdb *KVVDB, executeTimeout, batchSize, batchTimeout int) *KVServer {
	gob.Register([]Command{})
	applyCh := make(chan raft.ApplyMsg)

	kv := &KVServer{
		mu:              sync.RWMutex{},
		rf:              raft.Make(servers, me, logdb, applyCh),
		applyCh:         applyCh,
		stateMachine:    kvvdb,
		notifyChs:       make(map[int][]chan *CommandReply),
		bufferedCommand: make([]Command, 0),
		bufferedChs:     make([]chan *CommandReply, 0),
		executeTimeout:  time.Duration(executeTimeout) * time.Millisecond,
		batchSize:       batchSize,
		batchTimeout:    time.Duration(batchTimeout) * time.Millisecond,
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
		if len(kv.bufferedCommand) > 0 {
			batchCommand := kv.bufferedCommand
			kv.bufferedCommand = kv.bufferedCommand[:0]
			kv.bufferLock.Unlock()
			go kv.submitBatch(batchCommand)
		} else {
			kv.bufferLock.Unlock()
		}
	}
}
