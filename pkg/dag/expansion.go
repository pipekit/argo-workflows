package dag

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/expr/argoexpr"
	"github.com/argoproj/argo-workflows/v3/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// expandTask expands a single DAG task containing withItems, withParams, withSequence into multiple parallel tasks
// We want to be lazy with expanding. Unfortunately this is not quite possible as the When field might rely on
// expansion to work with the shouldExecute function. To address this we apply a trick, we try to expand, if we fail, we then
// check shouldExecute, if shouldExecute returns false, we continue on as normal else error out
func (e *DAGEvaluator) ExpandTask(ctx context.Context, task wfv1.DAGTask, scope map[string]string, substitutor Substitutor) ([]wfv1.DAGTask, error) {
	var err error
	var items []wfv1.Item
	if len(task.WithItems) > 0 {
		items = task.WithItems
	} else if task.WithParam != "" {
		resolvedParam, err := substitutor.Substitute(task.WithParam, scope)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal([]byte(resolvedParam), &items)
		if err != nil {
			mustExec, mustExecErr := shouldExecute(task.When)
			if mustExecErr != nil || mustExec {
				return nil, errors.Errorf(errors.CodeBadRequest, "withParam value could not be parsed as a JSON list: %s: %v", strings.TrimSpace(resolvedParam), err)
			}
		}
	} else if task.WithSequence != nil {
		items, err = expandSequence(task.WithSequence)
		if err != nil {
			mustExec, mustExecErr := shouldExecute(task.When)
			if mustExecErr != nil || mustExec {
				return nil, err
			}
		}
	} else {
		return []wfv1.DAGTask{task}, nil
	}

	// these fields can be very large (>100m) and marshalling 10k x 100m = 6GB of memory used and
	// very poor performance, so we just nil them out
	task.WithItems = nil
	task.WithParam = ""
	task.WithSequence = nil

	taskBytes, err := json.Marshal(task)
	if err != nil {
		return nil, errors.InternalWrapError(err)
	}

	expandedTasks := make([]wfv1.DAGTask, 0)
	for i, item := range items {
		var newTask wfv1.DAGTask
		newTaskName, err := processItem(ctx, taskBytes, task.Name, i, item, &newTask, task.When, scope, substitutor)
		if err != nil {
			return nil, err
		}
		if newTaskName == "" {
			continue
		}
		newTask.Name = newTaskName
		newTask.Template = task.Template
		expandedTasks = append(expandedTasks, newTask)
	}
	return expandedTasks, nil
}

func shouldExecute(when string) (bool, error) {
	if when == "" {
		return true, nil
	}
	return argoexpr.EvalBool(when, nil)
}

func expandSequence(seq *wfv1.Sequence) ([]wfv1.Item, error) {
	if seq == nil {
		return nil, nil
	}

	var start, end, count int64
	var err error

	if seq.Start != nil {
		if seq.Start.Type == intstr.Int {
			start = int64(seq.Start.IntValue())
		} else {
			start, err = strconv.ParseInt(seq.Start.String(), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse sequence start: %w", err)
			}
		}
	}

	if seq.End != nil {
		if seq.End.Type == intstr.Int {
			end = int64(seq.End.IntValue())
		} else {
			end, err = strconv.ParseInt(seq.End.String(), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse sequence end: %w", err)
			}
		}
	} else {
		return nil, nil
	}

	if start > end {
		return nil, nil
	}

	numElements := end - start + 1

	if seq.Count != nil {
		if seq.Count.Type == intstr.Int {
			count = int64(seq.Count.IntValue())
		} else {
			count, err = strconv.ParseInt(seq.Count.String(), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse sequence count: %w", err)
			}
		}
		if count < numElements {
			numElements = count
		}
	}

	var items []wfv1.Item
	for i := int64(0); i < numElements; i++ {
		val := start + i
		var raw json.RawMessage
		var err error
		if seq.Format != "" {
			strVal := fmt.Sprintf(seq.Format, val)
			raw, err = json.Marshal(strVal)
		} else {
			raw, err = json.Marshal(val)
		}
		if err != nil {
			return nil, err
		}
		items = append(items, wfv1.Item{
			Value: raw,
		})
	}

	return items, nil
}

func processItem(ctx context.Context, taskBytes []byte, taskName string, i int, item wfv1.Item, newTask *wfv1.DAGTask, when string, globalScope map[string]string, substitutor Substitutor) (string, error) {
	var newTaskName string

	err := json.Unmarshal(taskBytes, newTask)
	if err != nil {
		return "", errors.InternalWrapError(err)
	}

	if substitutor != nil {
		substScope := make(map[string]string)
		for k, v := range globalScope {
			substScope[k] = v
		}
		var raw interface{}
		err := json.Unmarshal(item.Value, &raw)
		if err != nil {
			// fallback to just the raw value
			substScope["item"] = string(item.Value)
		} else {
			switch v := raw.(type) {
			case string:
				substScope["item"] = v
			case float64:
				substScope["item"] = strconv.FormatFloat(v, 'f', -1, 64)
			case bool:
				substScope["item"] = strconv.FormatBool(v)
			default:
				// For lists and maps, we use the raw JSON
				substScope["item"] = string(item.Value)
			}
		}
		substScope["index"] = strconv.Itoa(i) // Marshal the new task, substitute, and unmarshal back
		taskJSON, err := json.Marshal(newTask)
		if err != nil {
			return "", errors.InternalWrapError(err)
		}
		substituted, err := substitutor.Substitute(string(taskJSON), substScope)
		if err != nil {
			return "", err
		}
		err = json.Unmarshal([]byte(substituted), newTask)
		if err != nil {
			return "", errors.InternalWrapError(err)
		}
	}

	if newTask.Name != "" && newTask.Name != taskName {
		newTaskName = newTask.Name
	} else {
		var itemStr string
		var raw interface{}
		err := json.Unmarshal(item.Value, &raw)
		if err != nil {
			itemStr = string(item.Value)
		} else {
			switch v := raw.(type) {
			case string:
				itemStr = v
			case float64:
				itemStr = strconv.FormatFloat(v, 'f', -1, 64)
			case bool:
				itemStr = strconv.FormatBool(v)
			default:
				itemStr = string(item.Value)
			}
		}
		if item.Value != nil {
			newTaskName = fmt.Sprintf("%s(%d:%s)", taskName, i, itemStr)
		} else {
			newTaskName = fmt.Sprintf("%s(%d)", taskName, i)
		}
	}

	// 'when' clause would also need substitution
	// We need to substitute when before evaluation
	if newTask.When != "" {
		when = newTask.When
	}
	proceed, err := shouldExecute(when)
	if err != nil {
		return "", err
	}
	if !proceed {
		return "", nil
	}

	return newTaskName, nil
}
