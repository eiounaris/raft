package util

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
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
	ElectionTimeout  int
	HeartbeatTimeout int
	ExecuteTimeout   int
	BatchSize        int
	BatchTimeout     int
}

func LoadPersistentConfig(filepath string) (*PersistentConfig, error) {
	var persistentConfig *PersistentConfig
	persistentConfigJsonBytes, err := os.ReadFile(filepath)
	if err != nil {
		return persistentConfig, err
	}
	if err = json.Unmarshal(persistentConfigJsonBytes, persistentConfig); err != nil {
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

// === StartRPCServer

func StartRPCServer(address string) (net.Listener, error) {
	rpc.HandleHTTP()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	go http.Serve(listener, nil)
	return listener, nil
}
