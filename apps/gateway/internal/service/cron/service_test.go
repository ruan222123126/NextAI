package cron

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/adapters"
	"nextai/apps/gateway/internal/service/ports"
)

func TestExecuteJobSuccessUpdatesState(t *testing.T) {
	store, dir := newTestStore(t)
	seedTestJob(t, store, "job-success", domain.CronRuntimeSpec{MaxConcurrency: 1, TimeoutSeconds: 5})

	svc := NewService(Dependencies{
		Store:   adapters.NewRepoStateStore(store),
		DataDir: dir,
		ExecuteTask: func(context.Context, domain.CronJobSpec) (bool, error) {
			return true, nil
		},
	})

	if err := svc.ExecuteJob("job-success"); err != nil {
		t.Fatalf("execute job failed: %v", err)
	}

	state := readState(t, store, "job-success")
	if state.LastStatus == nil || *state.LastStatus != statusSucceeded {
		t.Fatalf("expected last_status=%q, got=%v", statusSucceeded, state.LastStatus)
	}
	if state.LastError != nil {
		t.Fatalf("expected last_error=nil, got=%v", *state.LastError)
	}
}

func TestExecuteJobTimeoutMapped(t *testing.T) {
	store, dir := newTestStore(t)
	seedTestJob(t, store, "job-timeout", domain.CronRuntimeSpec{MaxConcurrency: 1, TimeoutSeconds: 1})

	svc := NewService(Dependencies{
		Store:   adapters.NewRepoStateStore(store),
		DataDir: dir,
		ExecuteTask: func(ctx context.Context, _ domain.CronJobSpec) (bool, error) {
			<-ctx.Done()
			return true, ctx.Err()
		},
	})

	err := svc.ExecuteJob("job-timeout")
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got=%v", err)
	}

	state := readState(t, store, "job-timeout")
	if state.LastStatus == nil || *state.LastStatus != statusFailed {
		t.Fatalf("expected last_status=%q, got=%v", statusFailed, state.LastStatus)
	}
	if state.LastError == nil || !strings.Contains(*state.LastError, "timeout") {
		t.Fatalf("expected timeout last_error, got=%v", state.LastError)
	}
}

func TestExecuteJobRespectsMaxConcurrency(t *testing.T) {
	store, dir := newTestStore(t)
	seedTestJob(t, store, "job-concurrency", domain.CronRuntimeSpec{MaxConcurrency: 1, TimeoutSeconds: 5})

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	svc := NewService(Dependencies{
		Store:   adapters.NewRepoStateStore(store),
		DataDir: dir,
		ExecuteTask: func(ctx context.Context, _ domain.CronJobSpec) (bool, error) {
			started <- struct{}{}
			select {
			case <-ctx.Done():
				return true, ctx.Err()
			case <-release:
				return true, nil
			}
		},
	})

	err1Ch := make(chan error, 1)
	go func() {
		err1Ch <- svc.ExecuteJob("job-concurrency")
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first execution did not start in time")
	}

	if err := svc.ExecuteJob("job-concurrency"); !errors.Is(err, ErrMaxConcurrencyReached) {
		t.Fatalf("expected ErrMaxConcurrencyReached, got=%v", err)
	}

	close(release)
	if err := <-err1Ch; err != nil {
		t.Fatalf("first execution failed: %v", err)
	}
}

func TestExecuteWorkflowNodeRoutesByHandlerRegistry(t *testing.T) {
	svc := NewService(Dependencies{})
	called := false
	svc.RegisterCronNodeHandler(stubCronNodeHandler{
		nodeType: "custom_event",
		execute: func(context.Context, domain.CronJobSpec, domain.CronWorkflowNode) (CronNodeResult, error) {
			called = true
			return CronNodeResult{Stop: true}, nil
		},
	})

	result, err := svc.executeWorkflowNode(context.Background(), domain.CronJobSpec{}, domain.CronWorkflowNode{Type: "custom_event"})
	if err != nil {
		t.Fatalf("executeWorkflowNode failed: %v", err)
	}
	if !called {
		t.Fatal("expected custom handler to be called")
	}
	if !result.Stop {
		t.Fatal("expected stop=true from custom handler result")
	}
}

