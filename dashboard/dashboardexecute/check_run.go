package dashboardexecute

import (
	"context"
	"fmt"
	"reflect"

	"github.com/turbot/steampipe/control/controlexecute"
	"github.com/turbot/steampipe/control/controlhooks"
	"github.com/turbot/steampipe/dashboard/dashboardevents"
	"github.com/turbot/steampipe/dashboard/dashboardinterfaces"
	"github.com/turbot/steampipe/steampipeconfig/modconfig"
)

// CheckRun is a struct representing the execution of a leaf dashboard node
type CheckRun struct {
	Name                 string                        `json:"name"`
	Title                string                        `json:"title,omitempty"`
	Width                int                           `json:"width,omitempty"`
	Error                error                         `json:"error,omitempty"`
	NodeType             string                        `json:"node_type"`
	ControlExecutionTree *controlexecute.ExecutionTree `json:"execution_tree"`
	DashboardName        string                        `json:"dashboard"`
	DashboardNode        modconfig.DashboardLeafNode   `json:"-"`
	parent               dashboardinterfaces.DashboardNodeParent
	runStatus            dashboardinterfaces.DashboardRunStatus
	executionTree        *DashboardExecutionTree
}

func NewCheckRun(resource modconfig.DashboardLeafNode, parent dashboardinterfaces.DashboardNodeParent, executionTree *DashboardExecutionTree) (*CheckRun, error) {

	// NOTE: for now we MUST declare container/dashboard children inline - therefore we cannot share children between runs in the tree
	// (if we supported the children property then we could reuse resources)
	// so FOR NOW it is safe to use the node name directly as the run name
	name := resource.Name()

	r := &CheckRun{
		Name:          name,
		Title:         resource.GetTitle(),
		Width:         resource.GetWidth(),
		DashboardNode: resource,
		DashboardName: executionTree.dashboardName,
		executionTree: executionTree,
		parent:        parent,

		// set to complete, optimistically
		// if any children have SQL we will set this to DashboardRunReady instead
		runStatus: dashboardinterfaces.DashboardRunComplete,
	}
	// verify node type
	switch resource.(type) {
	case *modconfig.Control:
		r.NodeType = modconfig.BlockTypeControl
	case *modconfig.Benchmark:
		r.NodeType = modconfig.BlockTypeBenchmark
	default:
		return nil, fmt.Errorf("check run instantiated with invalid node type %s", reflect.TypeOf(resource).Name())
	}

	//  set status to ready
	r.runStatus = dashboardinterfaces.DashboardRunReady

	// add r into execution tree
	executionTree.runs[r.Name] = r
	return r, nil
}

// Execute implements DashboardRunNode
func (r *CheckRun) Execute(ctx context.Context) error {
	executionTree, err := controlexecute.NewExecutionTree(ctx, r.executionTree.workspace, r.executionTree.client, r.DashboardNode.Name())
	if err != nil {
		// set the error status on the counter - this will raise counter error event
		r.SetError(err)
		return err
	}
	// create a context with a ControlEventHooks to report control execution progress
	ctx = controlhooks.AddControlHooksToContext(ctx, NewControlEventHooks(r))
	r.ControlExecutionTree = executionTree
	executionTree.Execute(ctx)

	// set complete status on counter - this will raise counter complete event
	r.SetComplete()

	return nil
}

// GetName implements DashboardNodeRun
func (r *CheckRun) GetName() string {
	return r.Name
}

// GetRunStatus implements DashboardNodeRun
func (r *CheckRun) GetRunStatus() dashboardinterfaces.DashboardRunStatus {
	return r.runStatus
}

// SetError implements DashboardNodeRun
func (r *CheckRun) SetError(err error) {
	r.Error = err
	r.runStatus = dashboardinterfaces.DashboardRunError
	// raise dashboard error event
	r.executionTree.workspace.PublishDashboardEvent(&dashboardevents.LeafNodeError{
		LeafNode: r,
		Session:  r.executionTree.sessionId,
		Error:    err,
	})
	// tell parent we are done
	r.parent.ChildCompleteChan() <- r

}

// SetComplete implements DashboardNodeRun
func (r *CheckRun) SetComplete() {
	r.runStatus = dashboardinterfaces.DashboardRunComplete
	// raise counter complete event
	r.executionTree.workspace.PublishDashboardEvent(&dashboardevents.LeafNodeComplete{
		LeafNode: r,
		Session:  r.executionTree.sessionId,
	})
	// tell parent we are done
	r.parent.ChildCompleteChan() <- r
}

// RunComplete implements DashboardNodeRun
func (r *CheckRun) RunComplete() bool {
	return r.runStatus == dashboardinterfaces.DashboardRunComplete || r.runStatus == dashboardinterfaces.DashboardRunError
}

// ChildrenComplete implements DashboardNodeRun
func (r *CheckRun) ChildrenComplete() bool {
	return r.RunComplete()
}