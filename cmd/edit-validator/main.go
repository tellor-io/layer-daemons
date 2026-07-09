// Command edit-validator submits a MsgEditValidator for the operator's validator,
// signing it through the remote signer (mTLS) — same path as create-validator.
// It changes ONLY the commission rate (description fields are left untouched via
// the [do-not-modify] sentinel, min-self-delegation is left nil). The staking
// module enforces: at most one commission edit per 24h, and the change must be
// within max_change_rate of the current rate. For max-profit-tellor that means
// at most a 1% step per day (10% -> 9% -> 8% -> ...).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	rsclient "github.com/tellor-io/layer-daemons/reporter/client"
	// sets the tellor bech32 address prefix via init()
	_ "github.com/tellor-io/layer/app/config"

	"cosmossdk.io/math"

	cosmosclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func must(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR (%s): %v\n", what, err)
		os.Exit(1)
	}
}

func main() {
	os.Exit(run())
}

func run() int {
	signerAddr := flag.String("remote-signer-addr", "", "remote signer gRPC address (host:port)")
	ca := flag.String("remote-signer-ca-cert", "", "CA cert path")
	cert := flag.String("remote-signer-client-cert", "", "client cert path")
	key := flag.String("remote-signer-client-key", "", "client key path")
	node := flag.String("node", "tcp://127.0.0.1:26657", "CometBFT RPC endpoint")
	chainID := flag.String("chain-id", "tellor-1", "chain id")
	gasPrices := flag.String("gas-prices", "0.000025loya", "gas prices")
	gas := flag.Uint64("gas", 400000, "gas limit")
	commRate := flag.String("commission-rate", "0.09", "new commission rate (one step, within max_change_rate)")
	dryRun := flag.Bool("dry-run", false, "build and sign but do not broadcast")
	flag.Parse()

	ctx := context.Background()

	ec := rsclient.CreateEncodingConfig()
	stakingtypes.RegisterInterfaces(ec.InterfaceRegistry)

	kr, fromAddr, conn, err := rsclient.NewRemoteSignerKeyringTx(ctx, "reporter", *signerAddr, *ca, *cert, *key)
	must("dial remote signer", err)
	defer conn.Close()

	rpcClient, err := rpchttp.New(*node, "/websocket")
	must("create rpc client", err)

	clientCtx := cosmosclient.Context{}.
		WithCodec(ec.Codec).
		WithInterfaceRegistry(ec.InterfaceRegistry).
		WithTxConfig(ec.TxConfig).
		WithChainID(*chainID).
		WithKeyring(kr).
		WithFromName("reporter").
		WithFrom("reporter").
		WithFromAddress(fromAddr).
		WithClient(rpcClient).
		WithBroadcastMode("sync").
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithSkipConfirmation(true)

	valAddr := sdk.ValAddress(fromAddr)

	newRate := math.LegacyMustNewDecFromStr(*commRate)
	// Leave the description unchanged: every field is the [do-not-modify] sentinel.
	desc := stakingtypes.NewDescription(
		stakingtypes.DoNotModifyDesc,
		stakingtypes.DoNotModifyDesc,
		stakingtypes.DoNotModifyDesc,
		stakingtypes.DoNotModifyDesc,
		stakingtypes.DoNotModifyDesc,
	)
	// nil min-self-delegation => unchanged.
	msg := stakingtypes.NewMsgEditValidator(valAddr.String(), desc, &newRate, nil)

	fmt.Println("operator account:", fromAddr.String())
	fmt.Println("validator:       ", valAddr.String())
	fmt.Println("new commission:  ", newRate.String())

	txf := tx.Factory{}.
		WithChainID(*chainID).
		WithKeybase(kr).
		WithTxConfig(ec.TxConfig).
		WithAccountRetriever(clientCtx.AccountRetriever).
		WithGas(*gas).
		WithGasPrices(*gasPrices).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT).
		WithSequence(0).
		WithUnordered(true).
		WithTimeoutTimestamp(time.Now().Add(60 * time.Second))

	txf, err = txf.Prepare(clientCtx)
	must("prepare tx factory", err)

	txb, err := txf.BuildUnsignedTx(msg)
	must("build unsigned tx", err)

	must("sign tx", tx.Sign(ctx, txf, "reporter", txb, true))

	txBytes, err := ec.TxConfig.TxEncoder()(txb.GetTx())
	must("encode tx", err)

	if *dryRun {
		fmt.Printf("dry-run OK: signed edit-validator tx built (%d bytes), not broadcasting\n", len(txBytes))
		return 0
	}

	res, err := clientCtx.BroadcastTx(txBytes)
	must("broadcast tx", err)
	fmt.Printf("broadcast: code=%d txhash=%s\n", res.Code, res.TxHash)
	if res.RawLog != "" {
		fmt.Println("rawlog:", res.RawLog)
	}
	if res.Code != 0 {
		return 2
	}
	return 0
}
