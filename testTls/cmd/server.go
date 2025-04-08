package main

import (
	"fmt"
	"net/rpc"
	"test/tlsRpc"
)

type Service struct{}

func (s *Service) Hello(arg string, reply *string) error {
	*reply = fmt.Sprintf("Hello, %s", arg)
	return nil
}

func main() {
	if err := rpc.Register(new(Service)); err != nil {
		panic(err)
	}
	tlsConfig, err := tlsRpc.InitServerTlsConfig("pem/peer0.cert", "pem/peer0.key", "pem/ca.cert")
	if err != nil {
		panic(err)
	}
	_, err = tlsRpc.StartTlsRpcServer(tlsConfig, ":9000")
	if err != nil {
		panic(err)
	}
	select {}
}
