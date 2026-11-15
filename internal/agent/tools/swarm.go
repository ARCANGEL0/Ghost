package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"charm.land/fantasy"
)

//go:embed swarm.md
var swarmDescription []byte

const (
	SwarmToolName   = "swarm"
	maxSwarmWorkers = 5
	maxSwarmTasks   = 12
)

type SwarmParams struct {
	Task     string   `json:"task" description:"The task to delegate"`
	Workers  int      `json:"workers,omitempty" description:"Maximum parallel workers, from 1 to 5"`
	Strategy string   `json:"strategy,omitempty" description:"parallel, sequential, or compare"`
	Subtasks []string `json:"subtasks,omitempty" description:"Specific tasks to run; Ghost creates a plan when omitted"`
}

func NewSwarmTool(runSubAgent SubAgentRunner, _ string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SwarmToolName,
		string(swarmDescription),
		func(ctx context.Context, params SwarmParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if strings.TrimSpace(params.Task) == "" {
				return fantasy.NewTextErrorResponse("task is required"), nil
			}

			workers := params.Workers
			if workers <= 0 {
				workers = 3
			}
			if workers > maxSwarmWorkers {
				workers = maxSwarmWorkers
			}

			strategy := params.Strategy
			if strategy == "" {
				strategy = "parallel"
			}
			if strategy == "divide_conquer" {
				strategy = "sequential"
			}
			if strategy != "parallel" && strategy != "sequential" && strategy != "compare" {
				return fantasy.NewTextErrorResponse("strategy must be parallel, sequential, or compare"), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session id missing from context")
			}
			agentMessageID := GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("agent message id missing from context")
			}

			subtasks := cleanSubtasks(params.Subtasks)
			if len(subtasks) == 0 {
				var err error
				subtasks, err = planSubtasks(ctx, runSubAgent, sessionID, agentMessageID, call.ID, params.Task, workers, strategy)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
			}
			if len(subtasks) > maxSwarmTasks {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("a swarm can run up to %d tasks", maxSwarmTasks)), nil
			}

			var results []swarmWorkerResult
			if strategy == "sequential" {
				results = executeSequential(ctx, runSubAgent, sessionID, agentMessageID, call.ID, subtasks)
			} else {
				results = executeSwarm(ctx, runSubAgent, sessionID, agentMessageID, call.ID, subtasks, workers)
			}
			return fantasy.NewTextResponse(mergeResults(results, strategy)), nil
		},
	)
}

type SubAgentRunner func(ctx context.Context, params SubAgentRunParams) (fantasy.ToolResponse, error)

type SubAgentRunParams struct {
	SessionID      string
	AgentMessageID string
	ToolCallID     string
	Prompt         string
	SessionTitle   string
	UseWorktree    bool
}

type swarmWorkerResult struct {
	Index    int
	Task     string
	Response string
	Error    string
}

func planSubtasks(ctx context.Context, runner SubAgentRunner, sessionID, messageID, callID, task string, workers int, strategy string) ([]string, error) {
	goal := "independent investigations whose results do not depend on each other"
	if strategy == "sequential" {
		goal = "ordered phases where each phase can use the prior phase's findings"
	} else if strategy == "compare" {
		goal = "distinct approaches to the same task, with their tradeoffs"
	}
	prompt := fmt.Sprintf("Break this task into at most %d concise subtasks. Create %s. The workers are read-only: ask them to inspect and report evidence, never to edit files. Return only JSON in this form: {\"subtasks\":[\"...\"]}.\n\nTask:\n%s", workers, goal, task)
	response, err := runner(ctx, SubAgentRunParams{
		SessionID:      sessionID,
		AgentMessageID: messageID,
		ToolCallID:     callID + "-plan",
		Prompt:         prompt,
		SessionTitle:   "Swarm Plan",
	})
	if err != nil {
		return nil, fmt.Errorf("plan swarm tasks: %w", err)
	}
	if response.IsError {
		return nil, fmt.Errorf("plan swarm tasks: %s", response.Content)
	}
	subtasks, err := parseSubtasks(response.Content)
	if err != nil {
		return nil, fmt.Errorf("could not read the swarm plan: %w; pass subtasks explicitly", err)
	}
	if len(subtasks) > workers {
		subtasks = subtasks[:workers]
	}
	return subtasks, nil
}

