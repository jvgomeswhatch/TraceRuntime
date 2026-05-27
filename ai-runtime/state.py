from typing import TypedDict


class ExecutionProfile(TypedDict):
    task_type: str        # "general" | "coding"
    model: str
    timeout_ms: int
    max_output_chars: int


class GraphState(TypedDict):
    task_id: str
    input: str
    deadline_unix_ms: int
    execution_profile: ExecutionProfile
    output: str
    execution_status: str   # completed | degraded | failed
    validation_status: str  # ok | empty | truncated | invalid_output
    inference_duration_ms: int
