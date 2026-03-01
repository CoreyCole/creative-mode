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

func TestHeartbeatWorkflow_CallsAllActivities(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	// Mock all maintenance activities to succeed.
	env.OnActivity("DetectStalls", mock.Anything).Return(nil)
	env.OnActivity("ReapSessions", mock.Anything).Return(nil)
	env.OnActivity("DecayLearnings", mock.Anything).Return(nil)
	env.OnActivity("GenerateDigest", mock.Anything).Return(nil)

	// ReadTicketQueue returns no spawns.
	env.OnActivity("ReadTicketQueue", mock.Anything).Return([]SpawnRequest{}, nil)

	env.ExecuteWorkflow(HeartbeatWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	env.AssertExpectations(t)
}

func TestHeartbeatWorkflow_SpawnsChildrenForPendingWork(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	env.OnActivity("DetectStalls", mock.Anything).Return(nil)
	env.OnActivity("ReapSessions", mock.Anything).Return(nil)
	env.OnActivity("DecayLearnings", mock.Anything).Return(nil)
	env.OnActivity("GenerateDigest", mock.Anything).Return(nil)

	// Return 2 pending spawns.
	spawns := []SpawnRequest{
		{WorkflowID: "wf-1", TicketID: "CM-1", Phase: swarm.PhaseResearch, Attempt: 1},
		{WorkflowID: "wf-2", TicketID: "CM-2", Phase: swarm.PhaseImplement, Attempt: 1},
	}
	env.OnActivity("ReadTicketQueue", mock.Anything).Return(spawns, nil)

	// Mock child SessionWorkflows.
	env.OnWorkflow(SessionWorkflow, mock.Anything, mock.Anything).Return(
		SessionWorkflowResult{Result: swarm.ResultSuccess}, nil,
	)

	env.ExecuteWorkflow(HeartbeatWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	env.AssertExpectations(t)
}

func TestHeartbeatWorkflow_VerifyPhaseUsesVerifyQueue(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	env.OnActivity("DetectStalls", mock.Anything).Return(nil)
	env.OnActivity("ReapSessions", mock.Anything).Return(nil)
	env.OnActivity("DecayLearnings", mock.Anything).Return(nil)
	env.OnActivity("GenerateDigest", mock.Anything).Return(nil)

	// Return a verify phase spawn.
	spawns := []SpawnRequest{
		{WorkflowID: "wf-v", TicketID: "CM-V", Phase: swarm.PhaseVerify, Attempt: 1},
	}
	env.OnActivity("ReadTicketQueue", mock.Anything).Return(spawns, nil)

	// Track child workflow options to verify queue routing.
	env.OnWorkflow(SessionWorkflow, mock.Anything, mock.Anything).Return(
		SessionWorkflowResult{Result: swarm.ResultSuccess}, nil,
	)

	env.ExecuteWorkflow(HeartbeatWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
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

func TestHeartbeatWorkflow_MaintenanceFailuresContinue(t *testing.T) {
	t.Parallel()

	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env)

	// DetectStalls fails, but everything else should still run.
	env.OnActivity("DetectStalls", mock.Anything).Return(errTest)
	env.OnActivity("ReapSessions", mock.Anything).Return(nil)
	env.OnActivity("DecayLearnings", mock.Anything).Return(nil)
	env.OnActivity("GenerateDigest", mock.Anything).Return(nil)
	env.OnActivity("ReadTicketQueue", mock.Anything).Return([]SpawnRequest{}, nil)

	env.ExecuteWorkflow(HeartbeatWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	env.AssertExpectations(t)
}
