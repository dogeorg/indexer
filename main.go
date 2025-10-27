package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/dogewalker/core"
	"github.com/dogeorg/dogewalker/walker"
	"github.com/dogeorg/governor"
	"github.com/dogeorg/indexer/index"
	"github.com/dogeorg/indexer/store"
	"github.com/dogeorg/indexer/web"
)

const RETRY_DELAY = 5 * time.Second
const MaxRollbackDepth = 1440 // 24 hours of blocks

type Config struct {
	connStr        string
	rpcHost        string
	rpcPort        int
	rpcUser        string
	rpcPass        string
	zmqHost        string
	zmqPort        int
	bindAPI        string
	corsOrigin     string
	chainName      string
	startingHeight int64
}

func main() {
	log.Printf("\n\n[Indexer] starting")

	var config Config
	flag.StringVar(&config.connStr, "dburl", getEnv("DB_URL", "index.db"), "Database connection string")
	flag.StringVar(&config.rpcHost, "rpchost", getEnv("RPC_HOST", "127.0.0.1"), "RPC host")
	flag.IntVar(&config.rpcPort, "rpcport", getEnvInt("RPC_PORT", 22555), "RPC port")
	flag.StringVar(&config.rpcUser, "rpcuser", getEnv("RPC_USER", "dogecoin"), "RPC username")
	flag.StringVar(&config.rpcPass, "rpcpass", getEnv("RPC_PASS", "dogecoin"), "RPC password")
	flag.StringVar(&config.zmqHost, "zmqhost", getEnv("ZMQ_HOST", "127.0.0.1"), "ZMQ host")
	flag.IntVar(&config.zmqPort, "zmqport", getEnvInt("ZMQ_PORT", 28332), "ZMQ port")
	flag.StringVar(&config.bindAPI, "bindapi", getEnv("BIND_API", "localhost:8000"), "API bind address")
	flag.StringVar(&config.corsOrigin, "cors-origin", getEnv("CORS_ORIGIN", "http://localhost:5173"), "CORS allowed origin")
	flag.StringVar(&config.chainName, "chain", getEnv("CHAIN", "mainnet"), "Chain Params (mainnet, testnet, regtest)")
	flag.Int64Var(&config.startingHeight, "startingheight", getEnvInt64("STARTING_HEIGHT", 5830000), "Starting Height")

	flag.Parse()

	var chain *doge.ChainParams
	switch config.chainName {
	case "mainnet":
		chain = &doge.DogeMainNetChain
	case "testnet":
		chain = &doge.DogeTestNetChain
	case "regtest":
		chain = &doge.DogeRegTestChain
	default:
		panic(errors.New("Unexpected chain: " + config.chainName))
	}

	gov := governor.New().CatchSignals().Restart(1 * time.Second)

	// create database store
	db, err := store.NewIndexStore(config.connStr, gov.GlobalContext())
	if err != nil {
		log.Fatalf("[Indexer] database init: %v", err)
	}

	// Core Node blockchain access.
	blockchain := core.NewCoreRPCClient(config.rpcHost, config.rpcPort, config.rpcUser, config.rpcPass)

	// TipChaser
	zmqAddr := fmt.Sprintf("tcp://%v:%v", config.zmqHost, config.zmqPort)
	zmqSvc, tipChanged := core.NewTipChaser(zmqAddr)
	gov.Add("ZMQ", zmqSvc)

	// Get the resume-point.
	var fromBlock []byte
	var fromHash string
	for !gov.Stopping() {
		fromBlock, err = db.GetResumePoint()
		if err == nil {
			break
		}
		log.Printf("[Indexer] get chainstate (will retry): %v", err)
		gov.Sleep(RETRY_DELAY)
	}
	if len(fromBlock) > 0 {
		fromHash = doge.HexEncode(fromBlock)
	} else {
		// Start from the Genesis Block.
		fromHash, err = blockchain.GetBlockHash(config.startingHeight, gov.GlobalContext())
		if err != nil {
			log.Printf("[Indexer] get genesis block hash: %v", err)
			return
		}
	}

	// Walk the Doge.
	walkSvc, blocks := walker.WalkTheDoge(walker.WalkerOptions{
		Chain:              chain,
		LastProcessedBlock: fromHash,
		Client:             blockchain,
		TipChanged:         tipChanged,
	})
	gov.Add("Walk", walkSvc)

	// Index the chain.
	indexer := index.NewIndexer(db, blocks, MaxRollbackDepth)
	gov.Add("Index", indexer)

	// REST API.
	gov.Add("API", web.New(config.bindAPI, db, indexer, config.corsOrigin))

	// run services until interrupted.
	gov.Start().WaitForShutdown()
	fmt.Println("[Indexer] stopped")
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
