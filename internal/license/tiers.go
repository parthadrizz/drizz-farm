package license

import "time"

// Tier represents a subscription tier.
type Tier string

const (
	TierStarter    Tier = "starter"
	TierPro        Tier = "pro"
	TierEnterprise Tier = "enterprise"
)

// TierLimits defines the limits for a tier.
type TierLimits struct {
	MaxEmulators      int           // -1 = unlimited
	MaxNodes          int           // -1 = unlimited
	MaxSeats          int           // -1 = unlimited
	MaxSessionMinutes int           // -1 = unlimited
	Features          []string
}

var tierLimits = map[Tier]TierLimits{
	TierStarter: {
		MaxEmulators:      2,
		MaxNodes:          1,
		MaxSeats:          2,
		MaxSessionMinutes: 30,
		Features:          []string{"android", "basic_logs", "screenshots", "dashboard_cli"},
	},
	TierPro: {
		MaxEmulators:      -1,
		MaxNodes:          -1,
		MaxSeats:          -1,
		MaxSessionMinutes: -1,
		Features: []string{
			"android", "ios", "federation",
			"network_sim", "video", "har", "crash_dumps",
			"analytics", "dashboard", "interactive_sessions",
			"session_sharing", "visual_baseline",
			"device_features_full", "ci_cd_all",
		},
	},
	TierEnterprise: {
		MaxEmulators:      -1,
		MaxNodes:          -1,
		MaxSeats:          -1,
		MaxSessionMinutes: -1,
		Features:          []string{"*"},
	},
}

// License represents a validated license.
type License struct {
	Key       string    `json:"key"`
	Tier      Tier      `json:"tier"`
	ExpiresAt time.Time `json:"expires_at"`
	Limits    TierLimits `json:"limits"`
}

// IsUnlimited returns true if a limit value means unlimited.
func IsUnlimited(limit int) bool {
	return limit == -1
}

// HasFeature checks if a license includes a specific feature.
func (l *License) HasFeature(feature string) bool {
	for _, f := range l.Limits.Features {
		if f == "*" || f == feature {
			return true
		}
	}
	return false
}

// IsExpired returns true if the license has expired.
func (l *License) IsExpired() bool {
	return time.Now().After(l.ExpiresAt)
}
