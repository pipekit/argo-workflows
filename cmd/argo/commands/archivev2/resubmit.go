package archivev2

import (
	"context"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-workflows/v3/cmd/argo/commands/client"
	"github.com/argoproj/argo-workflows/v3/cmd/argo/commands/common"
	workflowpkg "github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflow"
	workflowarchivepkg "github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflowarchive"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

type resubmitOps struct {
	priority      int32  // --priority
	memoized      bool   // --memoized
	namespace     string // --namespace
	labelSelector string // --selector
	fieldSelector string // --field-selector
}

// hasSelector returns true if the CLI arguments selects multiple workflows
func (o *resubmitOps) hasSelector() bool {
	return o.labelSelector != "" || o.fieldSelector != ""
}

func NewResubmitCommand() *cobra.Command {
	var (
		resubmitOpts  resubmitOps
		cliSubmitOpts = common.NewCliSubmitOpts()
	)

	command := &cobra.Command{
		Use:   "resubmit [WORKFLOW...]",
		Short: "resubmit one or more workflows",
		Example: `# Resubmit a workflow:
  argo archivev2 resubmit my-wf

# Resubmit multiple workflows:
  argo archivev2 resubmit my-wf another-wf

# Resubmit multiple workflows by label selector:
  argo archivev2 resubmit -l workflows.argoproj.io/test=true

# Resubmit multiple workflows by field selector:
  argo archivev2 resubmit --field-selector metadata.namespace=argo

# Resubmit and wait for completion:
  argo archivev2 resubmit --wait my-wf

# Resubmit and watch until completion:
  argo archivev2 resubmit --watch my-wf

# Resubmit and tail logs until completion:
  argo archivev2 resubmit --log my-wf
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flag("priority").Changed {
				cliSubmitOpts.Priority = &resubmitOpts.priority
			}

			ctx, apiClient, err := client.NewAPIClient(cmd.Context())
			if err != nil {
				return err
			}
			serviceClient := apiClient.NewWorkflowServiceClient(ctx) // needed for wait watch or log flags
			archiveServiceClient, err := apiClient.NewArchivedWorkflowServiceClient()
			if err != nil {
				return err
			}
			resubmitOpts.namespace = client.Namespace(ctx)
			return resubmitArchivedWorkflows(ctx, archiveServiceClient, serviceClient, resubmitOpts, cliSubmitOpts, args)
		},
	}

	command.Flags().StringArrayVarP(&cliSubmitOpts.Parameters, "parameter", "p", []string{}, "input parameter to override on the original workflow spec")
	command.Flags().Int32Var(&resubmitOpts.priority, "priority", 0, "workflow priority")
	command.Flags().VarP(&cliSubmitOpts.Output, "output", "o", "Output format. "+cliSubmitOpts.Output.Usage())
	command.Flags().BoolVarP(&cliSubmitOpts.Wait, "wait", "w", false, "wait for the workflow to complete, only works when a single workflow is resubmitted")
	command.Flags().BoolVar(&cliSubmitOpts.Watch, "watch", false, "watch the workflow until it completes, only works when a single workflow is resubmitted")
	command.Flags().BoolVar(&cliSubmitOpts.Log, "log", false, "log the workflow until it completes")
	command.Flags().BoolVar(&resubmitOpts.memoized, "memoized", false, "re-use successful steps & outputs from the previous run")
	command.Flags().StringVarP(&resubmitOpts.labelSelector, "selector", "l", "", "Selector (label query) to filter on, not including uninitialized ones, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	command.Flags().StringVar(&resubmitOpts.fieldSelector, "field-selector", "", "Selector (field query) to filter on, supports '=', '==', and '!='.(e.g. --field-selector key1=value1,key2=value2). The server only supports a limited number of field queries per type.")
	return command
}

// resubmitArchivedWorkflows resubmits workflows by given resubmitOpts or workflow names
func resubmitArchivedWorkflows(ctx context.Context, archiveServiceClient workflowarchivepkg.ArchivedWorkflowServiceClient, serviceClient workflowpkg.WorkflowServiceClient, resubmitOpts resubmitOps, cliSubmitOpts common.CliSubmitOpts, args []string) error {
	var (
		wfs wfv1.Workflows
		err error
	)

	if resubmitOpts.hasSelector() {
		wfs, err = listArchivedWorkflows(ctx, archiveServiceClient, resubmitOpts.namespace, resubmitOpts.labelSelector, 0)
		if err != nil {
			return err
		}
	}

	for _, name := range args {
		wfs = append(wfs, wfv1.Workflow{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: resubmitOpts.namespace,
			},
		})
	}

	var lastResubmitted *wfv1.Workflow
	resubmittedNames := make(map[string]bool)

	for _, wf := range wfs {
		key := wf.Namespace + "/" + wf.Name
		if resubmittedNames[key] {
			// de-duplication in case there is an overlap between the selector and given workflow names
			continue
		}
		resubmittedNames[key] = true

		lastResubmitted, err = archiveServiceClient.ResubmitArchivedWorkflow(ctx, &workflowarchivepkg.ResubmitArchivedWorkflowRequest{
			Namespace:  wf.Namespace,
			Name:       wf.Name,
			Memoized:   resubmitOpts.memoized,
			Parameters: cliSubmitOpts.Parameters,
		})
		if err != nil {
			return err
		}
		printWorkflow(lastResubmitted, cliSubmitOpts.Output.String())
	}

	if len(resubmittedNames) == 1 {
		// watch or wait when there is only one workflow retried
		return common.WaitWatchOrLog(ctx, serviceClient, lastResubmitted.Namespace, []string{lastResubmitted.Name}, cliSubmitOpts)
	}
	return nil
}
