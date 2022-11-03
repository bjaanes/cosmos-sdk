package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	tmcmd "github.com/tendermint/tendermint/cmd/tendermint/commands"
	tmcfg "github.com/tendermint/tendermint/config"
	tmcli "github.com/tendermint/tendermint/libs/cli"
	tmflags "github.com/tendermint/tendermint/libs/cli/flags"
	tmlog "github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
)

// DONTCOVER

// ServerContextKey defines the context key used to retrieve a server.Context from
// a command's Context.
const ServerContextKey = sdk.ContextKey("server.context")

// server context
type Context struct {
	Viper  *viper.Viper
	Config *tmcfg.Config
	Logger tmlog.Logger
}

// ErrorCode contains the exit code for server exit.
type ErrorCode struct {
	Code int
}

func (e ErrorCode) Error() string {
	return strconv.Itoa(e.Code)
}

func NewDefaultContext() *Context {
	return NewContext(
		viper.New(),
		tmcfg.DefaultConfig(),
		tmlog.NewTMLogger(tmlog.NewSyncWriter(os.Stdout)),
	)
}

func NewContext(v *viper.Viper, config *tmcfg.Config, logger tmlog.Logger) *Context {
	return &Context{v, config, logger}
}

// InterceptConfigsPreRunHandler performs a pre-run function for the root daemon
// application command. It will create a Viper literal and a default server
// Context. The server Tendermint configuration will either be read and parsed
// or created and saved to disk, where the server Context is updated to reflect
// the Tendermint configuration. It takes custom app config template and config
// settings to create a custom Tendermint configuration. If the custom template
// is empty, it uses default-template provided by the server. The Viper literal
// is used to read and parse the application configuration. Command handlers can
// fetch the server Context to get the Tendermint configuration or to get access
// to Viper.
func InterceptConfigsPreRunHandler(v *viper.Viper, cmd *cobra.Command, customAppTemplate string, customConfig interface{}, tmConfig *tmcfg.Config) error {
	serverCtx := NewDefaultContext()
	serverCtx.Viper = v

	if err := MergeInTendermintConfigFile(v, tmConfig); err != nil {
		return err
	}
	if err := MergeInAppConfigFile(v, customAppTemplate, customConfig); err != nil {
		return err
	}

	if err := v.Unmarshal(serverCtx); err != nil {
		return err
	}
	if err := v.Unmarshal(tmConfig); err != nil {
		return err
	}

	// return value is a tendermint configuration object
	serverCtx.Config = tmConfig
	logger := tmlog.NewTMLogger(tmlog.NewSyncWriter(os.Stdout))
	logger, err := tmflags.ParseLogLevel(tmConfig.LogLevel, logger, tmcfg.DefaultLogLevel)
	if err != nil {
		return err
	}

	// Check if the tendermint flag for trace logging is set
	// if it is then set up a tracing logger in this app as well
	if serverCtx.Viper.GetBool(tmcli.TraceFlag) {
		logger = tmlog.NewTracingLogger(logger)
	}

	serverCtx.Logger = logger.With("module", "main")

	return SetCmdServerContext(cmd, serverCtx)
}

// GetServerContextFromCmd returns a Context from a command or an empty Context
// if it has not been set.
func GetServerContextFromCmd(cmd *cobra.Command) *Context {
	if v := cmd.Context().Value(ServerContextKey); v != nil {
		serverCtxPtr := v.(*Context)
		return serverCtxPtr
	}

	return NewDefaultContext()
}

// SetCmdServerContext sets a command's Context value to the provided argument.
func SetCmdServerContext(cmd *cobra.Command, serverCtx *Context) error {
	v := cmd.Context().Value(ServerContextKey)
	if v == nil {
		return errors.New("server context not set")
	}

	serverCtxPtr := v.(*Context)
	*serverCtxPtr = *serverCtx

	return nil
}

func MergeInTendermintConfigFile(v *viper.Viper, defaultConfig *tmcfg.Config) error {
	homeDir := v.GetString(flags.FlagHome)
	configPath := filepath.Join(homeDir, "config")
	configFilePath := filepath.Join(configPath, "config.toml")

	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		tmcfg.EnsureRoot(configPath)

		if err = defaultConfig.ValidateBasic(); err != nil {
			return fmt.Errorf("error in config file: %w", err)
		}

		defaultConfig.RPC.PprofListenAddress = "localhost:6060"
		defaultConfig.P2P.RecvRate = 5120000
		defaultConfig.P2P.SendRate = 5120000
		defaultConfig.Consensus.TimeoutCommit = 5 * time.Second
		tmcfg.WriteConfigFile(configFilePath, defaultConfig)
	}

	v.SetConfigFile(configFilePath)
	return v.MergeInConfig()
}

