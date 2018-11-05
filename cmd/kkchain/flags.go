package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/invin/kkchain/accounts"
	"github.com/invin/kkchain/accounts/keystore"
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/config"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var (

	// ConfigFlag config file path
	ConfigFlag = cli.StringFlag{
		Name:  "config",
		Usage: "TOML configuration file",
	}

	DataDirFlag = cli.StringFlag{
		Name:  "datadir",
		Usage: "Data directory for the databases and keystore",
		Value: "",
	}

	// GeneralFlags config list
	GeneralFlags = []cli.Flag{
		DataDirFlag,
	}

	// NetworkSeedFlag network seed
	NetworkSeedsFlag = cli.StringSliceFlag{
		Name:  "network.seed",
		Usage: "network seed addresses, multi-value support.",
	}

	// NetworkListenFlag network listen
	NetworkListenFlag = cli.StringFlag{
		Name:  "network.listen",
		Usage: "network listen addresses, multi-value support.",
	}

	// NetworkKeyPathFlag network key
	NetworkKeyPathFlag = cli.StringFlag{
		Name:  "network.nodekey",
		Usage: "network node private key file path",
	}

	NetworkMaxPeersFlag = cli.IntFlag{
		Name:  "network.maxpeers",
		Usage: "Maximum number of network peers",
		Value: 20,
	}

	// NetworkFlags config list
	NetworkFlags = []cli.Flag{
		NetworkSeedsFlag,
		NetworkListenFlag,
		NetworkKeyPathFlag,
		NetworkMaxPeersFlag,
	}

	DhtBucketSizeFlag = cli.IntFlag{
		Name:  "dht.bucketsize",
		Usage: "Bucket size of routine table",
		Value: 16,
	}

	DhtFlags = []cli.Flag{
		DhtBucketSizeFlag,
	}

	ConsensusMineFlag = cli.BoolFlag{
		Name:  "consensus.mine",
		Usage: "Enable mining",
	}

	ConsensusTypeFlag = cli.StringFlag{
		Name:  "consensus.type",
		Usage: "Type of consensus",
		Value: "pow",
	}

	ConsensusCoinbaseFlag = cli.StringFlag{
		Name:  "consensus.coinbase",
		Usage: "Coinbase of consensus",
	}

	ConsensusFlags = []cli.Flag{
		ConsensusMineFlag,
		ConsensusTypeFlag,
		ConsensusCoinbaseFlag,
	}

	RPCEnabledFlag = cli.BoolFlag{
		Name:  "api.rpc",
		Usage: "Enable the HTTP-RPC server",
	}
	RPCModulesFlag = cli.StringSliceFlag{
		Name:  "api.rpcmodules",
		Usage: "Enable the HTTP-RPC service modules",
	}
	RPCListenAddrFlag = cli.StringFlag{
		Name:  "api.rpcaddr",
		Usage: "HTTP-RPC server listening interface",
	}

	APIFlags = []cli.Flag{
		RPCEnabledFlag,
		RPCModulesFlag,
		RPCListenAddrFlag,
	}

	// Account settings
	KeyStoreDirFlag = cli.StringFlag{
		Name:  "acc.keystoredir",
		Usage: "Directory for the keystore (default = inside the datadir)",
		Value: "",
	}

	UnlockedAccountFlag = cli.StringFlag{
		Name:  "acc.unlock",
		Usage: "Comma separated list of accounts to unlock",
		Value: "",
	}
	PasswordFileFlag = cli.StringFlag{
		Name:  "acc.password",
		Usage: "Password file to use for non-interactive password input",
		Value: "",
	}

	AccountFlags = []cli.Flag{
		KeyStoreDirFlag,
		UnlockedAccountFlag,
		PasswordFileFlag,
	}
)

func generalConfig(ctx *cli.Context, cfg *config.GeneralConfig) error {
	if ctx.GlobalIsSet(DataDirFlag.Name) {
		cfg.DataDir = ctx.GlobalString(DataDirFlag.Name)
	}

	if cfg.DataDir != "" {
		absdatadir, err := filepath.Abs(cfg.DataDir)
		if err != nil {
			return err
		}
		cfg.DataDir = absdatadir

		if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
			return err
		}
	}

	return nil
}

func networkConfig(ctx *cli.Context, cfg *config.NetworkConfig) {
	if ctx.GlobalIsSet(NetworkSeedsFlag.Name) {
		cfg.Seeds = ctx.GlobalStringSlice(NetworkSeedsFlag.Name)
	}
	if ctx.GlobalIsSet(NetworkListenFlag.Name) {
		cfg.Listen = ctx.GlobalString(NetworkListenFlag.Name)
	}
	if ctx.GlobalIsSet(NetworkKeyPathFlag.Name) {
		cfg.PrivateKey = ctx.GlobalString(NetworkKeyPathFlag.Name)
	}

	if ctx.GlobalIsSet(NetworkMaxPeersFlag.Name) {
		cfg.MaxPeers = int(ctx.GlobalInt(NetworkMaxPeersFlag.Name))
	}
}

