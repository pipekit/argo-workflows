package dag

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	// taskNameRegex matches task names in depends logic.
	// Group 1: taskName.Result (e.g., "task.Succeeded")
	// Group 2: taskName (bare task name, e.g., "task")
	taskNameRegex = regexp.MustCompile(`([a-zA-Z0-9\[\]\.\-_]+?\.[A-Z][a-zA-Z]+)|([a-zA-Z0-9\[\]\.\-_]+)`)
)

// resolveDependencies parses the depends logic and extracts dependencies.
// It returns the list of unique dependency names and the normalized depends expression.
func resolveDependencies(logic string, taskProvider func(string) Task) ([]string, string) {
	dependencySet := make(map[string]struct{})

	newLogic := taskNameRegex.ReplaceAllStringFunc(logic, func(match string) string {
		// Use FindStringSubmatch to determine which group matched
		groups := taskNameRegex.FindStringSubmatch(match)

		if groups[1] != "" {
			// Group 1: taskName.Result (e.g., "task.Succeeded")
			// We need to extract the task name and normalize it.
			lastDot := strings.LastIndex(match, ".")
			if lastDot != -1 {
				taskName := match[:lastDot]
				result := match[lastDot+1:]
				
				dependencySet[taskName] = struct{}{}
				return fmt.Sprintf("%s.%s", normalizeTaskName(taskName), result)
			}
			return match // Fallback (should not happen with this regex)
		}

		// Group 2: bare taskName (e.g., "task")
		taskName := match
		dependencySet[taskName] = struct{}{}
		
		task := taskProvider(taskName)
		return expandDependency(taskName, task)
	})

	// Convert set to sorted slice
	deps := make([]string, 0, len(dependencySet))
	for dep := range dependencySet {
		deps = append(deps, dep)
	}
	sort.Strings(deps)

	return deps, newLogic
}

// expandDependency expands a bare task name to a full depends expression.
func expandDependency(depName string, depTask Task) string {
	normalizedName := normalizeTaskName(depName)
	resultForTask := func(result string) string { return fmt.Sprintf("%s.%s", normalizedName, result) }

	taskDepends := []string{
		resultForTask("Succeeded"),
		resultForTask("Skipped"),
		resultForTask("Daemoned"),
	}

	if depTask != nil {
		continueOn := depTask.GetContinueOn()
		if continueOn != nil {
			if continueOn.Error {
				taskDepends = append(taskDepends, resultForTask("Errored"))
			}
			if continueOn.Failed {
				taskDepends = append(taskDepends, resultForTask("Failed"))
			}
		}
	}

	return "(" + strings.Join(taskDepends, " || ") + ")"
}

// normalizeTaskName normalizes a task name to be a valid identifier in expressions.
func normalizeTaskName(name string) string {
	r := strings.NewReplacer("-", "_", ".", "_", "[", "_", "]", "_")
	return r.Replace(name)
}