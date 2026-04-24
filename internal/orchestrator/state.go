package orchestrator

type State string

const (
	StatePlanning  State = "planning"
	StateCoding    State = "coding"
	StateVerifying State = "verifying"
	StateReviewing State = "reviewing"
	StateRevising  State = "revising"
	StateConverged State = "converged"
	StateBlocked   State = "blocked"
)

// TerminalStates are states where the task loop exits.
var TerminalStates = map[State]bool{
	StateConverged: true,
	StateBlocked:   true,
}
