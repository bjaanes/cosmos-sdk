package cli

import (
	"context"
	"fmt"
	"github.com/cosmos/cosmos-sdk/client/configinit"
	"github.com/spf13/viper"

	"github.com/spf13/cobra"
	cli2 "github.com/tendermint/tendermint/libs/cli"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/x/bank/client/cli"
)

// ExecTestCLICmd builds the client context, mocks the output and executes the command.
func ExecTestCLICmd(clientCtx client.Context, cmd *cobra.Command, extraArgs []string) (testutil.BufferWriter, error) {
	cmd.SetArgs(extraArgs)

	_, out := testutil.ApplyMockIO(cmd)
	clientCtx = clientCtx.WithOutput(out)

	v := viper.New()
	if err := configinit.InitiateViper(v, cmd, "TESTD"); err != nil {
		return out, err
	}
	clientCtx = clientCtx.WithViper(v)

	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	cmd.SetContext(ctx)
	if err := client.SetCmdClientContextHandler(clientCtx, cmd); err != nil {
		return out, err
	}

	if err := cmd.Execute(); err != nil {
		return out, err
	}

	return out, nil
}

func MsgSendExec(clientCtx client.Context, from, to, amount fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {
	args := []string{from.String(), to.String(), amount.String()}
	args = append(args, extraArgs...)

	return ExecTestCLICmd(clientCtx, cli.NewSendTxCmd(), args)
}

func QueryBalancesExec(clientCtx client.Context, address fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {
	args := []string{address.String(), fmt.Sprintf("--%s=json", cli2.OutputFlag)}
	args = append(args, extraArgs...)

	return ExecTestCLICmd(clientCtx, cli.GetBalancesCmd(), args)
}
