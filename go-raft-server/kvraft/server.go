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
	ch := make(chan *CommandReply, 1)
	kv.bufferLock.Lock()
	kv.bufferedCommand = append(kv.bufferedCommand, Command{args})
	kv.bufferedChs = append(kv.bufferedChs, ch)
	if len(kv.bufferedCommand) >= kv.batchSize {
		// batchCommand := make([]Command, len(kv.bufferedCommand))
		// batchChs := make([]chan *CommandReply, len(kv.bufferedChs))
		// copy(batchCommand, kv.bufferedCommand)
		// copy(batchChs, kv.bufferedChs)
		batchCommand := kv.bufferedCommand
		batchChs := kv.bufferedChs
		kv.bufferedCommand = kv.bufferedCommand[:0]
		kv.bufferedChs = kv.bufferedChs[:0]
		kv.bufferLock.Unlock()
		go kv.submitBatch(batchCommand, batchChs)
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

func (kv *KVServer) submitBatch(batchCommand []Command, batchChs []chan *CommandReply) {
	if len(batchCommand) == 0 {
		return
	}

	index, _, isLeader := kv.rf.Start(batchCommand)
	if !isLeader {
		for _, ch := range batchChs {
			ch <- &CommandReply{Err: ErrWrongLeader}
		}
		return
	}

	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.notifyChs[index] = batchChs
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
							ch <- replies[i]
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
			batchChs := kv.bufferedChs
			kv.bufferedCommand = kv.bufferedCommand[:0]
			kv.bufferedChs = kv.bufferedChs[:0]
			kv.bufferLock.Unlock()
			go kv.submitBatch(batchCommand, batchChs)
		} else {
			kv.bufferLock.Unlock()
		}
	}
}
