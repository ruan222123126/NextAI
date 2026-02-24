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
