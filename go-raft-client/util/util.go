package util

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// === Debugging

var Debug = false

func DPrintf(format string, a ...any) {
	if Debug {
		log.Printf(format, a...)
	}
}

// === Env

type Env struct {
	Me                   int
	Debug                bool
	PeersPath            string
	PersistentConfigPath string
}

func LoadEnv(envFiles []string) (*Env, error) {
	if err := godotenv.Load(envFiles...); err != nil {
		return nil, err
	}

	meString, ok := os.LookupEnv("me")
	if !ok {
		return nil, errors.New("there is no env of \"me\"")
	}
	me, err := strconv.Atoi(meString)
	if err != nil {
		return nil, err
	}

	debugString, ok := os.LookupEnv("debug")
	if !ok {
		return nil, errors.New("there is no env of \"debug\"")
	}
	debug, err := strconv.ParseBool(debugString)
	if err != nil {
		return nil, err
	}

	peersPath, ok := os.LookupEnv("peersPath")
	if !ok {
		return nil, errors.New("there is no env of \"peersInfoFilePath\"")
	}

	persistentConfigPath, ok := os.LookupEnv("persistentConfigPath")
	if !ok {
		return nil, errors.New("there is no env of \"persistentConfigPath\"")
	}

	return &Env{Me: me, Debug: debug, PeersPath: peersPath, PersistentConfigPath: persistentConfigPath}, nil
}

// === PersistentConfig

type PersistentConfig struct {
	ElectionTimeout      int    `json:"electionTimeout"`
	HeartbeatTimeout     int    `json:"heartbeatTimeout"`
	ExecuteTimeout       int    `json:"executeTimeout"`
	BatchSize            int    `json:"electionbatchSizeTimeout"`
	BatchTimeout         int    `json:"batchTimeout"`
	ClinetConnectTimeout int    `json:"clinetConnectTimeout"`
	CertFile             string `json:"certFile"`
	KeyFile              string `json:"keyFile"`
	CaFile               string `json:"caFile"`
}

func LoadPersistentConfig(filepath string) (*PersistentConfig, error) {
	var persistentConfig *PersistentConfig
	persistentConfigJsonBytes, err := os.ReadFile(filepath)
	if err != nil {
		return persistentConfig, err
	}
	if err = json.Unmarshal(persistentConfigJsonBytes, &persistentConfig); err != nil {
		return nil, err
	}
	return persistentConfig, nil
}

// === RegisterRPCService

func RegisterRPCService(service any) error {
	if err := rpc.Register(service); err != nil {
		return err
	}
	return nil
}

// === InitClientTlsConfig

func InitClientTlsConfig(clientCertFile string, clientKeyFile string, caCertFile string) (*tls.Config, error) {
	// 加载客户端证书和私钥
	clientCert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	if err != nil {
		return nil, err
	}

	// 加载 CA 证书（验证服务端用）
	caCert, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, err
	}
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	// 配置 TLS（双向认证）
	tlsConfig := &tls.Config{
		RootCAs: certPool,
		// 若需双向认证，添加客户端证书：
		Certificates: []tls.Certificate{clientCert},
		// 跳过证书的主机名验证（仅限测试）
		// InsecureSkipVerify: true,
	}
	return tlsConfig, nil
}

// === InitServerTlsConfig

func InitServerTlsConfig(ServerCertFile string, ServerKeyFile string, caCertFile string) (*tls.Config, error) {
	// 加载服务端证书和私钥
	ServerCert, err := tls.LoadX509KeyPair(ServerCertFile, ServerKeyFile)
	if err != nil {
		return nil, err
	}

	// 加载 CA 证书（验证客户端用）
	caCert, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, err
	}
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	// 配置 TLS（双向认证）
	tlsConfig := &tls.Config{
		ClientCAs:    certPool,
		Certificates: []tls.Certificate{ServerCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	return tlsConfig, nil
}

// === StartTlsRpcServer

func StartTlsRpcServer(tlsConfig *tls.Config, address string) (net.Listener, error) {
	// 创建 TLS 监听器
	// 服务端监听地址需与证书 SAN 中的 IP 匹配
	listener, err := tls.Listen("tcp", address, tlsConfig)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Println("Accept error: ", err)
				continue
			}
			go rpc.ServeConn(conn)
		}
	}()

	return listener, nil
}
