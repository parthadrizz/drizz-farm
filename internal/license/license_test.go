package license

import (
	"testing"
	"time"
)

func TestValidatorStubReturnsPro(t *testing.T) {
	v := NewValidator()
	lic, err := v.Validate("test-key")
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if lic.Tier != TierPro {
		t.Errorf("expected Pro tier, got %s", lic.Tier)
	}
	if !IsUnlimited(lic.Limits.MaxEmulators) {
		t.Errorf("expected unlimited emulators, got %d", lic.Limits.MaxEmulators)
	}
}

func TestValidatorDefaultsToStarter(t *testing.T) {
	v := NewValidator()
	lic := v.Current()
	if lic.Tier != TierStarter {
		t.Errorf("expected Starter tier, got %s", lic.Tier)
	}
	if lic.Limits.MaxEmulators != 2 {
		t.Errorf("expected 2 max emulators, got %d", lic.Limits.MaxEmulators)
	}
}

func TestCanCreateSession(t *testing.T) {
	v := NewValidator()

	// Starter: max 2
	if v.CanCreateSession(2) {
		t.Error("starter should not allow 3rd session")
	}
	if !v.CanCreateSession(1) {
		t.Error("starter should allow 2nd session")
	}

	// Pro: unlimited
	v.Validate("pro-key")
	if !v.CanCreateSession(100) {
		t.Error("pro should allow any number of sessions")
	}
}

func TestLicenseFeatureCheck(t *testing.T) {
	lic := &License{
		Tier:   TierPro,
		Limits: TierLimits{Features: []string{"android", "ios", "video"}},
	}

	if !lic.HasFeature("android") {
		t.Error("expected HasFeature(android) = true")
	}
	if lic.HasFeature("nonexistent") {
		t.Error("expected HasFeature(nonexistent) = false")
	}

	// Enterprise wildcard
	lic.Limits.Features = []string{"*"}
	if !lic.HasFeature("anything") {
		t.Error("expected wildcard to match any feature")
	}
}

func TestLicenseExpiry(t *testing.T) {
	lic := &License{ExpiresAt: time.Now().Add(-1 * time.Hour)}
	if !lic.IsExpired() {
		t.Error("expected expired license")
	}

	lic.ExpiresAt = time.Now().Add(1 * time.Hour)
	if lic.IsExpired() {
		t.Error("expected non-expired license")
	}
}