func dhtConfig(ctx *cli.Context, cfg *config.Config) {
	cfg.Dht.RoutingTableDir = filepath.Join(cfg.DataDir, "nodes")

	cfg.Dht.Seeds = cfg.Network.Seeds

}

func consensusConfig(ctx *cli.Context, cfg *config.ConsensusConfig) {
	if ctx.GlobalIsSet(ConsensusMineFlag.Name) {
		cfg.Mine = ctx.GlobalBool(ConsensusMineFlag.Name)
	}

	if ctx.GlobalIsSet(ConsensusTypeFlag.Name) {
		cfg.Type = ctx.GlobalString(ConsensusTypeFlag.Name)
	}

	if ctx.GlobalIsSet(ConsensusCoinbaseFlag.Name) {
		cfg.Coinbase = ctx.GlobalString(ConsensusCoinbaseFlag.Name)
	}

}

func apiConfig(ctx *cli.Context, cfg *config.ApiConfig) {
	if ctx.GlobalIsSet(RPCEnabledFlag.Name) {
		cfg.Rpc = ctx.GlobalBool(RPCEnabledFlag.Name)
	}
	if ctx.GlobalIsSet(RPCModulesFlag.Name) {
		cfg.RpcModules = ctx.GlobalStringSlice(RPCModulesFlag.Name)
	}
	if ctx.GlobalIsSet(RPCListenAddrFlag.Name) {
		cfg.RpcAddr = ctx.GlobalString(RPCListenAddrFlag.Name)
	}
}

func accountConfig(ctx *cli.Context, cfg *config.AccountConfig) {
	if ctx.GlobalIsSet(KeyStoreDirFlag.Name) {
		cfg.KeyStoreDir = ctx.GlobalString(KeyStoreDirFlag.Name)
	}

	if ctx.GlobalIsSet(UnlockedAccountFlag.Name) {
		cfg.Unlock = ctx.GlobalString(UnlockedAccountFlag.Name)
	}

	if ctx.GlobalIsSet(PasswordFileFlag.Name) {
		cfg.Password = ctx.GlobalString(PasswordFileFlag.Name)
	}

}

// MakeAddress converts an account specified directly as a hex encoded string or
// a key index in the key store to an internal account representation.
func MakeAddress(ks *keystore.KeyStore, account string) (accounts.Account, error) {
	// If the specified account is a valid address, return it
	if common.IsHexAddress(account) {
		return accounts.Account{Address: common.HexToAddress(account)}, nil
	}
	// Otherwise try to interpret the account as a keystore index
	index, err := strconv.Atoi(account)
	if err != nil || index < 0 {
		return accounts.Account{}, fmt.Errorf("invalid account address or index %q", account)
	}
	log.Warn("-------------------------------------------------------------------")
	log.Warn("Referring to accounts by order in the keystore folder is dangerous!")
	log.Warn("This functionality is deprecated and will be removed in the future!")
	log.Warn("Please use explicit addresses! (can search via `geth account list`)")
	log.Warn("-------------------------------------------------------------------")

	accs := ks.Accounts()
	if len(accs) <= index {
		return accounts.Account{}, fmt.Errorf("index %d higher than number of accounts %d", index, len(accs))
	}
	return accs[index], nil
}

// MakePasswordList reads password lines from the file specified by the global --password flag.
func MakePasswordList(ctx *cli.Context) []string {
	path := ctx.GlobalString(PasswordFileFlag.Name)
	if path == "" {
		return nil
	}
	text, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read password file: %v", err)
	}
	lines := strings.Split(string(text), "\n")
	// Sanitise DOS line endings.
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	return lines
}

// MigrateFlags sets the global flag from a local flag when it's set.
// This is a temporary function used for migrating old command/flags to the
// new format.
//
// e.g. geth account new --keystore /tmp/mykeystore --lightkdf
//
// is equivalent after calling this method with:
//
// geth --keystore /tmp/mykeystore --lightkdf account new
//
// This allows the use of the existing configuration functionality.
// When all flags are migrated this function can be removed and the existing
// configuration functionality must be changed that is uses local flags
func MigrateFlags(action func(ctx *cli.Context) error) func(*cli.Context) error {
	return func(ctx *cli.Context) error {
		for _, name := range ctx.FlagNames() {
			if ctx.IsSet(name) {
				ctx.GlobalSet(name, ctx.String(name))
			}
		}
		return action(ctx)
	}
}
