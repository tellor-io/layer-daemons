package client

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
)

func TestValidateAutoBalanceEthAddr(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input   string
		want    string
		wantErr bool
	}{
		"valid with 0x": {
			input: "0x6Ec401744008f4B018Ed9A36f76e6629799Ee50E",
			want:  "6ec401744008f4b018ed9a36f76e6629799ee50e",
		},
		"valid without 0x": {
			input: "6ec401744008f4b018ed9a36f76e6629799ee50e",
			want:  "6ec401744008f4b018ed9a36f76e6629799ee50e",
		},
		"empty": {
			input:   "",
			wantErr: true,
		},
		"too short": {
			input:   "0x1234",
			wantErr: true,
		},
		"non hex": {
			input:   "0xzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := validateAutoBalanceEthAddr(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseAutoBalanceExecutionTime(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input      string
		wantHour   int
		wantMinute int
		wantErr    bool
	}{
		"midnight":       {input: "00:00", wantHour: 0, wantMinute: 0},
		"end of day":     {input: "23:59", wantHour: 23, wantMinute: 59},
		"invalid format": {input: "3:00", wantErr: true},
		"extra segment":  {input: "03:00:00", wantErr: true},
		"bad hour":       {input: "24:00", wantErr: true},
		"bad minute":     {input: "12:60", wantErr: true},
		"empty":          {input: "", wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hour, minute, err := parseAutoBalanceExecutionTime(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantHour, hour)
			require.Equal(t, tc.wantMinute, minute)
		})
	}
}

func TestNextAutoBalanceRunUTC(t *testing.T) {
	t.Parallel()

	loc := time.UTC
	tests := []struct {
		name     string
		now      time.Time
		hour     int
		minute   int
		wantNext time.Time
	}{
		{
			name:     "later today",
			now:      time.Date(2026, 5, 20, 10, 0, 0, 0, loc),
			hour:     15,
			minute:   30,
			wantNext: time.Date(2026, 5, 20, 15, 30, 0, 0, loc),
		},
		{
			name:     "tomorrow when time passed",
			now:      time.Date(2026, 5, 20, 16, 0, 0, 0, loc),
			hour:     15,
			minute:   30,
			wantNext: time.Date(2026, 5, 21, 15, 30, 0, 0, loc),
		},
		{
			name:     "exactly at run time schedules tomorrow",
			now:      time.Date(2026, 5, 20, 15, 30, 0, 0, loc),
			hour:     15,
			minute:   30,
			wantNext: time.Date(2026, 5, 21, 15, 30, 0, 0, loc),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nextAutoBalanceRunUTC(tc.now, tc.hour, tc.minute)
			require.Equal(t, tc.wantNext, got)
		})
	}
}

func TestComputeAutoBalanceBridgeAmount(t *testing.T) {
	t.Parallel()

	keep := uint64(5_000_000)
	reserve := math.NewInt(autoBalanceGasReserveLoya)

	tests := map[string]struct {
		wallet   int64
		wantAmt  int64
		wantBridge bool
	}{
		"below keep":           {wallet: 4_000_000, wantBridge: false},
		"below keep plus reserve": {wallet: 5_500_000, wantBridge: false},
		"exact threshold": {
			wallet:     6_000_000,
			wantAmt:    0,
			wantBridge: false,
		},
		"above threshold": {
			wallet:     10_000_000,
			wantAmt:    4_000_000,
			wantBridge: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			walletBal := math.NewInt(tc.wallet)
			amount, bridge := computeAutoBalanceBridgeAmount(walletBal, keep)
			require.Equal(t, tc.wantBridge, bridge)
			if tc.wantBridge {
				want := walletBal.Sub(math.NewIntFromUint64(keep)).Sub(reserve)
				require.True(t, amount.Equal(want))
				require.Equal(t, tc.wantAmt, amount.Int64())
			}
		})
	}
}

func TestBuildAutoBalanceWithdrawMsg(t *testing.T) {
	t.Parallel()

	amount := math.NewInt(4_000_000)
	msg := buildAutoBalanceWithdrawMsg(
		"tellor1creator",
		"6ec401744008f4b018ed9a36f76e6629799ee50e",
		amount,
	)

	require.Equal(t, "tellor1creator", msg.Creator)
	require.Equal(t, "6ec401744008f4b018ed9a36f76e6629799ee50e", msg.Recipient)
	require.Equal(t, autoBalanceDenom, msg.Amount.Denom)
	require.True(t, msg.Amount.Amount.Equal(amount))
}

func TestAutoBalanceAlreadyBridgedToday(t *testing.T) {
	t.Parallel()

	c := NewClient(log.NewNopLogger(), "0.001loya")
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

	require.False(t, c.autoBalanceAlreadyBridgedToday(now))
	c.markAutoBalanceBridgedToday(now)
	require.True(t, c.autoBalanceAlreadyBridgedToday(now))
	require.False(t, c.autoBalanceAlreadyBridgedToday(now.Add(24*time.Hour)))
}
