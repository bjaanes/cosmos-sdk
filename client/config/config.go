package config

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/client/configinit"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
)

const DefaultConfigTemplate = `# This is a TOML config file.
# For more information, see https://github.com/toml-lang/toml

###############################################################################
###                           Client Configuration                            ###
###############################################################################

# The network chain ID
chain-id = "{{ .ChainID }}"
# The keyring's backend, where the keys are stored (os|file|kwallet|pass|test|memory)
keyring-backend = "{{ .KeyringBackend }}"
# CLI output format (text|json)
output = "{{ .Output }}"
# <host>:<port> to Tendermint RPC interface for this chain
node = "{{ .Node }}"
# Transaction broadcasting mode (sync|async)
broadcast-mode = "{{ .BroadcastMode }}"
`

// Default constants
const (
	chainID        = ""
	keyringBackend = "os"
	output         = "text"
	node           = "tcp://localhost:26657"
	broadcastMode  = "sync"
)

type ClientConfig struct {
	ChainID        string `mapstructure:"chain-id" json:"chain-id"`
	KeyringBackend string `mapstructure:"keyring-backend" json:"keyring-backend"`
	Output         string `mapstructure:"output" json:"output"`
	Node           string `mapstructure:"node" json:"node"`
	BroadcastMode  string `mapstructure:"broadcast-mode" json:"broadcast-mode"`
}

// defaultClientConfig returns the reference to ClientConfig with default values.
func defaultClientConfig() *ClientConfig {
	return &ClientConfig{chainID, keyringBackend, output, node, broadcastMode}
}

func (c *ClientConfig) SetChainID(chainID string) {
	c.ChainID = chainID
}

func (c *ClientConfig) SetKeyringBackend(keyringBackend string) {
	c.KeyringBackend = keyringBackend
}

func (c *ClientConfig) SetOutput(output string) {
	c.Output = output
}

func (c *ClientConfig) SetNode(node string) {
	c.Node = node
}

func (c *ClientConfig) SetBroadcastMode(broadcastMode string) {
	c.BroadcastMode = broadcastMode
}

func GetClientConfig(v *viper.Viper) (ClientConfig, error) {
	conf := new(ClientConfig)
	if err := v.Unmarshal(conf); err != nil {
		return ClientConfig{}, fmt.Errorf("couldn't get client config: %v", err)
	}

	return *conf, nil
}

func LoadClientConfigFile(v *viper.Viper, homeDir string) error {
	configPath := filepath.Join(homeDir, "config") // TODO: Dep
	configFilePath := filepath.Join(configPath, "client.toml")

	// if config.toml file does not exist we create it and write default ClientConfig values into it.
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		conf := defaultClientConfig()
		if err := ensureConfigPath(configPath); err != nil {
			return fmt.Errorf("couldn't make client config: %v", err)
		}

		if err := configinit.WriteConfigToFile(configFilePath, DefaultConfigTemplate, conf); err != nil {
			return fmt.Errorf("could not write client config to the file: %v", err)
		}
	}

	v.SetConfigFile(configFilePath)
	return v.MergeInConfig()
}

// ensureConfigPath creates a directory configPath if it does not exist
func ensureConfigPath(configPath string) error {
	return os.MkdirAll(configPath, os.ModePerm)
}
