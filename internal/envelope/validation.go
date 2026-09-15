package envelope

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrMissingID            = errors.New("missing id")
	ErrMissingIssuer        = errors.New("missing issuer.agent")
	ErrMissingRecipient     = errors.New("missing recipient.agent")
	ErrMissingPurpose       = errors.New("missing purpose")
	ErrMissingPolicyVersion = errors.New("missing policy_version")
	ErrMissingExpiresAt     = errors.New("missing expires_at")
	ErrExpired              = errors.New("envelope expired")
)

// ValidateStructure checks that required fields are present. It does not
// check the signature or expiration — see Verify and ValidateExpiration.
func ValidateStructure(e Envelope) error {
	if e.ID == "" {
		return ErrMissingID
	}
	if e.Issuer.Agent == "" {
		return ErrMissingIssuer
	}
	if e.Recipient.Agent == "" {
		return ErrMissingRecipient
	}
	if e.Purpose == "" {
		return ErrMissingPurpose
	}
	if e.PolicyVersion == "" {
		return ErrMissingPolicyVersion
	}
	if e.ExpiresAt.IsZero() {
		return ErrMissingExpiresAt
	}
	return nil
}

// ValidateExpiration checks that e.ExpiresAt has not passed relative to now.
func ValidateExpiration(e Envelope, now time.Time) error {
	if e.ExpiresAt.Before(now) {
		return fmt.Errorf("%w: expired at %s", ErrExpired, e.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}
