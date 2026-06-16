// Copyright IBM Corp. 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-mcp-server/pkg/utils"
	"github.com/hashicorp/terraform-mcp-server/version"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

const (
	TerraformAddress        = "TFE_ADDRESS"
	TerraformToken          = "TFE_TOKEN"
	TerraformSkipTLSVerify  = "TFE_SKIP_TLS_VERIFY"
	DefaultTerraformAddress = "https://app.terraform.io"
	ForwardClientIP         = "MCP_FORWARD_CLIENT_IP"
	ClientIPKey             = "CLIENT_IP"
)

var activeTfeClients sync.Map

// NewTfeClient creates a new TFE client for the given session
func NewTfeClient(sessionId string, terraformAddress string, terraformSkipTLSVerify bool, terraformToken string, clientIP string, logger *log.Logger) (*tfe.Client, error) {
	if terraformToken == "" {
		logger.Warn("No Terraform token provided, TFE client will not be available")
		return nil, utils.LogAndReturnError(logger, "required input: no Terraform token provided", nil)
	}

	config := &tfe.Config{
		Address:           terraformAddress,
		Token:             terraformToken,
		RetryServerErrors: true,
		Headers:           make(http.Header),
	}

	config.Headers.Set("User-Agent", fmt.Sprintf("terraform-mcp-server/%s", version.GetHumanVersion()))
	if clientIP != "" {
		config.Headers.Set("X-Forwarded-For", clientIP)
	}
	config.HTTPClient = createHTTPClient(terraformSkipTLSVerify, logger)

	client, err := tfe.NewClient(config)
	if err != nil {
		logger.Warnf("Failed to create a Terraform Cloud/Enterprise client: %v", err)
		return nil, utils.LogAndReturnError(logger, "creating TFE client", err)
	}

	activeTfeClients.Store(sessionId, client)
	logger.WithField("session_id", sessionId).Info("Created TFE client")
	return client, nil
}

// GetTfeClient retrieves the TFE client for the given session
func GetTfeClient(sessionId string) *tfe.Client {
	if value, ok := activeTfeClients.Load(sessionId); ok {
		return value.(*tfe.Client)
	}
	return nil
}

// DeleteTfeClient removes the TFE client for the given session
func DeleteTfeClient(sessionId string) {
	activeTfeClients.Delete(sessionId)
}

// SessionIdentityFromContext returns a stable identifier for the credential/session associated
// with this request, suitable for partitioning per-session caches. Each MCP session has its own
// auth-validated TFE client, so keying on the session ID prevents one session from reading state
// another session loaded. Returns "" when there is no active session.
func SessionIdentityFromContext(ctx context.Context) string {
	if session := server.ClientSessionFromContext(ctx); session != nil {
		return session.SessionID()
	}
	return ""
}

// GetTfeClientFromContext extracts TFE client from the MCP context
func GetTfeClientFromContext(ctx context.Context, logger *log.Logger) (*tfe.Client, error) {
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return nil, fmt.Errorf("no active session")
	}

	// Try to get existing client
	client := GetTfeClient(session.SessionID())
	if client != nil {
		return client, nil
	}

	logger.Warnf("TFE client not found, creating a new one")
	return CreateTfeClientForSession(ctx, session, logger)
}

// CreateTfeClientForSession creates only a TFE client for the session
func CreateTfeClientForSession(ctx context.Context, session server.ClientSession, logger *log.Logger) (*tfe.Client, error) {
	var err error
	terraformAddress, ok := ctx.Value(contextKey(TerraformAddress)).(string)
	if !ok || terraformAddress == "" {
		terraformAddress = utils.GetEnv(TerraformAddress, DefaultTerraformAddress)
	}

	terraformToken, ok := ctx.Value(contextKey(TerraformToken)).(string)
	if !ok || terraformToken == "" {
		terraformToken = utils.GetEnv(TerraformToken, "")
	}
	if terraformToken == "" {
		terraformToken, err = ReadCredentialsFile(extractHostname(terraformAddress), logger)
		if err != nil {
			return nil, err
		}
		logger.Info("Read TFE_TOKEN from credentials.tfrc.json")
	}


	// Get client IP from context for X-Forwarded-For header
	clientIP, _ := ctx.Value(contextKey(ClientIPKey)).(string)
	client, err := NewTfeClient(session.SessionID(), terraformAddress, parseTerraformSkipTLSVerify(ctx), terraformToken, clientIP, logger)
	return client, err
}
