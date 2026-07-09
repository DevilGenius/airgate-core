package account

import (
	"errors"

	"github.com/DevilGenius/airgate-core/internal/accountidentity"
)

func normalizeAccountIdentity(email *string, credentials map[string]string) (*string, map[string]string, error) {
	resolvedEmail, resolvedCredentials, err := accountidentity.Resolve(email, credentials)
	return resolvedEmail, resolvedCredentials, mapAccountIdentityError(err)
}

func normalizeAccountEmail(email *string) (*string, error) {
	normalized, err := accountidentity.NormalizeOptional(email)
	return normalized, mapAccountIdentityError(err)
}

func normalizeAccountIdentityUpdate(current Account, input UpdateInput) (UpdateInput, error) {
	resolvedEmail, resolvedCredentials, err := accountidentity.ResolveUpdate(
		current.Email,
		current.Credentials,
		input.Email,
		input.HasEmail,
		input.Credentials,
	)
	if err != nil {
		return input, mapAccountIdentityError(err)
	}
	input.Email = resolvedEmail
	input.HasEmail = true
	input.Credentials = resolvedCredentials
	return input, nil
}

func syncAccountCredentials(credentials map[string]string, email *string) map[string]string {
	return accountidentity.SyncCredentials(credentials, email)
}

func accountEmailsEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func mapAccountIdentityError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, accountidentity.ErrInvalidEmail):
		return ErrInvalidAccountEmail
	case errors.Is(err, accountidentity.ErrEmailMismatch):
		return ErrAccountEmailMismatch
	default:
		return err
	}
}
