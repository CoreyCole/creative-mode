package swarmorch

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"creative-mode/harness/internal/swarm"
)

var errTest = errors.New("test error")

// registerActivities registers the Activities struct on the test environment
// so string-based activity references resolve correctly.
func registerActivities(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivity(&Activities{})
}

func TestSessionWorkflow_ReturnsActivityResult(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	expected := SessionWorkflowResult{
		SessionID: "sess-1",
		Result:    swarm.ResultSuccess,
		Summary:   "all checks passed",
	}
	env.OnActivity("RunClaudeSession", mock.Anything, mock.Anything).Return(expected, nil)

	params := SessionParams{
		WorkflowID: "wf-1",
		TicketID:   "CM-1",
		Phase:      swarm.PhaseResearch,
		Attempt:    1,
	}

	env.ExecuteWorkflow(SessionWorkflow, params)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result SessionWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, expected.SessionID, result.SessionID)
	require.Equal(t, swarm.ResultSuccess, result.Result)
	require.Equal(t, "all checks passed", result.Summary)
}

func TestSessionWorkflow_ActivityFailure_ReturnsInfraFailure(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	env.OnActivity("RunClaudeSession", mock.Anything, mock.Anything).Return(
		SessionWorkflowResult{}, errTest,
	)

	params := SessionParams{
		WorkflowID: "wf-1",
		TicketID:   "CM-1",
		Phase:      swarm.PhaseImplement,
		Attempt:    1,
	}

	env.ExecuteWorkflow(SessionWorkflow, params)

	require.True(t, env.IsWorkflowCompleted())
	// Workflow should complete cleanly (no error) even when activity fails.
	require.NoError(t, env.GetWorkflowError())

	var result SessionWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, swarm.ResultInfraFailure, result.Result)
	require.Contains(t, result.Summary, "activity error")
}

func TestLeadFDEWorkflow_CallsAllActivities(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	env.OnActivity("DetectStalls", mock.Anything).Return(nil)
	env.OnActivity("ReapSessions", mock.Anything).Return(nil)
	env.OnActivity("DecayLearnings", mock.Anything).Return(nil)
	env.OnActivity("GenerateDigest", mock.Anything).Return(nil)
	env.OnActivity("CheckProjectHealth", mock.Anything).
		Return([]ProjectHealthStatus{}, nil)
	env.OnActivity("ReadTicketQueue", mock.Anything).
		Return([]SpawnRequest{}, nil)

	env.ExecuteWorkflow(LeadFDEWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestLeadFDEWorkflow_SpawnsSessionsForPendingWork(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	env.OnActivity("DetectStalls", mock.Anything).Return(nil)
	env.OnActivity("ReapSessions", mock.Anything).Return(nil)
	env.OnActivity("DecayLearnings", mock.Anything).Return(nil)
	env.OnActivity("GenerateDigest", mock.Anything).Return(nil)
	env.OnActivity("CheckProjectHealth", mock.Anything).
		Return([]ProjectHealthStatus{}, nil)

	spawns := []SpawnRequest{
		{
			WorkflowID: "wf-1",
			TicketID:   "CM-1",
			Phase:      swarm.PhaseResearch,
			Attempt:    1,
		},
	}
	env.OnActivity("ReadTicketQueue", mock.Anything).Return(spawns, nil)
	env.OnWorkflow(SessionWorkflow, mock.Anything, mock.Anything).Return(
		SessionWorkflowResult{Result: swarm.ResultSuccess}, nil,
	)

	env.ExecuteWorkflow(LeadFDEWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestProjectOrchestratorWorkflow_CompletesWhenAllDone(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	params := ProjectOrchestratorParams{
		WorkflowID: "wf-proj-1",
		ProjectID:  "wf-proj-1",
		TicketID:   "CM-PROJ-1",
	}

	// AdvanceProject returns all complete on first check.
	env.OnActivity("AdvanceProject", mock.Anything, params).Return(
		ProjectProgressResult{
			AllComplete:    true,
			TotalTickets:   3,
			CompletedCount: 3,
		}, nil,
	)

	env.OnActivity("PostProjectUpdate", mock.Anything, ProjectUpdateParams{
		TicketID:       "CM-PROJ-1",
		TotalTickets:   3,
		CompletedCount: 3,
	}).Return(nil)

	env.ExecuteWorkflow(ProjectOrchestratorWorkflow, params)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestProjectOrchestratorWorkflow_AdvancesWaves(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	params := ProjectOrchestratorParams{
		WorkflowID: "wf-proj-2",
		ProjectID:  "wf-proj-2",
		TicketID:   "CM-PROJ-2",
	}

	// First call: wave in progress with new tickets started.
	env.OnActivity("AdvanceProject", mock.Anything, params).Return(
		ProjectProgressResult{
			AllComplete:    false,
			TotalTickets:   4,
			CompletedCount: 2,
			StartedCount:   1,
		}, nil,
	).Once()

	// Second call: all complete.
	env.OnActivity("AdvanceProject", mock.Anything, params).Return(
		ProjectProgressResult{
			AllComplete:    true,
			TotalTickets:   4,
			CompletedCount: 4,
		}, nil,
	).Once()

	// PostProjectUpdate called for the wave advance (StartedCount > 0)
	// and the final completion.
	env.OnActivity(
		"PostProjectUpdate",
		mock.Anything,
		mock.Anything,
	).Return(nil)

	env.ExecuteWorkflow(ProjectOrchestratorWorkflow, params)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
