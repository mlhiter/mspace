package main

import "context"

type codexEngineAdapter struct{}

func (codexEngineAdapter) Execute(ctx context.Context, executionContext agentEngineExecutionContext, payload agentSessionPayload, updateRefs func(agentEngineExecution)) (agentEngineExecution, error) {
	return runCodexEngineProtocol(ctx, executionContext.RuntimeClient, executionContext.Config, executionContext.WorkerID, executionContext.TaskID, payload, updateRefs)
}
