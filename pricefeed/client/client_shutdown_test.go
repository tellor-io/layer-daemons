package client

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	daemonflags "github.com/tellor-io/layer-daemons/flags"
	"github.com/tellor-io/layer-daemons/mocks"
	"github.com/tellor-io/layer-daemons/pricefeed/client/types"
	"github.com/tellor-io/layer-daemons/testutil/constants"
	grpc_util "github.com/tellor-io/layer-daemons/testutil/grpc"

	"cosmossdk.io/log"
)

// TestStop_CompletesWhenStartFailsBeforeDaemonStartupDone guards the contract that
// daemonStartup must be released on every early return from start(); otherwise Stop()
// blocks forever on daemonStartup.Wait().
func TestStop_CompletesWhenStartFailsBeforeDaemonStartupDone(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mockGrpcClient              *mocks.GrpcClient
		exchangeIdToQueryConfig     map[types.ExchangeId]*types.ExchangeQueryConfig
		exchangeIdToExchangeDetails map[types.ExchangeId]types.ExchangeQueryDetails
		wantErrContains             string
	}{
		"tcp_connection_fails": {
			mockGrpcClient: grpc_util.GenerateMockGrpcClientWithOptionalTcpConnectionErrors(
				errors.New(connectionFailsErrorMsg),
				nil,
				false,
			),
			wantErrContains: connectionFailsErrorMsg,
		},
		"grpc_connection_fails": {
			mockGrpcClient: grpc_util.GenerateMockGrpcClientWithOptionalGrpcConnectionErrors(
				errors.New(connectionFailsErrorMsg),
				nil,
				false,
			),
			wantErrContains: connectionFailsErrorMsg,
		},
		"empty_exchange_config": {
			mockGrpcClient: grpc_util.GenerateMockGrpcClientWithOptionalGrpcConnectionErrors(
				nil,
				nil,
				true,
			),
			exchangeIdToQueryConfig:     map[types.ExchangeId]*types.ExchangeQueryConfig{},
			exchangeIdToExchangeDetails: map[types.ExchangeId]types.ExchangeQueryDetails{},
			wantErrContains:             "exchangeIds must not be empty",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := tc.exchangeIdToQueryConfig
			details := tc.exchangeIdToExchangeDetails
			if cfg == nil {
				cfg = constants.TestExchangeQueryConfigs
			}
			if details == nil {
				details = constants.TestExchangeIdToExchangeQueryDetails
			}

			faketaskRunner := FakeSubTaskRunner{}

			client := newClient(log.NewNopLogger())
			err := client.start(
				grpc_util.Ctx,
				daemonflags.GetDefaultDaemonFlags(),
				grpc_util.TcpEndpoint,
				tc.mockGrpcClient,
				[]types.MarketParam{},
				cfg,
				details,
				&faketaskRunner,
			)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErrContains)

			stopDone := make(chan struct{})
			go func() {
				client.Stop()
				close(stopDone)
			}()

			select {
			case <-stopDone:
			case <-time.After(2 * time.Second):
				t.Fatal("Stop() blocked — daemonStartup was likely not released on failed start()")
			}
		})
	}
}
