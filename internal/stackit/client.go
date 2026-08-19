// Package stackit wraps the official STACKIT Go SDK's IaaS (Compute Engine)
// API so the controller package only deals with typed helpers instead of
// raw SDK request builders.
package stackit

import (
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

// NewClient creates a STACKIT IaaS API client using the SDK's default
// authentication flow. Credentials are resolved automatically from
// environment variables (STACKIT_SERVICE_ACCOUNT_KEY_PATH,
// STACKIT_SERVICE_ACCOUNT_KEY, STACKIT_PRIVATE_KEY_PATH,
// STACKIT_PRIVATE_KEY, STACKIT_SERVICE_ACCOUNT_TOKEN,
// STACKIT_FEDERATED_TOKEN_FILE, STACKIT_SERVICE_ACCOUNT_EMAIL) or from
// $HOME/.stackit/credentials.json. See config/manager/manager.yaml for how
// the operator Deployment wires a Kubernetes Secret into these variables.
func NewClient() (*iaas.APIClient, error) {
	return iaas.NewAPIClient()
}
