package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/dogewalker/core"
	"github.com/dogeorg/dogewalker/walker"
	"github.com/dogeorg/governor"
	"github.com/dogeorg/indexer/index"
	"github.com/dogeorg/indexer/store"
)

type Config struct {
	rpcHost string
	rpcPort int
	rpcUser string
	rpcPass string
	zmqHost string
	zmqPort int
}

func main() {
	config := Config{
		rpcHost: "127.0.0.1",
		rpcPort: 22555,
		rpcUser: "dogecoin",
		rpcPass: "dogecoin",
		zmqHost: "127.0.0.1",
		zmqPort: 28332,
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
	db, err := store.NewPGStore("index.db", context.Background())
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}

	// Core Node blockchain access.
	blockchain := core.NewCoreRPCClient(config.rpcHost, config.rpcPort, config.rpcUser, config.rpcPass)

	// TipChaser
	zmqAddr := fmt.Sprintf("tcp://%v:%v", config.zmqHost, config.zmqPort)
	zmqSvc, tipChanged := core.NewTipChaser(zmqAddr)
	gov.Add("ZMQ", zmqSvc)

	// Get starting hash.
	fromBlock := db.GetChainPos()

	// Walk the Doge.
	walkSvc, blocks := walker.WalkTheDoge(walker.WalkerOptions{
		Chain:           chain,
		ResumeFromBlock: fromBlock,
		Client:          blockchain,
		TipChanged:      tipChanged,
	})
	gov.Add("Walk", walkSvc)

	// Index the chain.
	gov.Add("Index", index.NewIndexer(db, blocks))

	// run services until interrupted.
	gov.Start()
	gov.WaitForShutdown()
	fmt.Println("finished.")
}
