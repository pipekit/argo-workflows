package archivev2

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/argoproj/argo-workflows/v3/cmd/argo/commands/client"
	workflowarchivepkg "github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflowarchive"
)

// NewDeleteCommand returns a new instance of an `argo archivev2 delete` command
func NewDeleteCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "delete WORKFLOW...",
		Short: "delete a workflow in the archive",
		Example: `# Delete an archived workflow by name:
  argo archivev2 delete my-wf

# Delete multiple archived workflows:
  argo archivev2 delete my-wf my-other-wf
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, apiClient, err := client.NewAPIClient(cmd.Context())
			if err != nil {
				return err
			}
			serviceClient, err := apiClient.NewArchivedWorkflowServiceClient()
			if err != nil {
				return err
			}
			namespace := client.Namespace(ctx)
			for _, name := range args {
				if _, err = serviceClient.DeleteArchivedWorkflow(ctx, &workflowarchivepkg.DeleteArchivedWorkflowRequest{
					Name:      name,
					Namespace: namespace,
				}); err != nil {
					return err
				}
				fmt.Printf("Archived workflow '%s' deleted\n", name)
			}
			return nil
		},
	}
	return command
}
