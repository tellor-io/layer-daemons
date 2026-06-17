package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func bindViperForTest(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	viper.Reset()
	require.NoError(t, viper.BindPFlags(cmd.Flags()))
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()
}

func TestViperReadsPrometheusPortFromEnv(t *testing.T) {
	t.Setenv("PROMETHEUS_PORT", "12345")

	cmd := &cobra.Command{}
	cmd.Flags().Int("prometheus-port", 26661, "")
	bindViperForTest(t, cmd)

	require.Equal(t, 12345, viper.GetInt("prometheus-port"))
}

func TestViperReadsTestModeFlagsFromEnv(t *testing.T) {
	t.Setenv("TEST", "true")
	t.Setenv("TEST_QUERY_ID", "abc123")

	cmd := &cobra.Command{}
	cmd.Flags().Bool("test", false, "")
	cmd.Flags().String("test-query-id", "", "")
	bindViperForTest(t, cmd)

	require.True(t, viper.GetBool("test"))
	require.Equal(t, "abc123", viper.GetString("test-query-id"))
}
