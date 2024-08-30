package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/argoproj/argo-workflows/v3/workflow/executor/tracing"
)

func NewDataCommand() *cobra.Command {
	command := cobra.Command{
		Use:   "data",
		Short: "Process data",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			err := execData(ctx)
			if err != nil {
				return fmt.Errorf("%+v", err)
			}
			return nil
		},
	}
	return &command
}

func execData(ctx context.Context) error {
	wfExecutor := initExecutor()
	ctx = tracing.InjectTraceContext(ctx)

	// Don't allow cancellation to impact capture of results, parameters, artifacts, or defers.
	bgCtx := tracing.InjectTraceContext(context.Background())
	// Create a new empty (placeholder) task result with LabelKeyReportOutputsCompleted set to false.
	wfExecutor.InitializeOutput(bgCtx)
	defer wfExecutor.HandleError(bgCtx)
	defer wfExecutor.FinalizeOutput(bgCtx) //Ensures the LabelKeyReportOutputsCompleted is set to true.

	err := wfExecutor.Data(ctx)
	if err != nil {
		wfExecutor.AddError(err)
		return err
	}
	return nil
}
