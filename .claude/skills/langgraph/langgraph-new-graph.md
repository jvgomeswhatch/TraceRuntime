---
name: langgraph:new-graph
description: Scaffold de grafo LangGraph com estado TypedDict tipado, nós de validação, inferência Ollama e saída. Inclui compilação do grafo, tratamento de erro e estrutura de diretórios padrão para este projeto.
---

# Skill: langgraph:new-graph

## Input necessário
1. Nome do grafo (ex: `inference`, `summarization`)
2. Qual é o input esperado? (campo principal do estado)
3. Precisa de contexto adicional (RAG, histórico)? Sim/Não

## O que gerar

### 1. `app/graphs/<nome>.py`
```python
"""
Grafo de inferência principal do EventDrive AI runtime.

Fluxo: validate_input → ollama_inference → format_output
       └── em erro qualquer nó → error_handler → END
"""
from typing import Optional
from typing_extensions import TypedDict

from langgraph.graph import StateGraph, END

from app.nodes.validate import validate_node
from app.nodes.ollama import ollama_node
from app.nodes.output import format_output_node
from app.nodes.error import error_node


class InferenceState(TypedDict):
    # Campos de controle — NUNCA remover
    trace_id: str
    request_id: str

    # Input
    input: str
    context: Optional[str]   # para RAG quando implementado

    # Output
    output: Optional[str]
    error: Optional[str]
    duration_ms: Optional[int]

    # Roteamento interno
    _has_error: bool


def should_continue(state: InferenceState) -> str:
    """Router: qualquer erro vai direto para error_node."""
    if state.get("_has_error") or state.get("error"):
        return "error"
    return "continue"


def build_inference_graph() -> StateGraph:
    graph = StateGraph(InferenceState)

    graph.add_node("validate",  validate_node)
    graph.add_node("inference", ollama_node)
    graph.add_node("output",    format_output_node)
    graph.add_node("error",     error_node)

    graph.set_entry_point("validate")

    graph.add_conditional_edges(
        "validate",
        should_continue,
        {"continue": "inference", "error": "error"},
    )
    graph.add_conditional_edges(
        "inference",
        should_continue,
        {"continue": "output", "error": "error"},
    )

    graph.add_edge("output", END)
    graph.add_edge("error",  END)

    return graph.compile()


# Singleton compilado — compilar uma vez, reusar por request
inference_graph = build_inference_graph()
```

### 2. `app/nodes/validate.py`
```python
import logging
from app.graphs.inference import InferenceState

logger = logging.getLogger(__name__)

MAX_INPUT_LEN = 4096  # ~1k tokens — conservador para modelos 6B

def validate_node(state: InferenceState) -> InferenceState:
    logger.info("validate_node.start", extra={"trace_id": state["trace_id"]})

    inp = state.get("input", "").strip()

    if not inp:
        return {**state, "error": "input is empty", "_has_error": True}

    if len(inp) > MAX_INPUT_LEN:
        return {**state,
                "error": f"input too long: {len(inp)} chars (max {MAX_INPUT_LEN})",
                "_has_error": True}

    logger.info("validate_node.ok", extra={"trace_id": state["trace_id"]})
    return {**state, "input": inp, "_has_error": False}
```

### 3. `app/nodes/error.py`
```python
import logging
from app.graphs.inference import InferenceState

logger = logging.getLogger(__name__)

def error_node(state: InferenceState) -> InferenceState:
    logger.error("graph.error",
        extra={
            "trace_id": state.get("trace_id"),
            "error": state.get("error"),
        }
    )
    # Estado final com error preenchido — endpoint formata a resposta
    return state
```

### 4. `app/nodes/output.py`
```python
from app.graphs.inference import InferenceState

def format_output_node(state: InferenceState) -> InferenceState:
    # Ponto de pós-processamento: trim, sanitize, etc.
    output = (state.get("output") or "").strip()
    return {**state, "output": output}
```

### 5. Invocar o grafo (em `app/api/routes.py`)
```python
from app.graphs.inference import inference_graph

async def run_inference(trace_id: str, request_id: str, input_text: str) -> dict:
    result = await inference_graph.ainvoke({
        "trace_id":   trace_id,
        "request_id": request_id,
        "input":      input_text,
        "context":    None,
        "_has_error": False,
    })
    return result
```

## Checklist pós-geração
- [ ] `InferenceState` tem `trace_id` e `_has_error` — campos obrigatórios
- [ ] `build_inference_graph()` compila sem erro (`python -c "from app.graphs.inference import inference_graph"`)
- [ ] `error_node` alcançável de todos os nós que podem falhar
- [ ] Grafo é singleton — não recompilar por request
- [ ] `ainvoke` usado para async — nunca `invoke` síncrono em FastAPI async handler
