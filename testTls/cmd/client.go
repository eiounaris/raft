package main

import (
	"fmt"
	"test/peer"
	"test/tlsRpc"
)

func main() {
	tlsConfig, err := tlsRpc.InitClientTlsConfig("pem/peer1.cert", "pem/peer1.key", "pem/ca.cert")
	if err != nil {
		panic(err)
	}
	peer := peer.Peer{
		Id:        0,
		Ip:        "192.168.128.100",
		Port:      9000,
		San:       "peer0.example.com",
		TlsConfig: tlsConfig,
	}
	reply := new(string)
	if err := peer.TlsRpcCall("Service", "Hello", "zhangsan", reply); err != nil {
		panic(err)
	}
	fmt.Println(*reply)
}