func MergeInAppConfigFile(v *viper.Viper, customAppTemplate string, customConfig interface{}) error {
	homeDir := v.GetString(flags.FlagHome)
	configPath := filepath.Join(homeDir, "config")
	configFilePath := filepath.Join(configPath, "app.toml")

	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		if customAppTemplate != "" {
			config.SetConfigTemplate(customAppTemplate)

			if err = v.Unmarshal(&customConfig); err != nil {
				return fmt.Errorf("failed to parse %s: %w", configFilePath, err)
			}

			config.WriteConfigFile(configFilePath, customConfig)
		} else {
			appConf, err := config.ParseConfig(v)
			if err != nil {
				return fmt.Errorf("failed to parse %s: %w", configFilePath, err)
			}

			config.WriteConfigFile(configFilePath, appConf)
		}
	}

	v.SetConfigFile(configFilePath)
	return v.MergeInConfig()
}

// add server commands
func AddCommands(rootCmd *cobra.Command, defaultNodeHome string, appCreator types.AppCreator, appExport types.AppExporter, addStartFlags types.ModuleInitFlags) {
	tendermintCmd := &cobra.Command{
		Use:   "tendermint",
		Short: "Tendermint subcommands",
	}

	tendermintCmd.AddCommand(
		ShowNodeIDCmd(),
		ShowValidatorCmd(),
		ShowAddressCmd(),
		VersionCmd(),
		tmcmd.ResetAllCmd,
		tmcmd.ResetStateCmd,
	)

	startCmd := StartCmd(appCreator, defaultNodeHome)
	addStartFlags(startCmd)

	rootCmd.AddCommand(
		startCmd,
		tendermintCmd,
		ExportCmd(appExport, defaultNodeHome),
		version.NewVersionCommand(),
		NewRollbackCmd(appCreator, defaultNodeHome),
	)
}

// https://stackoverflow.com/questions/23558425/how-do-i-get-the-local-ip-address-in-go
// TODO there must be a better way to get external IP
func ExternalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		if skipInterface(iface) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}

		for _, addr := range addrs {
			ip := addrToIP(addr)
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", errors.New("are you connected to the network?")
}

// TrapSignal traps SIGINT and SIGTERM and terminates the server correctly.
func TrapSignal(cleanupFunc func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs

		if cleanupFunc != nil {
			cleanupFunc()
		}
		exitCode := 128

		switch sig {
		case syscall.SIGINT:
			exitCode += int(syscall.SIGINT)
		case syscall.SIGTERM:
			exitCode += int(syscall.SIGTERM)
		}

		os.Exit(exitCode)
	}()
}

// WaitForQuitSignals waits for SIGINT and SIGTERM and returns.
func WaitForQuitSignals() ErrorCode {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	return ErrorCode{Code: int(sig.(syscall.Signal)) + 128}
}

// GetAppDBBackend gets the backend type to use for the application DBs.
func GetAppDBBackend(opts types.AppOptions) dbm.BackendType {
	rv := cast.ToString(opts.Get("app-db-backend"))

	if len(rv) == 0 {
		rv = cast.ToString(opts.Get("db-backend"))
	}
	if len(rv) != 0 {
		return dbm.BackendType(rv)
	}
	return dbm.GoLevelDBBackend
}

func skipInterface(iface net.Interface) bool {
	if iface.Flags&net.FlagUp == 0 {
		return true // interface down
	}

	if iface.Flags&net.FlagLoopback != 0 {
		return true // loopback interface
	}

	return false
}

func addrToIP(addr net.Addr) net.IP {
	var ip net.IP

	switch v := addr.(type) {
	case *net.IPNet:
		ip = v.IP
	case *net.IPAddr:
		ip = v.IP
	}
	return ip
}

func openDB(rootDir string, backendType dbm.BackendType) (dbm.DB, error) {
	dataDir := filepath.Join(rootDir, "data")
	return dbm.NewDB("application", backendType, dataDir)
}

func openTraceWriter(traceWriterFile string) (w io.WriteCloser, err error) {
	if traceWriterFile == "" {
		return
	}
	return os.OpenFile(
		traceWriterFile,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		0o666,
	)
}
