package archivev2

import (
	"github.com/spf13/cobra"
)

func NewArchiveCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "archivev2",
		Short: "manage the workflow archive",
		Long: `Manage archived workflows. Archive commands mirror the standard workflow commands
but operate on workflows stored in the archive database.

Workflows are identified by name (unlike 'archive' which uses UID).`,
		Example: `# List archived workflows:
  argo archivev2 list

# Get an archived workflow by name:
  argo archivev2 get my-workflow

# Delete an archived workflow:
  argo archivev2 delete my-workflow

# Resubmit an archived workflow:
  argo archivev2 resubmit my-workflow --wait

# Retry a failed archived workflow:
  argo archivev2 retry my-workflow
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	command.AddCommand(NewDeleteCommand())
	command.AddCommand(NewGetCommand())
	command.AddCommand(NewListCommand())
	command.AddCommand(NewListLabelKeysCommand())
	command.AddCommand(NewListLabelValuesCommand())
	command.AddCommand(NewResubmitCommand())
	command.AddCommand(NewRetryCommand())

	return command
}
