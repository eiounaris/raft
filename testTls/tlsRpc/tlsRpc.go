package tlsRpc

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
)

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
