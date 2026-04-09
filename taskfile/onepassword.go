package taskfile

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/1password/onepassword-sdk-go"
)

// loadOnePasswordSecrets retrieves secrets from 1Password using the SDK.
// Each ref has the form 1password://account/vault/item/field.
//
// Authentication is determined automatically:
//   - If OP_SERVICE_ACCOUNT_TOKEN is set, it is used (CI/CD, automation)
//   - Otherwise, the desktop app integration is used with the account from the ref
func loadOnePasswordSecrets(entries []SecretEntry, env map[string]string) error {
	ctx := context.Background()

	// Cache clients per account to avoid creating multiple clients.
	clients := make(map[string]*onepassword.Client)

	for _, entry := range entries {
		account, opRef, err := parseOnePasswordRef(entry.Ref)
		if err != nil {
			return err
		}

		client, ok := clients[account]
		if !ok {
			var useDesktopApp bool
			client, useDesktopApp, err = newOnePasswordClient(account)
			if err != nil {
				return err
			}

			// When using the desktop app, validate the connection early.
			if useDesktopApp {
				if err := validateDesktopAppConnection(ctx, client, account); err != nil {
					return err
				}
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
	path := strings.TrimPrefix(ref, "1password://")

	account, rest, ok := strings.Cut(path, "/")
	if !ok || account == "" || rest == "" {
		return "", "", fmt.Errorf("invalid 1Password reference %q, expected 1password://account/vault/item/field", ref)
	}

	if !strings.Contains(account, ".") {
		account += ".1password.com"
	}

	return account, "op://" + rest, nil
}

func resolveHint(ref string, err error) string {
	path := strings.TrimPrefix(ref, "1password://")
	parts := strings.SplitN(path, "/", 4)

	msg := err.Error()

	switch {
	case strings.Contains(msg, "no vault matched"):
		vault := ""
		if len(parts) >= 2 {
			vault = parts[1]
		}
		return fmt.Sprintf("Vault %q was not found. Check that the vault name is correct and that your account has access to it.", vault)
	case strings.Contains(msg, "no item matched"):
		vault, item := "", ""
		if len(parts) >= 2 {
			vault = parts[1]
		}
		if len(parts) >= 3 {
			item = parts[2]
		}
		return fmt.Sprintf("Item %q was not found in vault %q. Check that the item name is correct.", item, vault)
	case strings.Contains(msg, "no field matched"):
		field := ""
		if len(parts) >= 4 {
			field = parts[3]
		}
		return fmt.Sprintf("Field %q was not found. Check that the field name is correct in 1Password.", field)
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

func newOnePasswordClient(account string) (client *onepassword.Client, useDesktopApp bool, err error) {
	ctx := context.Background()

	if token := os.Getenv("OP_SERVICE_ACCOUNT_TOKEN"); token != "" {
		client, err := onepassword.NewClient(
			ctx,
			onepassword.WithServiceAccountToken(token),
			onepassword.WithIntegrationInfo("gogo", "v1.0.0"),
		)
		if err != nil {
			return nil, false, fmt.Errorf("creating 1Password client with service account: %w", err)
		}
		return client, false, nil
	}

	client, err = onepassword.NewClient(
		ctx,
		onepassword.WithDesktopAppIntegration(account),
		onepassword.WithIntegrationInfo("gogo", "v1.0.0"),
	)
	if err != nil {
		return nil, true, fmt.Errorf(`creating 1Password client with desktop app (account %q): %w

Make sure the 1Password desktop app is installed and has SDK integration enabled:
  → Open 1Password > Settings > Developer > enable "Integrate with other apps"`, account, err)
	}

	return client, true, nil
}
