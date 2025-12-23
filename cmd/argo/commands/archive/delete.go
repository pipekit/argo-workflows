package archive

import (
	"fmt"

	"github.com/spf13/cobra"

	client "github.com/argoproj/argo-workflows/v3/cmd/argo/commands/client"
	workflowarchivepkg "github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflowarchive"
)

func NewDeleteCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "delete WORKFLOW...",
		Short: "delete a workflow in the archive",
		Example: `# Delete an archived workflow by name:
  argo archive delete my-workflow

# Delete an archived workflow by UID (auto-detected):
  argo archive delete a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11

# Delete multiple archived workflows:
  argo archive delete my-workflow my-other-workflow
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
			for _, identifier := range args {
				var req *workflowarchivepkg.DeleteArchivedWorkflowRequest
				if isUID(identifier) {
					req = &workflowarchivepkg.DeleteArchivedWorkflowRequest{Uid: identifier}
				} else {
					req = &workflowarchivepkg.DeleteArchivedWorkflowRequest{
						Name:      identifier,
						Namespace: namespace,
					}
				}
				if _, err = serviceClient.DeleteArchivedWorkflow(ctx, req); err != nil {
					return err
				}
				fmt.Printf("Archived workflow '%s' deleted\n", identifier)
			}
			return nil
		},
	}
	return command
}
