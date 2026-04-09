package pool

import "fmt"

// DeviceState represents the lifecycle state of a device instance.
type DeviceState int

const (
	StateCreating   DeviceState = iota // Being set up (AVD creation, verification)
	StateBooting                       // Starting up (emulator boot, device coming online)
	StateWarm                          // Ready for allocation
	StateAllocated                     // Locked to a session
	StateResetting                     // Cleaning up after session
	StateDestroying                    // Being shut down / removed
	StateError                         // Unhealthy, pending recovery
)

var stateNames = map[DeviceState]string{
	StateCreating:   "creating",
	StateBooting:    "booting",
	StateWarm:       "warm",
	StateAllocated:  "allocated",
	StateResetting:  "resetting",
	StateDestroying: "destroying",
	StateError:      "error",
}

func (s DeviceState) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", int(s))
}

// validTransitions defines allowed state transitions.
var validTransitions = map[DeviceState][]DeviceState{
	StateCreating:   {StateBooting, StateWarm, StateError, StateDestroying},
	StateBooting:    {StateWarm, StateError, StateDestroying},
	StateWarm:       {StateAllocated, StateDestroying, StateError},
	StateAllocated:  {StateResetting, StateDestroying, StateError},
	StateResetting:  {StateWarm, StateError, StateDestroying},
	StateError:      {StateDestroying, StateBooting, StateResetting, StateWarm},
	StateDestroying: {},
}

// CanTransitionTo checks if a transition from current state to target is valid.
func (s DeviceState) CanTransitionTo(target DeviceState) bool {
	allowed, ok := validTransitions[s]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == target {
			return true
		}
	}
	return false
}

// MarshalJSON implements json.Marshaler.
func (s DeviceState) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *DeviceState) UnmarshalJSON(data []byte) error {
	str := string(data)
	if len(str) >= 2 && str[0] == '"' {
		str = str[1 : len(str)-1]
	}
	for state, name := range stateNames {
		if name == str {
			*s = state
			return nil
		}
	}
	return fmt.Errorf("unknown device state: %s", str)
}