func parseSubtasks(response string) ([]string, error) {
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("planner did not return a JSON object")
	}
	var plan struct {
		Subtasks []string `json:"subtasks"`
	}
	if err := json.Unmarshal([]byte(response[start:end+1]), &plan); err != nil {
		return nil, err
	}
	subtasks := cleanSubtasks(plan.Subtasks)
	if len(subtasks) == 0 {
		return nil, fmt.Errorf("planner returned no subtasks")
	}
	return subtasks, nil
}

func cleanSubtasks(subtasks []string) []string {
	cleaned := make([]string, 0, len(subtasks))
	seen := make(map[string]struct{}, len(subtasks))
	for _, task := range subtasks {
		task = strings.TrimSpace(task)
		key := strings.ToLower(task)
		if task == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, task)
	}
	return cleaned
}

func executeSwarm(ctx context.Context, runner SubAgentRunner, sessionID, messageID, callID string, subtasks []string, workers int) []swarmWorkerResult {
	results := make([]swarmWorkerResult, len(subtasks))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i, task := range subtasks {
		results[i] = swarmWorkerResult{Index: i, Task: task}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			results[i].Error = ctx.Err().Error()
			continue
		}
		wg.Add(1)
		go func(index int, task string) {
			defer wg.Done()
			defer func() { <-sem }()
			response, err := runner(ctx, SubAgentRunParams{
				SessionID:      sessionID,
				AgentMessageID: messageID,
				ToolCallID:     fmt.Sprintf("%s-worker-%d", callID, index),
				Prompt:         task,
				SessionTitle:   fmt.Sprintf("Swarm %d", index+1),
				UseWorktree:    true,
			})
			if err != nil {
				results[index].Error = err.Error()
			} else if response.IsError {
				results[index].Error = response.Content
			} else {
				results[index].Response = response.Content
			}
		}(i, task)
	}
	wg.Wait()
	return results
}

func executeSequential(ctx context.Context, runner SubAgentRunner, sessionID, messageID, callID string, subtasks []string) []swarmWorkerResult {
	results := make([]swarmWorkerResult, 0, len(subtasks))
	previous := ""
	for i, task := range subtasks {
		if err := ctx.Err(); err != nil {
			results = append(results, swarmWorkerResult{Index: i, Task: task, Error: err.Error()})
			continue
		}
		prompt := task
		if previous != "" {
			prompt = "Findings from earlier phases:\n" + previous + "\n\nNext phase:\n" + task
		}
		response, err := runner(ctx, SubAgentRunParams{
			SessionID:      sessionID,
			AgentMessageID: messageID,
			ToolCallID:     fmt.Sprintf("%s-phase-%d", callID, i),
			Prompt:         prompt,
			SessionTitle:   fmt.Sprintf("Swarm Phase %d", i+1),
			UseWorktree:    true,
		})
		result := swarmWorkerResult{Index: i, Task: task}
		if err != nil {
			result.Error = err.Error()
		} else if response.IsError {
			result.Error = response.Content
		} else {
			result.Response = response.Content
			previous = response.Content
		}
		results = append(results, result)
	}
	return results
}

func mergeResults(results []swarmWorkerResult, strategy string) string {
	var sb strings.Builder
	switch strategy {
	case "compare":
		sb.WriteString("## Swarm comparison\n\n")
	case "sequential":
		sb.WriteString("## Swarm phases\n\n")
	default:
		sb.WriteString("## Swarm findings\n\n")
	}

	for _, result := range results {
		fmt.Fprintf(&sb, "### %s\n\n", result.Task)
		if result.Error != "" {
			fmt.Fprintf(&sb, "Worker error: %s\n\n", result.Error)
			continue
		}
		sb.WriteString(result.Response + "\n\n")
	}
	return strings.TrimSpace(sb.String())
}
