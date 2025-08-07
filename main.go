package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/dogewalker/core"
	"github.com/dogeorg/dogewalker/walker"
	"github.com/dogeorg/governor"
	"github.com/dogeorg/indexer/api"
	"github.com/dogeorg/indexer/index"
	"github.com/dogeorg/indexer/store"
)

const RETRY_DELAY = 5 * time.Second
const MaxRollbackDepth = 1440 // 24 hours of blocks

type Config struct {
	connStr string
	rpcHost string
	rpcPort int
	rpcUser string
	rpcPass string
	zmqHost string
	zmqPort int
	bindAPI string
}

func main() {
	log.Printf("\n\n[Indexer] starting")

	config := Config{
		connStr: "index.db", // "postgres://localhost/index?sslmode=disable",
		rpcHost: "127.0.0.1",
		rpcPort: 22555,
		rpcUser: "dogecoin",
		rpcPass: "dogecoin",
		zmqHost: "127.0.0.1",
		zmqPort: 28332,
		bindAPI: "localhost:8888",
	}
	chain := &doge.DogeMainNetChain

	webPort := os.Getenv("PORT")
	if webPort == "" {
		webPort = "8000"
	}
	listenPort := os.Getenv("LISTEN")
	if listenPort == "" {
		listenPort = "8001"
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
		fromHash, err = blockchain.GetBlockHash(0, gov.GlobalContext())
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
	gov.Add("Index", index.NewIndexer(db, blocks, MaxRollbackDepth))

	// REST API.
	gov.Add("API", api.New(config.bindAPI, db))

	// run services until interrupted.
	gov.Start().WaitForShutdown()
	fmt.Println("[Indexer] stopped")
}