func TestBuildWorkflowPlanAllowsRegisteredCustomNode(t *testing.T) {
	svc := NewService(Dependencies{})
	svc.RegisterCronNodeHandler(stubCronNodeHandler{
		nodeType: "custom_event",
		execute: func(context.Context, domain.CronJobSpec, domain.CronWorkflowNode) (CronNodeResult, error) {
			return CronNodeResult{}, nil
		},
	})

	plan, err := svc.buildWorkflowPlan(&domain.CronWorkflowSpec{
		Version: "v1",
		Nodes: []domain.CronWorkflowNode{
			{ID: "start", Type: "start"},
			{ID: "n1", Type: "custom_event", ContinueOnError: true},
		},
		Edges: []domain.CronWorkflowEdge{
			{ID: "e1", Source: "start", Target: "n1"},
		},
	})
	if err != nil {
		t.Fatalf("buildWorkflowPlan failed: %v", err)
	}
	if len(plan.Order) != 1 || plan.Order[0].Type != "custom_event" {
		t.Fatalf("unexpected plan order: %#v", plan.Order)
	}
}

func TestBuildWorkflowPlanRejectsUnknownNodeType(t *testing.T) {
	svc := NewService(Dependencies{})

	_, err := svc.buildWorkflowPlan(&domain.CronWorkflowSpec{
		Version: "v1",
		Nodes: []domain.CronWorkflowNode{
			{ID: "start", Type: "start"},
			{ID: "n1", Type: "unknown"},
		},
		Edges: []domain.CronWorkflowEdge{
			{ID: "e1", Source: "start", Target: "n1"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported type="unknown"`) {
		t.Fatalf("expected unsupported node type error, got=%v", err)
	}
}

func TestExecuteWorkflowTaskBuiltInNodesSuccess(t *testing.T) {
	executedTexts := make([]string, 0, 2)
	svc := NewService(Dependencies{
		ChannelResolver: stubChannelResolver{resolvedChannelName: "console"},
		ExecuteConsoleAgentTask: func(_ context.Context, _ domain.CronJobSpec, text string) error {
			executedTexts = append(executedTexts, text)
			return nil
		},
	})

	job := newWorkflowTestJob(
		[]domain.CronWorkflowNode{
			{ID: "start", Type: "start"},
			{ID: "n1", Type: "text_event", Text: "first"},
			{ID: "n2", Type: "if_event", IfCondition: "task_type == workflow"},
			{ID: "n3", Type: "delay", DelaySeconds: 0},
			{ID: "n4", Type: "text_event", Text: "second"},
		},
		[]domain.CronWorkflowEdge{
			{ID: "e1", Source: "start", Target: "n1"},
			{ID: "e2", Source: "n1", Target: "n2"},
			{ID: "e3", Source: "n2", Target: "n3"},
			{ID: "e4", Source: "n3", Target: "n4"},
		},
	)

	execution, err := svc.executeWorkflowTask(context.Background(), job)
	if err != nil {
		t.Fatalf("executeWorkflowTask failed: %v", err)
	}
	if execution == nil {
		t.Fatal("expected non-nil workflow execution")
	}
	if execution.HadFailures {
		t.Fatalf("expected had_failures=false, execution=%+v", execution)
	}
	if len(execution.Nodes) != 4 {
		t.Fatalf("execution nodes=%d, want=4", len(execution.Nodes))
	}
	for idx, step := range execution.Nodes {
		if step.Status != statusSucceeded {
			t.Fatalf("node[%d] status=%q, want=%q", idx, step.Status, statusSucceeded)
		}
	}
	if len(executedTexts) != 2 {
		t.Fatalf("executed text count=%d, want=2", len(executedTexts))
	}
	if executedTexts[0] != "first" || executedTexts[1] != "second" {
		t.Fatalf("executed texts=%v, want=[first second]", executedTexts)
	}
}

func TestExecuteWorkflowTaskIfStopMarksRemainingNodesSkipped(t *testing.T) {
	executedTexts := 0
	svc := NewService(Dependencies{
		ChannelResolver: stubChannelResolver{resolvedChannelName: "console"},
		ExecuteConsoleAgentTask: func(_ context.Context, _ domain.CronJobSpec, _ string) error {
			executedTexts++
			return nil
		},
	})

	job := newWorkflowTestJob(
		[]domain.CronWorkflowNode{
			{ID: "start", Type: "start"},
			{ID: "n1", Type: "if_event", IfCondition: "channel == qq"},
			{ID: "n2", Type: "text_event", Text: "should-not-run"},
		},
		[]domain.CronWorkflowEdge{
			{ID: "e1", Source: "start", Target: "n1"},
			{ID: "e2", Source: "n1", Target: "n2"},
		},
	)

	execution, err := svc.executeWorkflowTask(context.Background(), job)
	if err != nil {
		t.Fatalf("executeWorkflowTask failed: %v", err)
	}
	if execution == nil {
		t.Fatal("expected non-nil workflow execution")
	}
	if execution.HadFailures {
		t.Fatalf("expected had_failures=false, execution=%+v", execution)
	}
	if len(execution.Nodes) != 2 {
		t.Fatalf("execution nodes=%d, want=2", len(execution.Nodes))
	}
	if execution.Nodes[0].Status != statusSucceeded {
		t.Fatalf("first node status=%q, want=%q", execution.Nodes[0].Status, statusSucceeded)
	}
	if execution.Nodes[1].Status != workflowNodeExecutionSkipped {
		t.Fatalf("second node status=%q, want=%q", execution.Nodes[1].Status, workflowNodeExecutionSkipped)
	}
	if executedTexts != 0 {
		t.Fatalf("executed texts=%d, want=0", executedTexts)
	}
}

func TestExecuteWorkflowTaskContinueOnErrorKeepsRunning(t *testing.T) {
	executedTexts := make([]string, 0, 2)
	svc := NewService(Dependencies{
		ChannelResolver: stubChannelResolver{resolvedChannelName: "console"},
		ExecuteConsoleAgentTask: func(_ context.Context, _ domain.CronJobSpec, text string) error {
			executedTexts = append(executedTexts, text)
			if text == "fail-node" {
				return errors.New("boom")
			}
			return nil
		},
	})

	job := newWorkflowTestJob(
		[]domain.CronWorkflowNode{
			{ID: "start", Type: "start"},
			{ID: "n1", Type: "text_event", Text: "fail-node", ContinueOnError: true},
			{ID: "n2", Type: "text_event", Text: "after-fail"},
		},
		[]domain.CronWorkflowEdge{
			{ID: "e1", Source: "start", Target: "n1"},
			{ID: "e2", Source: "n1", Target: "n2"},
		},
	)

	execution, err := svc.executeWorkflowTask(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "workflow node n1 failed") {
		t.Fatalf("expected n1 failure error, got=%v", err)
	}
	if execution == nil {
		t.Fatal("expected non-nil workflow execution")
	}
	if !execution.HadFailures {
		t.Fatalf("expected had_failures=true, execution=%+v", execution)
	}
	if len(execution.Nodes) != 2 {
		t.Fatalf("execution nodes=%d, want=2", len(execution.Nodes))
	}
	if execution.Nodes[0].Status != statusFailed {
		t.Fatalf("first node status=%q, want=%q", execution.Nodes[0].Status, statusFailed)
	}
	if execution.Nodes[1].Status != statusSucceeded {
		t.Fatalf("second node status=%q, want=%q", execution.Nodes[1].Status, statusSucceeded)
	}
	if len(executedTexts) != 2 || executedTexts[0] != "fail-node" || executedTexts[1] != "after-fail" {
		t.Fatalf("executed texts=%v, want=[fail-node after-fail]", executedTexts)
	}
}

func TestExecuteWorkflowTaskNodeErrorWithoutContinueStopsFlow(t *testing.T) {
	executedTexts := make([]string, 0, 2)
	svc := NewService(Dependencies{
		ChannelResolver: stubChannelResolver{resolvedChannelName: "console"},
		ExecuteConsoleAgentTask: func(_ context.Context, _ domain.CronJobSpec, text string) error {
			executedTexts = append(executedTexts, text)
			if text == "fail-node" {
				return errors.New("boom")
			}
			return nil
		},
	})

	job := newWorkflowTestJob(
		[]domain.CronWorkflowNode{
			{ID: "start", Type: "start"},
			{ID: "n1", Type: "text_event", Text: "fail-node"},
			{ID: "n2", Type: "text_event", Text: "should-not-run"},
		},
		[]domain.CronWorkflowEdge{
			{ID: "e1", Source: "start", Target: "n1"},
			{ID: "e2", Source: "n1", Target: "n2"},
		},
	)

	execution, err := svc.executeWorkflowTask(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "workflow node n1 failed") {
		t.Fatalf("expected n1 failure error, got=%v", err)
	}
	if execution == nil {
		t.Fatal("expected non-nil workflow execution")
	}
	if !execution.HadFailures {
		t.Fatalf("expected had_failures=true, execution=%+v", execution)
	}
	if len(execution.Nodes) != 2 {
		t.Fatalf("execution nodes=%d, want=2", len(execution.Nodes))
	}
	if execution.Nodes[0].Status != statusFailed {
		t.Fatalf("first node status=%q, want=%q", execution.Nodes[0].Status, statusFailed)
	}
	if execution.Nodes[1].Status != workflowNodeExecutionSkipped {
		t.Fatalf("second node status=%q, want=%q", execution.Nodes[1].Status, workflowNodeExecutionSkipped)
	}
	if len(executedTexts) != 1 || executedTexts[0] != "fail-node" {
		t.Fatalf("executed texts=%v, want=[fail-node]", executedTexts)
	}
}

func newWorkflowTestJob(nodes []domain.CronWorkflowNode, edges []domain.CronWorkflowEdge) domain.CronJobSpec {
	return domain.CronJobSpec{
		ID:       "job-workflow-test",
		Name:     "job-workflow-test",
		TaskType: taskTypeWorkflow,
		Dispatch: domain.CronDispatchSpec{
			Channel: "console",
			Target: domain.CronDispatchTarget{
				UserID:    "u-workflow",
				SessionID: "s-workflow",
			},
		},
		Workflow: &domain.CronWorkflowSpec{
			Version: workflowVersionV1,
			Nodes:   nodes,
			Edges:   edges,
		},
	}
}

func newTestStore(t *testing.T) (*repo.Store, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "nextai-cron-service-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := repo.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store, dir
}

func seedTestJob(t *testing.T, store *repo.Store, jobID string, runtime domain.CronRuntimeSpec) {
	t.Helper()
	if err := store.Write(func(st *repo.State) error {
		st.CronJobs[jobID] = domain.CronJobSpec{
			ID:       jobID,
			Name:     jobID,
			Enabled:  false,
			TaskType: "text",
			Text:     "hello",
			Schedule: domain.CronScheduleSpec{Type: "interval", Cron: "60s"},
			Dispatch: domain.CronDispatchSpec{
				Target: domain.CronDispatchTarget{
					UserID:    "u1",
					SessionID: "s1",
				},
			},
			Runtime: runtime,
		}
		st.CronStates[jobID] = domain.CronJobState{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func readState(t *testing.T, store *repo.Store, jobID string) domain.CronJobState {
	t.Helper()
	var state domain.CronJobState
	store.Read(func(st *repo.State) {
		state = st.CronStates[jobID]
	})
	return state
}

type stubCronNodeHandler struct {
	nodeType string
	execute  func(ctx context.Context, job domain.CronJobSpec, node domain.CronWorkflowNode) (CronNodeResult, error)
}

func (h stubCronNodeHandler) Type() string {
	return h.nodeType
}

func (h stubCronNodeHandler) Execute(
	ctx context.Context,
	job domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (CronNodeResult, error) {
	if h.execute == nil {
		return CronNodeResult{}, nil
	}
	return h.execute(ctx, job, node)
}

type stubChannelResolver struct {
	resolvedChannelName string
	err                 error
}

func (r stubChannelResolver) ResolveChannel(name string) (ports.Channel, map[string]interface{}, string, error) {
	if r.err != nil {
		return nil, nil, "", r.err
	}
	resolved := strings.TrimSpace(r.resolvedChannelName)
	if resolved == "" {
		resolved = strings.TrimSpace(name)
	}
	return nil, map[string]interface{}{}, resolved, nil
}
