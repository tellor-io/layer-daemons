// Command create-reporter submits a MsgCreateReporter for the operator account,
// signing it through the remote signer (mTLS + scope-checked SignTx) — same path
// as the reporter/unjail/create-validator commands, so no local private key is
// required. MsgCreateReporter must be on the signer's SignTx allowlist.
// Creating a reporter auto-self-selects the creator's own delegation, so the
// validator's self-bond becomes the reporter's stake.
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
	reportertypes "github.com/tellor-io/layer/x/reporter/types"

	"cosmossdk.io/math"

	cosmosclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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
	commRate := flag.String("commission-rate", "0.0", "reporter commission rate")
	minTokens := flag.String("min-tokens-required", "1000000", "min tokens a selector needs to join (loya)")
	moniker := flag.String("moniker", "max-profit-tellor", "reporter moniker")
	dryRun := flag.Bool("dry-run", false, "build and sign but do not broadcast")
	flag.Parse()

	ctx := context.Background()

	ec := rsclient.CreateEncodingConfig()
	reportertypes.RegisterInterfaces(ec.InterfaceRegistry)

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

	minTok, ok := math.NewIntFromString(*minTokens)
	if !ok {
		must("parse min-tokens-required", fmt.Errorf("invalid %q", *minTokens))
	}

	msg := &reportertypes.MsgCreateReporter{
		ReporterAddress:   fromAddr.String(),
		CommissionRate:    math.LegacyMustNewDecFromStr(*commRate),
		MinTokensRequired: minTok,
		Moniker:           *moniker,
	}

	fmt.Println("reporter account:", fromAddr.String())
	fmt.Println("commission:      ", *commRate)
	fmt.Println("min-tokens:      ", *minTokens, "loya")
	fmt.Println("moniker:         ", *moniker)

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
		fmt.Printf("dry-run OK: signed create-reporter tx built (%d bytes), not broadcasting\n", len(txBytes))
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
