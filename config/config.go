package config

import (
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/invin/kkchain/accounts"
	"github.com/invin/kkchain/accounts/keystore"
	"github.com/invin/kkchain/storage"
	"github.com/invin/kkchain/storage/memdb"
	"github.com/invin/kkchain/storage/rocksdb"

	"github.com/spf13/viper"
)

var (
	DefaultGeneralConfig = GeneralConfig{
		DataDir: DefaultDataDir(),
	}

	seeds                = []string{"89b8bb2b66a41220a9b8ba8f019c291dc69c8d9b1ee023813f9db8f8bdcd1f76@/ip4/127.0.0.1/tcp/9998"}
	DefaultNetworkConfig = NetworkConfig{
		Listen:     "/ip4/127.0.0.1/tcp/9998",
		PrivateKey: "node1.key",
		NetworkId:  1,
		MaxPeers:   20,
		Seeds:      seeds,
	}

	DefaultDhtConfig = DhtConfig{
		BucketSize: 16,
	}

	DefaultConsensusConfig = ConsensusConfig{
		Mine: false,
		Type: "pow",
	}

	DefaultAPIConfig = ApiConfig{
		Rpc:        false,
		RpcModules: []string{"kkc"},
		RpcAddr:    "/ip4/127.0.0.1/tcp/8545",
	}

	DefaultAccountConfig = AccountConfig{
		KeyStoreDir: "",
	}
)

// kkchain global configurations.
type Config struct {
	GeneralConfig `mapstructure:",squash"`

	// network config.
	Network *NetworkConfig `mapstructure:"network"`
	// dht config.
	Dht *DhtConfig `mapstructure:"dht"`

	Consensus *ConsensusConfig `mapstructure:"consensus"`

	Api *ApiConfig `mapstructure:"api"`

	Account *AccountConfig `mapstructure:"acc"`
}

// General settings
type GeneralConfig struct {
	DataDir string `mapstructure:"datadir"`
}

type NetworkConfig struct {
	//seed node address.
	Seeds []string `mapstructure:"seeds"`
	// Listen address.
	Listen string `mapstructure:"listen"`
	// Network node privateKey address. If nil, generate a new node.
	PrivateKey string `mapstructure:"privatekey"`
	// Network ID
	NetworkId uint32 `mapstructure:"networkid"`
	// Max peers connect with self node
	MaxPeers int `mapstructure:"maxpeers"`
}

type DhtConfig struct {
	BucketSize      int `mapstructure:"bucketsize"`
	RoutingTableDir string
	Seeds           []string
}

type ConsensusConfig struct {
	Mine     bool   `mapstructure:"mine"`
	Type     string `mapstructure:"type"`
	Coinbase string `mapstructure:"coinbase"`
}

type ApiConfig struct {
	Rpc        bool     `mapstructure:"rpc"`
	RpcModules []string `mapstructure:"rpcmodules"`
	RpcAddr    string   `mapstructure:"rpcaddr"`
}

type AccountConfig struct {
	KeyStoreDir string `mapstructure:"keystoredir"`
	Unlock      string `mapstructure:"unlock"`
	Password    string `mapstructure:"password"`
}

func LoadConfig(file string, cfg *Config) error {
	viper.SetConfigFile(file)
	viper.SetConfigType("toml")

	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	err := viper.Unmarshal(cfg)
	if err != nil {
		return err
	}
	return nil
}

// DefaultDataDir is the default data directory to use for the databases and other
// persistence requirements.
func DefaultDataDir() string {
	// Try to place the data folder in the user's home dir
	home := homeDir()
	if home != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "KKchain")
		} else if runtime.GOOS == "windows" {
			return filepath.Join(home, "AppData", "Roaming", "KKchain")
		} else {
			return filepath.Join(home, ".kkchain")
		}
	}
	// As we cannot guess a stable location, return empty and handle later
	return ""
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if usr, err := user.Current(); err == nil {
		return usr.HomeDir
	}
	return ""
}

// ResolvePath resolves path in the instance directory.
func (c *Config) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if c.DataDir == "" {
		return ""
	}

	return filepath.Join(c.DataDir, path)
}

// OpenDatabase opens an existing database with the given name (or creates one if no
// previous can be found) from within the node's instance directory. If the node is
// ephemeral, a memory database is returned.
func OpenDatabase(c *Config, name string) (storage.Database, error) {
	if c.DataDir == "" {
		return memdb.New(), nil
	}

	db, err := rocksdb.New(c.ResolvePath(name), nil)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// AccountConfig determines the settings for scrypt and keydirectory
func (c *Config) AccountConfig() (int, int, string, error) {
	scryptN := keystore.StandardScryptN
	scryptP := keystore.StandardScryptP

	var (
		keydir string
		err    error
	)
	switch {
	case filepath.IsAbs(c.Account.KeyStoreDir):
		keydir = c.Account.KeyStoreDir
	case c.DataDir != "":
		if c.Account.KeyStoreDir == "" {
			keydir = filepath.Join(c.DataDir, "keystore")
		} else {
			keydir, err = filepath.Abs(c.Account.KeyStoreDir)
		}
	case c.Account.KeyStoreDir != "":
		keydir, err = filepath.Abs(c.Account.KeyStoreDir)
	}
	return scryptN, scryptP, keydir, err
}

func MakeAccountManager(conf *Config) (*accounts.Manager, string, error) {

	scryptN, scryptP, keydir, err := conf.AccountConfig()
	var ephemeral string
	if keydir == "" {
		// There is no datadir.
		keydir, err = ioutil.TempDir("", "go-ethereum-keystore")
		ephemeral = keydir
	}

	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(keydir, 0700); err != nil {
		return nil, "", err
	}
	// Assemble the account manager and supported backends
	backends := []accounts.Backend{
		keystore.NewKeyStore(keydir, scryptN, scryptP),
	}

	return accounts.NewManager(backends...), ephemeral, nil
}
