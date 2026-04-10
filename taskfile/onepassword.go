package taskfile

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/1password/onepassword-sdk-go"
)

// loadOnePasswordSecrets retrieves secrets from 1Password using the SDK.
// Each ref has the form 1password://account/vault/item/field.
//
// Authentication is determined automatically:
//   - If OP_SERVICE_ACCOUNT_TOKEN is set, it is used (CI/CD, automation)
//   - Otherwise, the desktop app integration is used with the account from the ref
const onePasswordTimeout = 5 * time.Second

func loadOnePasswordSecrets(entries []SecretEntry, env map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), onePasswordTimeout)
	defer cancel()

	// Cache clients per account to avoid creating multiple clients.
	clients := make(map[string]*onepassword.Client)

	for _, entry := range entries {
		account, opRef, err := parseOnePasswordRef(entry.Ref)
		if err != nil {
			return err
		}

		client, ok := clients[account]
		if !ok {
			logTask(colorCyan, "1password", "connecting to "+account)

			client, err = newOnePasswordClient(ctx, account)
			if err != nil {
				return err
			}
			clients[account] = client
		}

		logTask(colorCyan, "1password", "reading "+entry.Ref)

		secret, err := client.Secrets().Resolve(ctx, opRef)
		if err != nil {
			return fmt.Errorf("resolving 1Password secret %q: %w\n\n%s", entry.Ref, err, resolveHint(entry.Ref, err))
		}

		env[entry.Env] = secret
	}

	return nil
}

// parseOnePasswordRef extracts the account and op:// reference from a 1password:// URI.
// Input:  1password://account/vault/item/field
// Output: account, op://vault/item/field
func parseOnePasswordRef(ref string) (account, opRef string, err error) {
	path, ok := strings.CutPrefix(ref, onePasswordScheme)
	if !ok {
		return "", "", fmt.Errorf("invalid 1Password reference %q, expected 1password://account/vault/item/field", ref)
	}

	account, rest, ok := strings.Cut(path, "/")
	if !ok || account == "" || rest == "" {
		return "", "", fmt.Errorf("invalid 1Password reference %q, expected 1password://account/vault/item/field", ref)
	}

	if !strings.Contains(account, ".") {
		account += ".1password.com"
	}

	return account, "op://" + rest, nil
}

// resolveHint returns a user-friendly hint based on the 1Password error.
func resolveHint(ref string, err error) string {
	path, _ := strings.CutPrefix(ref, onePasswordScheme)
	parts := strings.SplitN(path, "/", 4)

	msg := err.Error()

	switch {
	case strings.Contains(msg, "no vault matched") && len(parts) > 1:
		return fmt.Sprintf("Vault %q was not found. Check that the vault name is correct and that your account has access to it.", parts[1])
	case strings.Contains(msg, "no item matched") && len(parts) > 2:
		return fmt.Sprintf("Item %q was not found in vault %q. Check that the item name is correct.", parts[2], parts[1])
	case strings.Contains(msg, "no field matched") && len(parts) > 3:
		return fmt.Sprintf("Field %q was not found. Check that the field name is correct in 1Password.", parts[3])
	default:
		return "Check that the secret reference follows the format 1password://account/vault/item/field"
	}
}

// validateDesktopAppConnection checks that the desktop app integration is working
// by listing vaults. If the SDK integration is not enabled, the client is created
// successfully but all operations fail with misleading errors.
func validateDesktopAppConnection(ctx context.Context, client *onepassword.Client, account string) error {
	if _, err := client.Vaults().List(ctx); err != nil {
		return fmt.Errorf(`1Password desktop app integration is not working for account %q: %w

Make sure:
  1. The 1Password desktop app is running and unlocked
  2. SDK integration is enabled: Settings > Developer > "Integrate with other apps"
  3. The account name %q matches the one shown in the 1Password sidebar`, account, err, account)
	}
	return nil
}

var opIntegrationInfo = onepassword.WithIntegrationInfo("gogo", "v1.0.0")

// newOnePasswordClient creates a 1Password client, preferring service account token over desktop app.
// The call is wrapped in a timeout because the SDK may not respect context cancellation.
func newOnePasswordClient(ctx context.Context, account string) (*onepassword.Client, error) {
	type result struct {
		client *onepassword.Client
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		client, err := newOnePasswordClientBlocking(ctx, account)
		ch <- result{client, err}
	}()

	select {
	case r := <-ch:
		return r.client, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf(`1Password connection timed out for account %q

Make sure:
  1. The 1Password desktop app is running and unlocked
  2. SDK integration is enabled: Settings > Developer > "Integrate with other apps"`, account)
	}
}

func newOnePasswordClientBlocking(ctx context.Context, account string) (*onepassword.Client, error) {
	if token := os.Getenv("OP_SERVICE_ACCOUNT_TOKEN"); token != "" {
		client, err := onepassword.NewClient(
			ctx,
			onepassword.WithServiceAccountToken(token),
			opIntegrationInfo,
		)
		if err != nil {
			return nil, fmt.Errorf("creating 1Password client with service account: %w", err)
		}
		return client, nil
	}

	client, err := onepassword.NewClient(
		ctx,
		onepassword.WithDesktopAppIntegration(account),
		opIntegrationInfo,
	)
	if err != nil {
		return nil, fmt.Errorf("creating 1Password client with desktop app (account %q): %w", account, err)
	}

	// Validate the desktop app connection early.
	if err := validateDesktopAppConnection(ctx, client, account); err != nil {
		return nil, err
	}

	return client, nil
}
