package commands

import (
	"context"
	"fmt"

	"github.com/argoproj/pkg/stats"
	"github.com/spf13/cobra"

	"github.com/argoproj/argo-workflows/v3/workflow/executor/tracing"
)

func NewInitCommand() *cobra.Command {
	command := cobra.Command{
		Use:   "init",
		Short: "Load artifacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := tracing.InjectTraceContext(context.Background())
			err := loadArtifacts(ctx)
			if err != nil {
				return fmt.Errorf("%+v", err)
			}
			return nil
		},
	}
	return &command
}

func loadArtifacts(ctx context.Context) error {
	wfExecutor := initExecutor()
	ctx, span := wfExecutor.Tracing.Tracing.Tracer.Start(ctx, "init-container")
	defer span.End()
	defer wfExecutor.HandleError(ctx)
	defer stats.LogStats()

	if err := wfExecutor.Init(); err != nil {
		wfExecutor.AddError(err)
		return err
	}
	// Download input artifacts
	err := wfExecutor.StageFiles()
	if err != nil {
		wfExecutor.AddError(err)
		return err
	}
	err = wfExecutor.LoadArtifacts(ctx)
	if err != nil {
		wfExecutor.AddError(err)
		return err
	}
	return nil
}
