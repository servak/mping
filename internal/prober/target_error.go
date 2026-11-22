package prober

import (
	"fmt"
	"strings"
)

// TargetError says why a target cannot be probed: there is no prober for it,
// its prober rejects it, or its name cannot be resolved.
type TargetError struct {
	Target string // as given to AddTargets
	Err    error
}

func (e TargetError) Error() string {
	return fmt.Sprintf("failed to route target %s: %v", e.Target, e.Err)
}

func (e TargetError) Unwrap() error { return e.Err }

// TargetErrors are the errors of all targets that cannot be probed, in the
// order in which the targets were given, each target once. Its message lists
// only the first ones.
type TargetErrors []TargetError

// maxListedErrors is how many targets the message of TargetErrors lists.
const maxListedErrors = 10

func (e TargetErrors) Error() string {
	if len(e) == 1 {
		return e[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d targets failed", len(e))
	for i, te := range e {
		if i == maxListedErrors {
			fmt.Fprintf(&b, "\n  and %d more", len(e)-i)
			break
		}
		fmt.Fprintf(&b, "\n  %s: %v", te.Target, te.Err)
	}
	return b.String()
}

func (e TargetErrors) Unwrap() []error {
	errs := make([]error, len(e))
	for i, te := range e {
		errs[i] = te
	}
	return errs
}

// ProberError says why a prober cannot be created (e.g. because it may not
// open an ICMP socket or its source interface does not exist), whatever the
// targets are.
type ProberError struct {
	Prober string // its name in the config
	Err    error
}

func (e ProberError) Error() string {
	return fmt.Sprintf("failed to get prober for %s: %v", e.Prober, e.Err)
}

func (e ProberError) Unwrap() error { return e.Err }
