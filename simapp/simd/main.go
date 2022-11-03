package main

import (
	"os"

	"cosmossdk.io/simapp"
	"cosmossdk.io/simapp/simd/cmd"
	"github.com/cosmos/cosmos-sdk/server"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

func main() {
	envPrefix := "SIMD"
	rootCmd := cmd.NewRootCmd(envPrefix)
	if err := svrcmd.Execute(rootCmd, envPrefix, simapp.DefaultNodeHome); err != nil {
		switch e := err.(type) {
		case server.ErrorCode:
			os.Exit(e.Code)

		default:
			os.Exit(1)
		}
	}
}
