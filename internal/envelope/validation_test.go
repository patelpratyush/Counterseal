package envelope

import (
	"errors"
	"testing"
	"time"
)

func validEnvelope() Envelope {
	return Envelope{
		ID:            "env_1",
		Version:       "1",
		Issuer:        Party{Agent: "support-agent"},
		Recipient:     Party{Agent: "billing-agent"},
		Purpose:       "customer_support_refund",
		PolicyVersion: "v1",
		ExpiresAt:     time.Now().Add(time.Hour).UTC(),
	}
}

func TestValidateStructurePasses(t *testing.T) {
	if err := ValidateStructure(validEnvelope()); err != nil {
		t.Fatalf("expected valid envelope to pass, got: %v", err)
	}
}

func TestValidateStructureCatchesMissingFields(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(e *Envelope)
		wantErr error
	}{
		{"missing id", func(e *Envelope) { e.ID = "" }, ErrMissingID},
		{"missing issuer", func(e *Envelope) { e.Issuer.Agent = "" }, ErrMissingIssuer},
		{"missing recipient", func(e *Envelope) { e.Recipient.Agent = "" }, ErrMissingRecipient},
		{"missing purpose", func(e *Envelope) { e.Purpose = "" }, ErrMissingPurpose},
		{"missing policy version", func(e *Envelope) { e.PolicyVersion = "" }, ErrMissingPolicyVersion},
		{"missing expires_at", func(e *Envelope) { e.ExpiresAt = time.Time{} }, ErrMissingExpiresAt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := validEnvelope()
			c.mutate(&e)
			err := ValidateStructure(e)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("expected %v, got %v", c.wantErr, err)
			}
		})
	}
}

func TestValidateExpirationRejectsPast(t *testing.T) {
	e := validEnvelope()
	e.ExpiresAt = time.Now().Add(-time.Hour).UTC()
	err := ValidateExpiration(e, time.Now())
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestValidateExpirationAcceptsFuture(t *testing.T) {
	e := validEnvelope()
	if err := ValidateExpiration(e, time.Now()); err != nil {
		t.Fatalf("expected no error for future expiry, got %v", err)
	}
}

func TestExpirationBoundary(t *testing.T) {
	e := validEnvelope()
	for _, delta := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
		err := ValidateExpiration(e, e.ExpiresAt.Add(delta))
		if delta < 0 && err != nil {
			t.Fatalf("rejected before expiry: %v", err)
		}
		if delta >= 0 && !errors.Is(err, ErrExpired) {
			t.Fatalf("accepted at/after expiry: %v", err)
		}
	}
}
