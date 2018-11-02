package main

import (
	"flag"
	"fmt"
	"time"

	"encoding/hex"

	"github.com/invin/kkchain/config"
	"github.com/invin/kkchain/crypto/ed25519"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/p2p/impl"

	log "github.com/sirupsen/logrus"
)

// init config and loggingLoger
func init() {
	//config.Init("")
	// mainLogger.Debugf("debug %s", "secret")
	// mainLogger.Info("info")
	// mainLogger.Notice("notice")
	// mainLogger.Warning("warning")
	// mainLogger.Error("err")
	// mainLogger.Critical("crit")
}

func main() {
	port := flag.String("p", "9998", "")
	sec := flag.Int("s", 120, "")
	keypath := flag.String("k", "", "")
	flag.Parse()

	seconds := time.Duration(*sec)

	listen := "/ip4/127.0.0.1/tcp/" + *port
	config.DefaultNetworkConfig.PrivateKey = *keypath
	config.DefaultNetworkConfig.Listen = listen

	if *port != "9998" {
		remoteKeyPath := "node1.key"
		pri, _ := p2p.LoadNodeKeyFromFile(remoteKeyPath)
		pub, _ := ed25519.New().PrivateToPublic(pri)
		node := "/ip4/127.0.0.1/tcp/9998"
		node = hex.EncodeToString(pub) + "@" + node
		log.Infof("remote peer: %s", node)
		config.DefaultNetworkConfig.Seeds = []string{node}
		config.DefaultDhtConfig.Seeds = []string{node}
	}

	net := impl.NewNetwork(&config.DefaultNetworkConfig, &config.DefaultDhtConfig, nil)
	err := net.Start()
	if err != nil {
		fmt.Printf("failed to start server: %s\n", err)
	}

	time.Sleep(time.Second * seconds)
}
