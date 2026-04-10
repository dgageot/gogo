package taskfile

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/1password/onepassword-sdk-go"
)

// loadOnePasswordSecrets retrieves secrets from 1Password using the SDK.
// Each ref has the form 1password://account/vault/item/field.
//
// Authentication is determined automatically:
//   - If OP_SERVICE_ACCOUNT_TOKEN is set, it is used (CI/CD, automation)
//   - Otherwise, the desktop app integration is used with the account from the ref
func loadOnePasswordSecrets(entries, env map[string]string) error {
	ctx := context.Background()

	// Cache clients per account to avoid creating multiple clients.
	clients := make(map[string]*onepassword.Client)

	for name, ref := range entries {
		account, opRef, err := parseOnePasswordRef(ref)
		if err != nil {
			return err
		}

		client, ok := clients[account]
		if !ok {
			logTask(colorCyan, "1password", "connecting to "+account)

			client, err = newOnePasswordClient(account)
			if err != nil {
				return err
			}
			clients[account] = client
		}

		logTask(colorCyan, "1password", "reading "+ref)

		secret, err := client.Secrets().Resolve(ctx, opRef)
		if err != nil {
			return fmt.Errorf("resolving 1Password secret %q: %w\n\n%s", ref, err, resolveHint(ctx, client, ref, err))
		}

		env[name] = secret
	}

	return nil
}

// parseOnePasswordRef extracts the account and op:// reference from a 1password:// URI.
// Input:  1password://account/vault/item/field
// Output: account, op://vault/item/field
func parseOnePasswordRef(ref string) (account, opRef string, err error) {
	const expected = "expected 1password://account/vault/item/field"

	path, ok := strings.CutPrefix(ref, onePasswordScheme)
	if !ok {
		return "", "", fmt.Errorf("invalid 1Password reference %q, %s", ref, expected)
	}

	account, rest, ok := strings.Cut(path, "/")
	if !ok || account == "" || rest == "" {
		return "", "", fmt.Errorf("invalid 1Password reference %q, %s", ref, expected)
	}

	if !strings.Contains(account, ".") {
		account += ".1password.com"
	}

	return account, "op://" + rest, nil
}

// resolveHint returns a user-friendly hint based on the 1Password error.
func resolveHint(ctx context.Context, client *onepassword.Client, ref string, err error) string {
	path, _ := strings.CutPrefix(ref, onePasswordScheme)
	parts := strings.SplitN(path, "/", 4)

	msg := err.Error()

	switch {
	case strings.Contains(msg, "no vault matched") && len(parts) > 1:
		hint := fmt.Sprintf("Vault %q was not found.", parts[1])
		if names := listVaultNames(ctx, client); len(names) > 0 {
			hint += " Available vaults: " + strings.Join(names, ", ")
		}
		return hint
	case strings.Contains(msg, "no item matched") && len(parts) > 2:
		return fmt.Sprintf("Item %q was not found in vault %q. Check that the item name is correct.", parts[2], parts[1])
	case strings.Contains(msg, "no field matched") && len(parts) > 3:
		return fmt.Sprintf("Field %q was not found. Check that the field name is correct in 1Password.", parts[3])
	default:
		return "Check that the secret reference follows the format 1password://account/vault/item/field"
	}
}

// listVaultNames returns the sorted names of all accessible vaults.
func listVaultNames(ctx context.Context, client *onepassword.Client) []string {
	vaults, err := client.Vaults().List(ctx)
	if err != nil {
		return nil
	}
	names := make([]string, len(vaults))
	for i, v := range vaults {
		names[i] = v.Title
	}
	slices.Sort(names)
	return names
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
func newOnePasswordClient(account string) (*onepassword.Client, error) {
	ctx := context.Background()

	opts := []onepassword.ClientOption{opIntegrationInfo}
	if token := os.Getenv("OP_SERVICE_ACCOUNT_TOKEN"); token != "" {
		opts = append(opts, onepassword.WithServiceAccountToken(token))
	} else {
		opts = append(opts, onepassword.WithDesktopAppIntegration(account))
	}

	client, err := onepassword.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating 1Password client for account %q: %w", account, err)
	}

	// Validate desktop app connection early when not using service account.
	if os.Getenv("OP_SERVICE_ACCOUNT_TOKEN") == "" {
		if err := validateDesktopAppConnection(ctx, client, account); err != nil {
			return nil, err
		}
	}

	return client, nil
}
