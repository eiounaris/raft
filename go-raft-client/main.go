package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-raft-client/kvraft"
	"go-raft-client/peer"
	"go-raft-client/util"
)

func main() {

	// 加载.env文件环境变量
	envFiles := []string{".env"}
	env, err := util.LoadEnv(envFiles)
	if err != nil {
		panic(err)
	}
	persistentConfigPath := env.PersistentConfigPath

	// 加载节点配置信息
	peers, err := peer.LoadPeers(env.PeersPath)
	if err != nil {
		panic(err)
	}

	// 加载系统配置信息
	persistentConfig, err := util.LoadPersistentConfig(persistentConfigPath)
	if err != nil {
		panic(err)
	}

	// 初始化 TlsConfig
	tlsConfig, err := util.InitClientTlsConfig(persistentConfig.CertFile, persistentConfig.KeyFile, persistentConfig.CaFile)
	if err != nil {
		panic(err)
	}
	for i := range peers {
		peers[i].TlsConfig = tlsConfig
	}

	// 启动节点 Clerk 服务
	ck := kvraft.MakeClerk(peers)

	// 启动命令行程序
	fmt.Println("1. get <key>                         - 查询键值")
	fmt.Println("2. set <key> <value> <version>       - 设置键值")
	fmt.Println("2. delete <key> <version>            - 删除键值")
	fmt.Println("3. test <subCmd>                     - 测试 TPS")
	fmt.Println("4. exit                              - 退出程序")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" {
			fmt.Println("退出程序...")
			return
		}
		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "get":
			if len(parts) != 2 {
				fmt.Println("参数错误，使用方式: get <key>")
				continue
			}
			key := parts[1]
			fmt.Println("等待 get 命令执行")
			reply := ck.Get([]byte(key))
			if reply.Err == kvraft.ErrNoKey {
				fmt.Printf("查询结果: %v\n", reply.Err)
			} else {
				fmt.Printf("查询结果: %s, %v\n", reply.Value, reply.Version)
			}

		case "set":
			if len(parts) != 4 {
				fmt.Println("参数错误，使用方式: set <key> <value> <version>")
				continue
			}
			key := parts[1]
			value := parts[2]
			version, err := strconv.Atoi(parts[3])
			if err != nil || version < 1 {
				fmt.Println("参数错误：version 字段为大于 0 的数字")
			}
			fmt.Println("等待 set 命令执行 ")
			reply := ck.Set([]byte(key), []byte(value), version)
			fmt.Printf("执行结果: %v\n", reply.Err)

		case "delete":
			if len(parts) != 3 {
				fmt.Println("参数错误，使用方式: delete <key> <version>")
				continue
			}
			key := parts[1]
			version, err := strconv.Atoi(parts[2])
			if err != nil || version < 1 {
				fmt.Println("参数错误：version 字段为大于 0 的数字")
			}
			fmt.Println("等待 put 命令执行")
			reply := ck.Delete([]byte(key), version)
			fmt.Printf("执行结果: %v\n", reply.Err)

		case "test":
			if len(parts) < 2 {
				fmt.Println("参数错误：参数错误，使用方式: test <subCmd>")
				continue
			}
			switch parts[1] {
			case "get":
				{
					key := parts[2]
					clients := 200
					wg := new(sync.WaitGroup)
					tBegin := time.Now()
					requestNumsPerClient := 100
					for i := range clients {
						wg.Add(1)
						go func() {
							defer wg.Done()
							for j := range requestNumsPerClient {
								fmt.Printf("等待用户 {%v} 第 {%v} 次 get 命令执行\n", i, j)
								reply := ck.Get([]byte(key))
								if reply.Err == kvraft.ErrNoKey {
									fmt.Printf("用户 {%v} 第 {%v} 次 get 命令查询结果: %v\n", i, j, reply.Err)
								} else {
									fmt.Printf("用户 {%v} 第 {%v} 次 get 命令查询结果: %s, %v\n", i, j, reply.Value, reply.Version)
								}
							}
						}()
					}
					wg.Wait()
					tEnd := time.Now()
					fmt.Printf("TPS: %v\n", float64(clients*requestNumsPerClient)/tEnd.Sub(tBegin).Seconds())
				}
			default:
				{
					fmt.Println("暂不支持其他测试")
				}
			}

		default:
			fmt.Println("未知命令，支持命令格式如下:")
			fmt.Println("1. get <key>                         - 查询键值")
			fmt.Println("2. set <key> <value> <version>       - 设置键值")
			fmt.Println("2. delete <key> <version>            - 删除键值")
			fmt.Println("3. test <subCmd>                     - 测试 TPS")
			fmt.Println("4. exit                              - 退出程序")
		}
	}
}
