package archive

import (
	"github.com/spf13/cobra"
)

func NewArchiveCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "archive",
		Short: "manage the workflow archive - deprecated, use `archivev2`",
		Long:  "NOTE: `archive` command is deprecated in order for the archive commands to mirror non-archive commands, please use `archivev2`. ",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	command.AddCommand(NewListCommand())
	command.AddCommand(NewGetCommand())
	command.AddCommand(NewDeleteCommand())
	command.AddCommand(NewListLabelKeyCommand())
	command.AddCommand(NewListLabelValueCommand())
	command.AddCommand(NewResubmitCommand())
	command.AddCommand(NewRetryCommand())
	return command
}
