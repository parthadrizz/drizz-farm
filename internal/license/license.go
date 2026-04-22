package license

import (
	"time"

	"github.com/rs/zerolog/log"
)

// Validator validates and caches license state.
type Validator struct {
	current *License
}

// NewValidator creates a new license validator.
// In development mode, it returns a Pro license stub.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate checks a license key. Currently stubs to Pro tier for development.
func (v *Validator) Validate(key string) (*License, error) {
	// TODO: Implement real license validation against license.drizz.ai
	// For now, return Pro tier to unblock development.
	log.Info().Str("tier", "pro").Msg("license: using development Pro license (stub)")

	v.current = &License{
		Key:       key,
		Tier:      TierPro,
		ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
		Limits:    tierLimits[TierPro],
	}
	return v.current, nil
}

// Current returns the currently validated license, or a default Starter license.
func (v *Validator) Current() *License {
	if v.current != nil {
		return v.current
	}

	// Default to starter tier if no license validated
	return &License{
		Key:       "",
		Tier:      TierStarter,
		ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
		Limits:    tierLimits[TierStarter],
	}
}

// CanCreateSession checks if the license allows creating another session.
func (v *Validator) CanCreateSession(currentEmulators int) bool {
	lic := v.Current()
	if lic.IsExpired() {
		return false
	}
	if IsUnlimited(lic.Limits.MaxEmulators) {
		return true
	}
	return currentEmulators < lic.Limits.MaxEmulators
}

// MaxSessionDuration returns the max session duration for the current tier.
func (v *Validator) MaxSessionDuration() time.Duration {
	lic := v.Current()
	if IsUnlimited(lic.Limits.MaxSessionMinutes) {
		return 0 // 0 means unlimited
	}
	return time.Duration(lic.Limits.MaxSessionMinutes) * time.Minute
}
