// Command create-validator submits a MsgCreateValidator for the operator account,
// signing it through the remote signer (mTLS + scope-checked SignTx) — the same path
// the reporter and cmd/unjail use — so no local private key is required.
// MsgCreateValidator must be on the signer's SignTx allowlist. The consensus pubkey is
// the validator's ed25519 key (held by the remote signer / presented by the node).
package main

import (
	"context"
	"encoding/base64"
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
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
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
	gas := flag.Uint64("gas", 600000, "gas limit")
	moniker := flag.String("moniker", "", "validator moniker")
	pubkeyB64 := flag.String("consensus-pubkey", "", "ed25519 consensus pubkey (base64)")
	amount := flag.String("amount", "", "self-bond amount in loya (e.g. 19000000)")
	commRate := flag.String("commission-rate", "0.10", "commission rate")
	commMax := flag.String("commission-max-rate", "0.20", "commission max rate")
	commMaxChange := flag.String("commission-max-change-rate", "0.01", "commission max change rate")
	minSelf := flag.String("min-self-delegation", "1", "min self delegation (loya)")
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

	pkBytes, err := base64.StdEncoding.DecodeString(*pubkeyB64)
	must("decode consensus pubkey", err)
	if len(pkBytes) != 32 {
		must("consensus pubkey length", fmt.Errorf("expected 32 bytes, got %d", len(pkBytes)))
	}
	consPub := &ed25519.PubKey{Key: pkBytes}

	amt, ok := math.NewIntFromString(*amount)
	if !ok {
		must("parse amount", fmt.Errorf("invalid amount %q", *amount))
	}
	selfDel := sdk.NewCoin("loya", amt)

	minSelfInt, ok := math.NewIntFromString(*minSelf)
	if !ok {
		must("parse min-self-delegation", fmt.Errorf("invalid %q", *minSelf))
	}

	desc := stakingtypes.NewDescription(*moniker, "", "", "", "")
	comm := stakingtypes.NewCommissionRates(
		math.LegacyMustNewDecFromStr(*commRate),
		math.LegacyMustNewDecFromStr(*commMax),
		math.LegacyMustNewDecFromStr(*commMaxChange),
	)

	msg, err := stakingtypes.NewMsgCreateValidator(valAddr.String(), consPub, selfDel, desc, comm, minSelfInt)
	must("build MsgCreateValidator", err)

	fmt.Println("operator account: ", fromAddr.String())
	fmt.Println("validator:        ", valAddr.String())
	fmt.Println("moniker:          ", *moniker)
	fmt.Println("self-bond:        ", selfDel.String())
	fmt.Println("commission:       ", *commRate, "/", *commMax, "/", *commMaxChange)
	fmt.Println("min-self-deleg:   ", *minSelf, "loya")
	fmt.Println("consensus pubkey: ", *pubkeyB64)

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
		fmt.Printf("dry-run OK: signed create-validator tx built (%d bytes), not broadcasting\n", len(txBytes))
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
