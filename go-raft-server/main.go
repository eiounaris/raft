package main

import (
	"bufio"
	"encoding/gob"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"go-raft-server/kvdb"
	"go-raft-server/kvraft"
	"go-raft-server/peer"
	"go-raft-server/raft"
	"go-raft-server/util"
)

func main() {

	// 加载 .env 文件环境变量
	envFiles := []string{".env"}
	env, err := util.LoadEnv(envFiles)
	if err != nil {
		panic(err)
	}
	envMe := env.Me
	util.Debug = env.Debug
	peersPath := env.PeersPath
	persistentConfigPath := env.PersistentConfigPath

	// 加载节点配置信息
	peers, err := peer.LoadPeers(peersPath)
	if err != nil {
		panic(err)
	}

	// 加载系统配置信息
	persistentConfig, err := util.LoadPersistentConfig(persistentConfigPath)
	if err != nil {
		panic(err)
	}
	raft.ElectionTimeout = persistentConfig.ElectionTimeout
	raft.HeartbeatTimeout = persistentConfig.HeartbeatTimeout

	// 使用 -me flag 重置环境变量 me
	flagMe := flag.Int("me", envMe, "peer id")
	flag.Parse()
	me := *flagMe

	// 启动节点 Raft logdb 数据库
	logdb, err := kvdb.MakeKVDB(fmt.Sprintf("data/logdb%v", me))
	if err != nil {
		panic(err)
	}

	// 创建节点 Raft ApplyMsg 通道
	applyCh := make(chan raft.ApplyMsg)
	go func() {
		for msg := range applyCh {
			log.Printf("receives Raft ApplyMsg.Index (%v)\n", msg.CommandIndex)
		}
	}()

	// 注册 Command transferred
	gob.Register([]kvraft.Command{})

	// 初始化 TlsConfig
	tlsConfig, err := util.InitClientTlsConfig(persistentConfig.CertFile, persistentConfig.KeyFile, persistentConfig.CaFile)
	if err != nil {
		panic(err)
	}
	for i := range peers {
		peers[i].TlsConfig = tlsConfig
	}

	// 启动节点 Raft
	service := raft.Make(peers, me, logdb, applyCh)

	// 启动 rpc 服务
	if _, err := util.StartTlsRpcServer(tlsConfig, fmt.Sprintf("%v:%v", peers[me].Ip, peers[me].Port)); err != nil {
		panic(err)
	}
	// if _, err = util.StartRPCServer(fmt.Sprintf(":%v", peers[me].Port)); err != nil {
	// 	panic(fmt.Sprintf("error when start rpc service: %v\n", err))
	// }

	// 打印节点启动日志
	log.Printf("peer Raft service started, lisening addr: %v:%v\n", peers[me].Ip, peers[me].Port)

	// 启动命令行程序
	// 1. 创建通道
	inputCh := make(chan string)

	// 2. 启动协程读取输入
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() { // 循环读取每一行
			input := scanner.Text()
			if input == "" {
				continue
			}
			if input == "exit" { // 输入 exit 时退出
				inputCh <- input
				return
			}

			if input == "test command" { // 输入 test command 测试共识命令切片时的 TPS
				blockOfCommands := make([]kvraft.Command, 100)
				for index := range blockOfCommands {
					blockOfCommands[index] = kvraft.Command{
						CommandArgs: &kvraft.CommandArgs{
							Key:     []byte("testKey"),
							Value:   []byte("testValue"),
							Version: 0,
							Op:      kvraft.OpGet},
					}
				}
				clients := 10
				requestNums := 100
				wg := new(sync.WaitGroup)
				tBegin := time.Now()
				for range clients {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for range requestNums {
							// 调用 raft 服务
							service.Start(blockOfCommands)
							time.Sleep(20 * time.Millisecond)
						}
					}()
				}
				wg.Wait()
				tEnd := time.Now()
				fmt.Printf("consensus content is : %v\n", blockOfCommands)
				fmt.Printf("TPS: %v\n", (float64(clients*len(blockOfCommands)*requestNums))/(tEnd.Sub(tBegin).Seconds()))
				continue
			}

			if input == "test string" { // 输入 test string 测试共识字符串时的 TPS
				blockOfString := make([]string, 100)
				for index := range blockOfString {
					blockOfString[index] = "testString"
				}
				clients := 10
				requestNums := 100
				wg := new(sync.WaitGroup)
				tBegin := time.Now()
				for range clients {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for range requestNums {
							// 调用 raft 服务
							service.Start(blockOfString)
							time.Sleep(20 * time.Millisecond)
						}
					}()
				}
				wg.Wait()
				tEnd := time.Now()
				fmt.Printf("consensus content is : %v\n", blockOfString)
				fmt.Printf("TPS: %v\n", (float64(clients*len(blockOfString)*requestNums))/(tEnd.Sub(tBegin).Seconds()))
				continue
			}

			// 调用 raft 服务
			service.Start(input)
		}
	}()

	// 3. 主线程处理输入
	for input := range inputCh {
		if input == "exit" {
			fmt.Println("exit...")
			return
		}
	}
}

// === Raft
// ===
// === KVRaft
/*
package main

import (
	"flag"
	"fmt"
	"log"

	"go-raft-server/kvdb"
	"go-raft-server/kvraft"
	"go-raft-server/peer"
	"go-raft-server/raft"
	"go-raft-server/util"
)

func main() {

	// 加载 .env 文件环境变量
	envFiles := []string{".env"}
	env, err := util.LoadEnv(envFiles)
	if err != nil {
		panic(err)
	}
	envMe := env.Me
	util.Debug = env.Debug
	peersPath := env.PeersPath
	persistentConfigPath := env.PersistentConfigPath

	// 加载节点配置信息
	peers, err := peer.LoadPeers(peersPath)
	if err != nil {
		panic(err)
	}

	// 加载系统配置信息
	persistentConfig, err := util.LoadPersistentConfig(persistentConfigPath)
	if err != nil {
		panic(err)
	}
	raft.ElectionTimeout = persistentConfig.ElectionTimeout
	raft.HeartbeatTimeout = persistentConfig.HeartbeatTimeout

	// 使用 -me flag 重置环境变量 me
	flagMe := flag.Int("me", envMe, "peer id")
	flag.Parse()
	me := *flagMe

	// 加载持久存储 服务
	logdb, err := kvdb.MakeKVDB(fmt.Sprintf("data/logdb%v", me))
	if err != nil {
		panic(err)
	}
	kvvdb, err := kvraft.MakeKVVDB(fmt.Sprintf("data/kvvdb%v", me))
	if err != nil {
		panic(err)
	}

	kvraft.StartKVServer(peers, me, logdb, kvvdb, persistentConfig.ElectionTimeout, persistentConfig.BatchSize, persistentConfig.BatchTimeout)

	// 启动 rpc 服务
	if _, err = util.StartRPCServer(fmt.Sprintf("%v:%v", peers[me].Ip, peers[me].Port)); err != nil {
		panic(err)
	}

	// 打印节点启动日志
	log.Printf("peer Raft and KVRaft service started, lisening addr: %v:%v\n", peers[me].Ip, peers[me].Port)
	select {}
}
*/
