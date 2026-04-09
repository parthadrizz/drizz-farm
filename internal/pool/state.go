package pool

import "fmt"

// EmulatorState represents the lifecycle state of an emulator instance.
type EmulatorState int

const (
	StateCreating   EmulatorState = iota // AVD being created
	StateBooting                         // Emulator process starting
	StateWarm                            // Ready for allocation
	StateAllocated                       // Locked to a session
	StateResetting                       // Restoring clean snapshot
	StateDestroying                      // Being killed and cleaned up
	StateError                           // Unhealthy, pending recovery
)

var stateNames = map[EmulatorState]string{
	StateCreating:   "creating",
	StateBooting:    "booting",
	StateWarm:       "warm",
	StateAllocated:  "allocated",
	StateResetting:  "resetting",
	StateDestroying: "destroying",
	StateError:      "error",
}

func (s EmulatorState) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", int(s))
}

// validTransitions defines allowed state transitions.
var validTransitions = map[EmulatorState][]EmulatorState{
	StateCreating:   {StateBooting, StateError, StateDestroying},
	StateBooting:    {StateWarm, StateError, StateDestroying},
	StateWarm:       {StateAllocated, StateDestroying, StateError},
	StateAllocated:  {StateResetting, StateDestroying, StateError},
	StateResetting:  {StateWarm, StateError, StateDestroying},
	StateError:      {StateDestroying, StateBooting, StateResetting},
	StateDestroying: {}, // Terminal state
}

// CanTransitionTo checks if a transition from current state to target is valid.
func (s EmulatorState) CanTransitionTo(target EmulatorState) bool {
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
func (s EmulatorState) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *EmulatorState) UnmarshalJSON(data []byte) error {
	str := string(data)
	// Strip quotes
	if len(str) >= 2 && str[0] == '"' {
		str = str[1 : len(str)-1]
	}
	for state, name := range stateNames {
		if name == str {
			*s = state
			return nil
		}
	}
	return fmt.Errorf("unknown emulator state: %s", str)
}
