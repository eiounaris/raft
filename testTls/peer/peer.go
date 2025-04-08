package peer

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/rpc"
	"os"
)

// === Peer

type Peer struct {
	Id        int    `json:"id"`
	Ip        string `json:"ip"`
	Port      int    `json:"port"`
	San       string `json:"san"`
	TlsConfig *tls.Config
}

func LoadPeers(filepath string) ([]Peer, error) {
	var peers []Peer
	peersBytes, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(peersBytes, &peers); err != nil {
		return nil, err
	}
	return peers, nil
}

func (peer *Peer) Call(svc string, svcMeth string, args any, reply any) error {
	client, err := rpc.Dial("tcp", fmt.Sprintf("%v:%v", peer.Ip, peer.Port))
	if err != nil {
		return err
	}
	return client.Call(svc+"."+svcMeth, args, reply)
}

func (peer *Peer) TlsRpcCall(svc string, svcMeth string, args any, reply any) error {
	if peer.TlsConfig == nil {
		panic(fmt.Sprintf("peer {%v}'s tlsCOnfig == nil\n", peer.Id))
	}
	// 建立 TLS 连接
	conn, err := tls.Dial("tcp", fmt.Sprintf("%v:%v", peer.San, peer.Port), peer.TlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	// 创建 RPC 客户端
	client := rpc.NewClient(conn)
	defer client.Close()

	return client.Call(svc+"."+svcMeth, args, reply)
}
