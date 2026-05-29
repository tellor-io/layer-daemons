package contract_handlers

import (
	"os"
	"testing"
)

const runContractIntegrationTestsEnv = "RUN_CONTRACT_INTEGRATION_TESTS"

func skipContractIntegrationUnlessEnabled(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping contract integration test in short mode")
	}
	if os.Getenv(runContractIntegrationTestsEnv) == "" {
		t.Skipf("skipping contract integration test; set %s=1 to run", runContractIntegrationTestsEnv)
	}
}
