package app

import "nextai/apps/gateway/internal/runner"

func buildToolDefinition(name string) runner.ToolDefinition {
	switch name {
	case "view":
		return runner.ToolDefinition{
			Name:        "view",
			Description: "Read line ranges for one or multiple files. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of view operations; pass one item for single-file view.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Absolute file path on local filesystem.",
								},
								"start": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based starting line number (inclusive).",
								},
								"end": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based ending line number (inclusive).",
								},
							},
							"required":             []string{"path", "start", "end"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "edit":
		return runner.ToolDefinition{
			Name:        "edit",
			Description: "Replace line ranges for one or multiple files; can create missing files directly. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of edit operations; pass one item for single-file edit.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Absolute file path on local filesystem.",
								},
								"start": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based starting line number (inclusive).",
								},
								"end": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based ending line number (inclusive).",
								},
								"content": map[string]interface{}{
									"type":        "string",
									"description": "Replacement text for the selected line range.",
								},
							},
							"required":             []string{"path", "start", "end", "content"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "shell":
		return runner.ToolDefinition{
			Name:        "shell",
			Description: "Execute one or multiple shell commands under server security controls. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of shell command operations; pass one item for single command.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"command": map[string]interface{}{
									"type": "string",
								},
								"cwd": map[string]interface{}{
									"type": "string",
								},
								"timeout_seconds": map[string]interface{}{
									"type":    "integer",
									"minimum": 1,
								},
							},
							"required":             []string{"command"},
							"additionalProperties": false,
						},
					},
				},
				"required": []string{"items"},
			},
		}
	case "exec_command":
		return runner.ToolDefinition{
			Name:        "exec_command",
			Description: "Execute a shell command, optionally returning a live session_id for follow-up write_stdin calls.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cmd": map[string]interface{}{
						"type":        "string",
						"description": "Shell command text to execute.",
					},
					"workdir": map[string]interface{}{
						"type":        "string",
						"description": "Optional working directory.",
					},
					"shell": map[string]interface{}{
						"type":        "string",
						"description": "Optional shell override path.",
					},
					"login": map[string]interface{}{
						"type":        "boolean",
						"description": "Optional login shell flag.",
					},
					"tty": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the session should allow interactive stdin writes.",
					},
					"yield_time_ms": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "How long to wait for initial output before returning.",
					},
					"max_output_tokens": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional output budget hint.",
					},
					"sandbox_permissions": map[string]interface{}{
						"type":        "string",
						"description": "Optional sandbox permission request.",
					},
					"justification": map[string]interface{}{
						"type":        "string",
						"description": "Optional escalation justification.",
					},
					"prefix_rule": map[string]interface{}{
						"type":        "array",
						"description": "Optional command prefix allowlist hint.",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
				"required":             []string{"cmd"},
				"additionalProperties": true,
			},
		}
	case "write_stdin":
		return runner.ToolDefinition{
			Name:        "write_stdin",
			Description: "Write stdin to a live shell session (session_id) and collect incremental output.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Session id returned by exec_command.",
					},
					"chars": map[string]interface{}{
						"type":        "string",
						"description": "Bytes/chars to write to stdin; leave empty for polling output only.",
					},
					"yield_time_ms": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "How long to wait for additional output.",
					},
					"max_output_tokens": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional output budget hint.",
					},
				},
				"required":             []string{"session_id"},
				"additionalProperties": true,
			},
		}
	case "browser":
		return runner.ToolDefinition{
			Name:        "browser",
			Description: "Delegate browser tasks to local Playwright agent script. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of browser tasks; pass one item for single task.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"task": map[string]interface{}{
									"type":        "string",
									"description": "Natural language task for browser agent.",
								},
								"timeout_seconds": map[string]interface{}{
									"type":    "integer",
									"minimum": 1,
								},
							},
							"required":             []string{"task"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "search":
		return runner.ToolDefinition{
			Name:        "search",
			Description: "Search the web via configured search APIs. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of search requests; pass one item for single query.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"query": map[string]interface{}{
									"type":        "string",
									"description": "Search query text.",
								},
								"provider": map[string]interface{}{
									"type":        "string",
									"description": "Optional provider override: serpapi | tavily | brave.",
								},
								"count": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "Optional max results per query.",
								},
								"timeout_seconds": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "Optional timeout for a single query.",
								},
							},
							"required":             []string{"query"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "open":
		return runner.ToolDefinition{
			Name:        "open",
			Description: "Open a local absolute path via view or open an HTTP(S) URL via browser summary task.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute local file path or HTTP(S) URL.",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Alternative URL field, same as path when using HTTP(S).",
					},
					"lineno": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional line anchor for local file open.",
					},
					"start": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional 1-based start line for local file open.",
					},
					"end": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional 1-based end line for local file open.",
					},
					"timeout_seconds": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional browser timeout when path/url is HTTP(S).",
					},
				},
				"additionalProperties": true,
			},
		}
	case "find":
		return runner.ToolDefinition{
			Name:        "find",
			Description: "Find plain-text pattern in one or multiple workspace files. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of find operations; pass one item for single-file find.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Workspace file path (relative or absolute within workspace).",
								},
								"pattern": map[string]interface{}{
									"type":        "string",
									"description": "Literal pattern text to match.",
								},
								"ignore_case": map[string]interface{}{
									"type":        "boolean",
									"description": "Optional case-insensitive match flag.",
								},
							},
							"required":             []string{"path", "pattern"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "click":
		return runner.ToolDefinition{
			Name:        "click",
			Description: "Approximate click action routed to browser tool. No persistent page session is guaranteed.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Optional target URL.",
					},
					"ref_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional reference id or URL from previous context.",
					},
					"id": map[string]interface{}{
						"description": "Optional clickable element id.",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "Optional CSS/XPath selector.",
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "Optional explicit browser task text.",
					},
					"timeout_seconds": map[string]interface{}{
						"type":    "integer",
						"minimum": 1,
					},
				},
				"additionalProperties": true,
			},
		}
	case "screenshot":
		return runner.ToolDefinition{
			Name:        "screenshot",
			Description: "Approximate screenshot action routed to browser tool. No persistent page session is guaranteed.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Optional target URL.",
					},
					"ref_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional reference id or URL from previous context.",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Optional screenshot output hint.",
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "Optional explicit browser task text.",
					},
					"timeout_seconds": map[string]interface{}{
						"type":    "integer",
						"minimum": 1,
					},
				},
				"additionalProperties": true,
			},
		}
	case "self_ops":
		return runner.ToolDefinition{
			Name:        "self_ops",
			Description: "Execute self-operation actions: bootstrap_session, set_session_model, preview_mutation, apply_mutation.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"enum": []string{"bootstrap_session", "set_session_model", "preview_mutation", "apply_mutation"},
					},
					"user_id":         map[string]interface{}{"type": "string"},
					"channel":         map[string]interface{}{"type": "string"},
					"session_seed":    map[string]interface{}{"type": "string"},
					"first_input":     map[string]interface{}{"type": "string"},
					"prompt_mode":     map[string]interface{}{"type": "string"},
					"session_id":      map[string]interface{}{"type": "string"},
					"provider_id":     map[string]interface{}{"type": "string"},
					"model":           map[string]interface{}{"type": "string"},
					"target":          map[string]interface{}{"type": "string"},
					"operations":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object", "additionalProperties": true}},
					"allow_sensitive": map[string]interface{}{"type": "boolean"},
					"mutation_id":     map[string]interface{}{"type": "string"},
					"confirm_hash":    map[string]interface{}{"type": "string"},
				},
				"required":             []string{"action"},
				"additionalProperties": true,
			},
		}
	case "apply_patch":
		return runner.ToolDefinition{
			Name:        "apply_patch",
			Description: "Apply a Codex-style patch payload to files on disk.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"patch":   map[string]interface{}{"type": "string"},
					"workdir": map[string]interface{}{"type": "string"},
					"cwd":     map[string]interface{}{"type": "string"},
				},
				"required":             []string{"patch"},
				"additionalProperties": false,
			},
		}
	case "request_user_input":
		return runner.ToolDefinition{
			Name:        "request_user_input",
			Description: "Ask 1-3 clarifying questions and wait for user answers via /agent/tool-input-answer.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"request_id": map[string]interface{}{"type": "string"},
					"questions": map[string]interface{}{
						"type":     "array",
						"minItems": 1,
						"maxItems": 3,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id":       map[string]interface{}{"type": "string"},
								"header":   map[string]interface{}{"type": "string"},
								"question": map[string]interface{}{"type": "string"},
								"options": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"label":       map[string]interface{}{"type": "string"},
											"description": map[string]interface{}{"type": "string"},
										},
										"required":             []string{"label"},
										"additionalProperties": false,
									},
								},
							},
							"required":             []string{"question"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"questions"},
				"additionalProperties": false,
			},
		}
	case "output_plan":
		return runner.ToolDefinition{
			Name:        "output_plan",
			Description: "Persist final structured plan into plan mode state.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": true,
					},
				},
				"required":             []string{"plan"},
				"additionalProperties": true,
			},
		}
	case "Bash":
		return runner.ToolDefinition{
			Name:        "Bash",
			Description: "Execute a bash command with optional timeout in milliseconds.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command":     map[string]interface{}{"type": "string"},
					"timeout":     map[string]interface{}{"type": "number"},
					"description": map[string]interface{}{"type": "string"},
					"cwd":         map[string]interface{}{"type": "string"},
					"workdir":     map[string]interface{}{"type": "string"},
				},
				"required":             []string{"command"},
				"additionalProperties": false,
			},
		}
	case "Read":
		return runner.ToolDefinition{
			Name:        "Read",
			Description: "Read a file with optional line offset/limit.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"offset":    map[string]interface{}{"type": "number"},
					"limit":     map[string]interface{}{"type": "number"},
				},
				"required":             []string{"file_path"},
				"additionalProperties": false,
			},
		}
	case "NotebookRead":
		return runner.ToolDefinition{
			Name:        "NotebookRead",
			Description: "Read cells from a Jupyter notebook.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"notebook_path": map[string]interface{}{"type": "string"},
					"cell_id":       map[string]interface{}{"type": "string"},
				},
				"required":             []string{"notebook_path"},
				"additionalProperties": false,
			},
		}
	case "Write":
		return runner.ToolDefinition{
			Name:        "Write",
			Description: "Write full file content.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"content":   map[string]interface{}{"type": "string"},
				},
				"required":             []string{"file_path", "content"},
				"additionalProperties": false,
			},
		}
	case "Edit":
		return runner.ToolDefinition{
			Name:        "Edit",
			Description: "Replace exact string in a file.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path":   map[string]interface{}{"type": "string"},
					"old_string":  map[string]interface{}{"type": "string"},
					"new_string":  map[string]interface{}{"type": "string"},
					"replace_all": map[string]interface{}{"type": "boolean"},
				},
				"required":             []string{"file_path", "old_string", "new_string"},
				"additionalProperties": false,
			},
		}
	case "MultiEdit":
		return runner.ToolDefinition{
			Name:        "MultiEdit",
			Description: "Apply multiple exact string edits to a file atomically.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"edits": map[string]interface{}{
						"type":     "array",
						"minItems": 1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"old_string":  map[string]interface{}{"type": "string"},
								"new_string":  map[string]interface{}{"type": "string"},
								"replace_all": map[string]interface{}{"type": "boolean"},
							},
							"required":             []string{"old_string", "new_string"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"file_path", "edits"},
				"additionalProperties": false,
			},
		}
	case "NotebookEdit":
		return runner.ToolDefinition{
			Name:        "NotebookEdit",
			Description: "Replace, insert, or delete notebook cells.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"notebook_path": map[string]interface{}{"type": "string"},
					"cell_id":       map[string]interface{}{"type": "string"},
					"new_source":    map[string]interface{}{"type": "string"},
					"cell_type": map[string]interface{}{
						"type": "string",
						"enum": []string{"code", "markdown"},
					},
					"edit_mode": map[string]interface{}{
						"type": "string",
						"enum": []string{"replace", "insert", "delete"},
					},
				},
				"required":             []string{"notebook_path", "new_source"},
				"additionalProperties": false,
			},
		}
	case "LS":
		return runner.ToolDefinition{
			Name:        "LS",
			Description: "List entries in a directory.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
					"ignore": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
		}
	case "Glob":
		return runner.ToolDefinition{
			Name:        "Glob",
			Description: "Match files by glob pattern.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{"type": "string"},
					"path":    map[string]interface{}{"type": "string"},
				},
				"required":             []string{"pattern"},
				"additionalProperties": false,
			},
		}
	case "Grep":
		return runner.ToolDefinition{
			Name:        "Grep",
			Description: "Regex search in files.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern":     map[string]interface{}{"type": "string"},
					"path":        map[string]interface{}{"type": "string"},
					"glob":        map[string]interface{}{"type": "string"},
					"output_mode": map[string]interface{}{"type": "string", "enum": []string{"content", "files_with_matches", "count"}},
					"-B":          map[string]interface{}{"type": "number"},
					"-A":          map[string]interface{}{"type": "number"},
					"-C":          map[string]interface{}{"type": "number"},
					"-n":          map[string]interface{}{"type": "boolean"},
					"-i":          map[string]interface{}{"type": "boolean"},
					"type":        map[string]interface{}{"type": "string"},
					"head_limit":  map[string]interface{}{"type": "number"},
					"multiline":   map[string]interface{}{"type": "boolean"},
				},
				"required":             []string{"pattern"},
				"additionalProperties": false,
			},
		}
	case "WebSearch":
		return runner.ToolDefinition{
			Name:        "WebSearch",
			Description: "Search web results.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
					"allowed_domains": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"blocked_domains": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		}
	case "WebFetch":
		return runner.ToolDefinition{
			Name:        "WebFetch",
			Description: "Fetch and summarize a URL.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":    map[string]interface{}{"type": "string"},
					"prompt": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"url", "prompt"},
				"additionalProperties": false,
			},
		}
	default:
		return runner.ToolDefinition{
			Name: name,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": true,
			},
		}
	}
}
