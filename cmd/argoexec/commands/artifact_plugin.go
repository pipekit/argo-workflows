package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/argoproj/pkg/stats"
	"github.com/spf13/cobra"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/logging"
	"github.com/argoproj/argo-workflows/v3/workflow/executor/osspecific"
)

func NewArtifactPluginCommand() *cobra.Command {
	var artifactPlugin string
	command := cobra.Command{
		Use:   "artifact-plugin",
		Short: "Load artifacts from an artifact plugin only",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := logging.RequireLoggerFromContext(ctx)

			name, args := args[0], args[1:]
			logger.WithFields(logging.Fields{"name": name, "args": args}).Debug(ctx, "starting command")

			go func() {
				command, closer, err := startCommand(ctx, name, args, template)
				if err != nil {
					logger.WithError(err).Error(ctx, "failed to start command")
					return
				}
				defer closer()
				// setup signal handlers
				signals := make(chan os.Signal, 1)
				defer close(signals)
				signal.Notify(signals)
				defer signal.Reset()

				go func() {
					for s := range signals {
						if osspecific.CanIgnoreSignal(s) {
							logger.WithField("signal", s).Debug(ctx, "ignore signal")
							continue
						}

						logger.WithField("signal", s).Debug(ctx, "forwarding signal")
						_ = osspecific.Kill(command.Process.Pid, s.(syscall.Signal))
					}
				}()
				//				pid := command.Process.Pid
				//				ctx, cancel := context.WithCancel(ctx)
				//				defer cancel()
			}()
			err := loadArtifactPlugin(cmd.Context(), artifactPlugin)
			if err != nil {
				return fmt.Errorf("%+v", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&artifactPlugin, "plugin-name", "", "Artifact plugin name")
	return &command
}

func loadArtifactPlugin(ctx context.Context, pluginName string) error {
	wfExecutor := initExecutor(ctx)
	defer wfExecutor.HandleError(ctx)
	defer stats.LogStats()

	err := wfExecutor.LoadArtifactsFromPlugin(ctx, wfv1.ArtifactPluginName(pluginName))
	if err != nil {
		wfExecutor.AddError(ctx, err)
		return err
	}
	return nil
}
