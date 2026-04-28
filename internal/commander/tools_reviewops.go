package commander

import (
	"context"
	"fmt"
)

// --- Review task tool implementations ---
// These tools use function deps (not direct store imports) to avoid
// import cycles between commander and reviewops/server packages.

func listReviewTasksTool(list func(projectID string) ([]map[string]any, error)) Tool {
	return Tool{
		Name:        "list_review_tasks",
		Description: "List review/triage tasks for a project. Returns a JSON array of task summaries with id, kind, repo, target_number, and status. Use to see what review tasks exist.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{
					"type":        "string",
					"description": "The project's id. Required.",
				},
			},
			"required": []string{"project_id"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			projectID, _ := args["project_id"].(string)
			if projectID == "" {
				return "", fmt.Errorf("project_id is required")
			}
			tasks, err := list(projectID)
			if err != nil {
				return "", fmt.Errorf("listing review tasks: %w", err)
			}
			return marshalJSON(tasks)
		},
	}
}

func getReviewTaskTool(get func(taskID string) (map[string]any, error)) Tool {
	return Tool{
		Name:        "get_review_task",
		Description: "Get full details for a single review task by id. Returns the task object with id, project_id, domain, kind, repo, target_number, status, and timestamps.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "The review task's id (e.g. rt_xxx).",
				},
			},
			"required": []string{"task_id"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			taskID, _ := args["task_id"].(string)
			if taskID == "" {
				return "", fmt.Errorf("task_id is required")
			}
			task, err := get(taskID)
			if err != nil {
				return "", fmt.Errorf("getting review task: %w", err)
			}
			return marshalJSON(task)
		},
	}
}

func listReviewArtifactsTool(list func(taskID string) ([]map[string]any, error)) Tool {
	return Tool{
		Name:        "list_review_artifacts",
		Description: "List artifacts for a review task. Returns a JSON array of artifacts with id, kind, content, and created_at. Artifact kinds include source_context, task_result, and markdown.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "The review task's id.",
				},
			},
			"required": []string{"task_id"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			taskID, _ := args["task_id"].(string)
			if taskID == "" {
				return "", fmt.Errorf("task_id is required")
			}
			arts, err := list(taskID)
			if err != nil {
				return "", fmt.Errorf("listing review artifacts: %w", err)
			}
			return marshalJSON(arts)
		},
	}
}

func createReviewTaskTool(create func(projectID, domain, kind, repo string, targetNumber int) (map[string]any, error)) Tool {
	return Tool{
		Name:        "create_review_task",
		Risk:        RiskConfirm,
		Description: "Create a new review/triage task for a project. Specify the GitHub repo (owner/repo), task kind (triage_issue, review_pr, rereview_pr, respond_reviewer), and target number (issue/PR number).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{
					"type":        "string",
					"description": "The project's id.",
				},
				"kind": map[string]any{
					"type":        "string",
					"description": "Task kind: triage_issue, review_pr, rereview_pr, or respond_reviewer.",
					"enum":        []string{"triage_issue", "review_pr", "rereview_pr", "respond_reviewer"},
				},
				"repo": map[string]any{
					"type":        "string",
					"description": "GitHub repository (owner/repo format).",
				},
				"target_number": map[string]any{
					"type":        "integer",
					"description": "Issue or PR number.",
				},
			},
			"required": []string{"project_id", "kind", "repo", "target_number"},
		},
		Summarize: func(args map[string]any) string {
			kind, _ := args["kind"].(string)
			repo, _ := args["repo"].(string)
			num, _ := args["target_number"].(float64)
			return fmt.Sprintf("Create %s task for %s#%d", kind, repo, int(num))
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			projectID, _ := args["project_id"].(string)
			kind, _ := args["kind"].(string)
			repo, _ := args["repo"].(string)
			numFloat, _ := args["target_number"].(float64)
			targetNumber := int(numFloat)
			if projectID == "" || kind == "" || repo == "" || targetNumber <= 0 {
				return "", fmt.Errorf("project_id, kind, repo, and target_number (positive integer) are all required")
			}
			task, err := create(projectID, "github", kind, repo, targetNumber)
			if err != nil {
				return "", fmt.Errorf("creating review task: %w", err)
			}
			return marshalJSON(task)
		},
	}
}

func runReviewTaskTool(run func(ctx context.Context, taskID string) (map[string]any, error)) Tool {
	return Tool{
		Name:        "run_review_task",
		Risk:        RiskConfirm,
		Description: "Run a review task: fetches source context from GitHub, generates a task result with proposed actions, and creates a draft artifact. The task must be in queued or draft_ready status. No GitHub writes are performed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "The review task's id to run.",
				},
			},
			"required": []string{"task_id"},
		},
		Summarize: func(args map[string]any) string {
			taskID, _ := args["task_id"].(string)
			return fmt.Sprintf("Run review task %s", taskID)
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			taskID, _ := args["task_id"].(string)
			if taskID == "" {
				return "", fmt.Errorf("task_id is required")
			}
			result, err := run(ctx, taskID)
			if err != nil {
				return "", fmt.Errorf("running review task: %w", err)
			}
			return marshalJSON(result)
		},
	}
}
