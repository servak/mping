package command

import "github.com/servak/mping/internal/target"

// ExpandTargets expands the targets of a file, if any, followed by args
func ExpandTargets(args []string, filePath string) ([]string, error) {
	targets, err := collectTargets(args, filePath)
	if err != nil {
		return nil, err
	}
	return target.Expand(targets, target.MaxTargets)
}

// collectTargets gathers targets from command-line arguments and optional file
func collectTargets(args []string, filePath string) ([]string, error) {
	targets := []string{}
	if filePath != "" {
		var err error
		if targets, err = target.ReadFile(filePath); err != nil {
			return nil, err
		}
	}
	return append(targets, args...), nil
}
