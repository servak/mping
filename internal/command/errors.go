package command

// ExitError carries a specific process exit code alongside an error.
// It lets automation distinguish "probing worked but targets were unhealthy"
// from usage or configuration errors (which exit with status 1).
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

// ExitCodeThresholdExceeded is returned when targets violate a health threshold
const ExitCodeThresholdExceeded = 2
