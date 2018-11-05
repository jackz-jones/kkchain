package main

import (
	"fmt"
	"os"
	"os/signal"
	"sort"

	//"strconv"
	"syscall"
	//"time"

	"github.com/invin/kkchain/config"
	"github.com/invin/kkchain/node"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var (
	version   string
	commit    string
	branch    string
	compileAt string
)

func init() {
	// // 设置日志格式
	// log.SetFormatter(&log.JSONFormatter{})
	// // 设置输出
	// log.SetOutput(os.Stdout)
	// 设置日志级别
	//log.SetLevel(log.DebugLevel)
}

func main() {
	app := cli.NewApp()
	app.Action = run
	app.Name = "kkchain"
	//app.Version = fmt.Sprintf("%s, branch %s, commit %s", version, branch, commit)
	//timestamp, _ := strconv.ParseInt(compileAt, 10, 64)
	//app.Compiled = time.Unix(timestamp, 0)
	app.Usage = "the kkchain command line interface"
	app.Copyright = "Copyright 2018-2019 The kkchain Authors"

	app.Flags = append(app.Flags, ConfigFlag)
	app.Flags = append(app.Flags, GeneralFlags...)
	app.Flags = append(app.Flags, NetworkFlags...)
	app.Flags = append(app.Flags, DhtFlags...)
	app.Flags = append(app.Flags, ConsensusFlags...)
	app.Flags = append(app.Flags, APIFlags...)
	app.Flags = append(app.Flags, AccountFlags...)

	sort.Sort(cli.FlagsByName(app.Flags))

	app.Commands = []cli.Command{
		accountCommand,
	}

	sort.Sort(cli.CommandsByName(app.Commands))

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx *cli.Context) error {

	cfg := makeConfig(ctx)

	n, err := makeNode(cfg)
	if err != nil {
		return err
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	quitCh := make(chan bool, 1)

	n.Start()

	go func() {
		<-c

		n.Stop()
		log.Error("exit")

		quitCh <- true
		return
	}()

	select {
	case <-quitCh:
		return nil
	}

}

func makeConfig(ctx *cli.Context) *config.Config {
	// Load defaults.
	cfg := config.Config{
		GeneralConfig: config.DefaultGeneralConfig,
		Network:       &config.DefaultNetworkConfig,
		Dht:           &config.DefaultDhtConfig,
		Consensus:     &config.DefaultConsensusConfig,
		Api:           &config.DefaultAPIConfig,
		Account:       &config.AccountConfig{},
	}

	// Load config file.
	if file := ctx.GlobalString(ConfigFlag.Name); file != "" {
		if err := config.LoadConfig(file, &cfg); err != nil {
			log.Fatalf("%v", err)
		}
	}

	// load config from cli args
	if err := generalConfig(ctx, &cfg.GeneralConfig); err != nil {
		log.Fatalf("%v", err)
	}

	networkConfig(ctx, cfg.Network)
	dhtConfig(ctx, &cfg)
	consensusConfig(ctx, cfg.Consensus)
	apiConfig(ctx, cfg.Api)
	accountConfig(ctx, cfg.Account)

	return &cfg
}

func makeNode(cfg *config.Config) (*node.Node, error) {

	node, err := node.New(cfg)

	return node, err
}
