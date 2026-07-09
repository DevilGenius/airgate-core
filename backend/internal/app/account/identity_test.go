package account

import (
	"context"
	"errors"
	"testing"
)

func TestServiceCreateNormalizesTopLevelEmail(t *testing.T) {
	rawEmail := "  OAuth.User@Example.COM "
	var captured CreateInput
	service := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			captured = input
			return Account{ID: 7, Email: input.Email}, nil
		},
	}, nil, nil, nil)

	created, err := service.Create(t.Context(), CreateInput{
		Name:        "duplicate labels are allowed",
		Email:       &rawEmail,
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]string{"access_token": "token"},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if captured.Email == nil || *captured.Email != "oauth.user@example.com" || created.Email == nil || *created.Email != "oauth.user@example.com" {
		t.Fatalf("normalized email captured=%#v created=%#v", captured.Email, created.Email)
	}
	if captured.Credentials["access_token"] != "token" || captured.Credentials["email"] != "oauth.user@example.com" {
		t.Fatalf("captured credentials = %#v", captured.Credentials)
	}
}

func TestAccountIdentityResolvesCredentialEmailAndRejectsMismatch(t *testing.T) {
	invalid := "not-an-email"
	if _, _, err := normalizeAccountIdentity(&invalid, map[string]string{}); !errors.Is(err, ErrInvalidAccountEmail) {
		t.Fatalf("invalid email error = %v, want ErrInvalidAccountEmail", err)
	}

	topLevel := " OAuth@Example.COM "
	email, credentials, err := normalizeAccountIdentity(&topLevel, map[string]string{"email": "oauth@example.com", "access_token": "token"})
	if err != nil || email == nil || *email != "oauth@example.com" || credentials["email"] != "oauth@example.com" {
		t.Fatalf("matching identity = email %#v credentials %#v err %v", email, credentials, err)
	}

	credentialOnly, credentials, err := normalizeAccountIdentity(nil, map[string]string{"email": " Legacy@Example.COM ", "access_token": "token"})
	if err != nil || credentialOnly == nil || *credentialOnly != "legacy@example.com" || credentials["email"] != "legacy@example.com" {
		t.Fatalf("credential-only identity = email %#v credentials %#v err %v", credentialOnly, credentials, err)
	}

	mismatch := "other@example.com"
	if _, _, err := normalizeAccountIdentity(&topLevel, map[string]string{"email": mismatch}); !errors.Is(err, ErrAccountEmailMismatch) {
		t.Fatalf("mismatch error = %v, want ErrAccountEmailMismatch", err)
	}

	empty := "  "
	email, credentials, err = normalizeAccountIdentity(&empty, map[string]string{"email": "", "api_key": "sk"})
	if err != nil || email != nil || credentials["api_key"] != "sk" {
		t.Fatalf("empty email normalization = email %#v credentials %#v err %v", email, credentials, err)
	}
	if _, ok := credentials["email"]; ok {
		t.Fatalf("empty credential email should be removed: %#v", credentials)
	}
}

func TestNormalizeAccountIdentityUpdateMaintainsMirrors(t *testing.T) {
	currentEmail := "current@example.com"
	current := Account{
		Email:       &currentEmail,
		Credentials: map[string]string{"email": currentEmail, "refresh_token": "old"},
	}

	newEmail := " New@Example.COM "
	updated, err := normalizeAccountIdentityUpdate(current, UpdateInput{Email: &newEmail, HasEmail: true})
	if err != nil || updated.Email == nil || *updated.Email != "new@example.com" || updated.Credentials["email"] != "new@example.com" || updated.Credentials["refresh_token"] != "old" {
		t.Fatalf("top-level update = %+v err %v", updated, err)
	}

	updated, err = normalizeAccountIdentityUpdate(current, UpdateInput{Credentials: map[string]string{"access_token": "new"}})
	if err != nil || updated.Email == nil || *updated.Email != currentEmail || updated.Credentials["email"] != currentEmail {
		t.Fatalf("credentials without email = %+v err %v", updated, err)
	}

	updated, err = normalizeAccountIdentityUpdate(current, UpdateInput{Credentials: map[string]string{"email": "credential@example.com"}})
	if err != nil || updated.Email == nil || *updated.Email != "credential@example.com" || updated.Credentials["email"] != "credential@example.com" {
		t.Fatalf("credentials email update = %+v err %v", updated, err)
	}

	updated, err = normalizeAccountIdentityUpdate(current, UpdateInput{HasEmail: true})
	if err != nil || updated.Email != nil {
		t.Fatalf("clear email update = %+v err %v", updated, err)
	}
	if _, ok := updated.Credentials["email"]; ok {
		t.Fatalf("cleared identity still has credentials.email: %#v", updated.Credentials)
	}

	if _, err := normalizeAccountIdentityUpdate(current, UpdateInput{
		Email:       &newEmail,
		HasEmail:    true,
		Credentials: map[string]string{"email": "different@example.com"},
	}); !errors.Is(err, ErrAccountEmailMismatch) {
		t.Fatalf("update mismatch error = %v, want ErrAccountEmailMismatch", err)
	}
	if _, err := normalizeAccountIdentityUpdate(current, UpdateInput{
		HasEmail:    true,
		Credentials: map[string]string{"email": "different@example.com"},
	}); !errors.Is(err, ErrAccountEmailMismatch) {
		t.Fatalf("clear/update mismatch error = %v, want ErrAccountEmailMismatch", err)
	}
}
