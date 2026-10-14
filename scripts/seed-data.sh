#!/usr/bin/env bash
# seed-data.sh — Pre-populate architecture patterns and comparison sets.
#
# Safe to re-run — uses INSERT OR IGNORE / checks for existing data.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
SE="$PROJECT_DIR/mentat"

echo "=== Seeding Architecture Patterns ==="

# --- Agent Patterns ---
$SE pattern add lifecycle-hooks       --name "Lifecycle Hooks"          --type pattern      --category hooks       --desc "Pre/post hooks on task or agent lifecycle events (before_run, after_run, on_error)" 2>/dev/null || true
$SE pattern add enforcement-hooks     --name "Enforcement Hooks"       --type pattern      --category hooks       --desc "Pre-commit, pre-push, or CI hooks that enforce code quality gates" 2>/dev/null || true
$SE pattern add extension-hooks       --name "Extension Hooks"         --type pattern      --category hooks       --desc "Plugin or middleware hooks that allow extending core behavior" 2>/dev/null || true
$SE pattern add agent-loop            --name "Agent Loop"              --type pattern      --category loops       --desc "Iterative agent execution loop (observe-think-act or ReAct pattern)" 2>/dev/null || true
$SE pattern add retry-with-backoff    --name "Retry with Backoff"      --type pattern      --category loops       --desc "Automatic retry with exponential or linear backoff on failure" 2>/dev/null || true
$SE pattern add feedback-loop         --name "Feedback Loop"           --type pattern      --category loops       --desc "Output fed back as input for iterative refinement" 2>/dev/null || true
$SE pattern add tool-permission       --name "Tool Permissions"        --type pattern      --category tool_calls  --desc "Allowlist/denylist or approval flow for tool execution" 2>/dev/null || true
$SE pattern add tool-sandboxing       --name "Tool Sandboxing"         --type pattern      --category tool_calls  --desc "Isolated execution environment for tool calls (containers, ptys)" 2>/dev/null || true
$SE pattern add token-budgeting       --name "Token Budgeting"         --type pattern      --category tool_calls  --desc "Tracking and limiting token usage per request or session" 2>/dev/null || true
$SE pattern add mcp-integration       --name "MCP Integration"         --type pattern      --category tool_calls  --desc "Model Context Protocol server or client implementation" 2>/dev/null || true
$SE pattern add multi-agent-coord     --name "Multi-Agent Coordination" --type pattern     --category agents      --desc "Multiple agents collaborating on a task with delegation or handoff" 2>/dev/null || true
$SE pattern add agent-registry        --name "Agent Registry"          --type pattern      --category agents      --desc "Central registry of agent capabilities for discovery and matching" 2>/dev/null || true
$SE pattern add skill-composition     --name "Skill Composition"       --type pattern      --category skills      --desc "Composable skill definitions that agents can discover and invoke" 2>/dev/null || true
$SE pattern add progressive-loading   --name "Progressive Loading"     --type pattern      --category context     --desc "Lazy or on-demand loading of context (CLAUDE.md, boot prompts, deferred tools)" 2>/dev/null || true
$SE pattern add namespace-isolation   --name "Namespace Isolation"     --type pattern      --category memory      --desc "Isolate context/data by user or application namespace" 2>/dev/null || true
$SE pattern add vector-search         --name "Vector Search"           --type pattern      --category memory      --desc "Embedding-based semantic search for memory retrieval" 2>/dev/null || true
$SE pattern add context-windowing     --name "Context Windowing"       --type pattern      --category memory      --desc "Sliding window or summarization to manage context size" 2>/dev/null || true
$SE pattern add plugin-architecture   --name "Plugin Architecture"     --type pattern      --category architecture --desc "Extensible plugin system with lifecycle management" 2>/dev/null || true
$SE pattern add dag-orchestration     --name "DAG Orchestration"       --type pattern      --category architecture --desc "Directed acyclic graph for task dependency and parallel execution" 2>/dev/null || true
$SE pattern add provider-abstraction  --name "Provider Abstraction"    --type pattern      --category architecture --desc "Abstract interface over multiple LLM or service providers" 2>/dev/null || true
$SE pattern add event-driven          --name "Event-Driven Architecture" --type pattern    --category architecture --desc "Event bus or pub/sub for decoupled component communication" 2>/dev/null || true

# --- Anti-Patterns ---
$SE pattern add god-file              --name "God File"                --type anti_pattern --category architecture --desc "Single massive file handling too many concerns (>1000 LoC)" 2>/dev/null || true
$SE pattern add no-error-handling     --name "Missing Error Handling"  --type anti_pattern --category code_quality --desc "Errors swallowed or ignored without logging or propagation" 2>/dev/null || true
$SE pattern add hardcoded-provider    --name "Hardcoded Provider"      --type anti_pattern --category architecture --desc "Direct dependency on a specific LLM provider without abstraction" 2>/dev/null || true
$SE pattern add unbounded-context     --name "Unbounded Context"       --type anti_pattern --category memory      --desc "No limit on context size, risking token overflow" 2>/dev/null || true
$SE pattern add no-tool-validation    --name "No Tool Validation"      --type anti_pattern --category tool_calls  --desc "Tool calls executed without input validation or permission checks" 2>/dev/null || true

echo ""
echo "=== Seeding Comparison Sets ==="

# Delete the UAT one if it exists, recreate cleanly
$SE gap compare-set create own-vs-memory \
  --name "Own Projects vs Memory Tools" \
  --desc "Compare Cortex and Nanite against external memory/RAG solutions" \
  --subjects cortex,nanite \
  --references basic-memory,hindsight,memori,code-review-graph,graphrag 2>/dev/null || true

$SE gap compare-set create own-vs-agents \
  --name "Own Projects vs Agent Frameworks" \
  --desc "Compare agentrc and Nexus against external agent frameworks" \
  --subjects agentrc,nexus \
  --references adk-go,adk-python,agent-framework,agentfactory,strands-sdk,chef,koog 2>/dev/null || true

$SE gap compare-set create own-vs-automation \
  --name "Own Projects vs Automation/DAG Tools" \
  --desc "Compare Hadron and Carrier against workflow orchestration platforms" \
  --subjects hadron,carrier \
  --references dagster,prefect,kestra,tracecat,trigger-dev,maestro,n8n-workflows 2>/dev/null || true

$SE gap compare-set create own-vs-cli \
  --name "Own Projects vs CLI Coding Agents" \
  --desc "Compare Conduit against other coding agent CLIs" \
  --subjects conduit \
  --references codex,gemini-cli,opencode,qwen-code,not-clawd-code 2>/dev/null || true

$SE gap compare-set create own-vs-orchestration \
  --name "Own Projects vs Orchestration Platforms" \
  --desc "Compare Fragments Engine and Nexus against orchestration tools" \
  --subjects fragments-engine,nexus \
  --references agent-orchestrator,swarms,owl,hermes-agent,ruflo,openclaw 2>/dev/null || true

$SE gap compare-set create own-vs-skills \
  --name "Own Projects vs Skills Frameworks" \
  --desc "Compare agentrc skills system against skills/plugin frameworks" \
  --subjects agentrc \
  --references anthropic-skills,marcus-skills,obsidian-skills,claude-plugins,wshobson-agents 2>/dev/null || true

$SE gap compare-set create own-vs-mcp \
  --name "Own Projects vs MCP Implementations" \
  --desc "Compare our MCP integrations against dedicated MCP servers" \
  --subjects conduit,cortex,cerberus,hadron \
  --references cli-mcp-server,deep-research-mcp,spec-workflow-mcp,web-search-mcp,cursor-background-agent-mcp 2>/dev/null || true

echo ""
echo "=== Seeding Known Pattern Links ==="

# Own projects — patterns we know exist from exploration
# Hadron
$SE pattern link lifecycle-hooks hadron       --quality exemplary --notes "internal/plugin/ hook system, pre/post on blueprint and task levels" 2>/dev/null || true
$SE pattern link dag-orchestration hadron     --quality exemplary --notes "internal/pipeline/ DAG planner with parallel execution and retries" 2>/dev/null || true
$SE pattern link mcp-integration hadron       --quality exemplary --notes "internal/mcpadapter/ exposes blueprints and runs as MCP tools" 2>/dev/null || true
$SE pattern link enforcement-hooks hadron     --quality present   --notes "lefthook.yml for pre-commit" 2>/dev/null || true
$SE pattern link plugin-architecture hadron   --quality present   --notes "internal/plugin/ with lifecycle management" 2>/dev/null || true
$SE pattern link retry-with-backoff hadron    --quality present   --notes "Configurable retries per task in blueprint spec" 2>/dev/null || true

# Conduit
$SE pattern link provider-abstraction conduit --quality exemplary --notes "internal/provider/ with 55+ subdirs — Anthropic, OpenAI, Ollama, PTY" 2>/dev/null || true
$SE pattern link tool-permission conduit      --quality exemplary --notes "internal/permission/ access control layer for tool execution" 2>/dev/null || true
$SE pattern link mcp-integration conduit      --quality exemplary --notes "internal/mcp/ (12 subdirs) client + server, tool forwarding" 2>/dev/null || true
$SE pattern link multi-agent-coord conduit    --quality present   --notes "Nexus matching + Hadron DAG orchestration for multi-agent" 2>/dev/null || true
$SE pattern link plugin-architecture conduit  --quality present   --notes "fragments-engine/plugin system, envelope registry" 2>/dev/null || true

# Cortex
$SE pattern link namespace-isolation cortex   --quality exemplary --notes "internal/contextpolicy/ enforces namespace boundaries" 2>/dev/null || true
$SE pattern link vector-search cortex         --quality exemplary --notes "internal/embedding/ (13 subdirs) for RAG" 2>/dev/null || true
$SE pattern link mcp-integration cortex       --quality exemplary --notes "internal/mcpadapter/ exposes context tools" 2>/dev/null || true
$SE pattern link progressive-loading cortex   --quality present   --notes "Context type registry with on-demand loading" 2>/dev/null || true

# Fragments Engine
$SE pattern link plugin-architecture fragments-engine --quality exemplary --notes "internal/plugin/ shared across all apps" 2>/dev/null || true
$SE pattern link multi-agent-coord fragments-engine   --quality present   --notes "internal/service/ (12 subdirs) multi-service orchestration" 2>/dev/null || true
$SE pattern link event-driven fragments-engine        --quality present   --notes "OTel events for observability and retention triggers" 2>/dev/null || true

# Cerberus
$SE pattern link mcp-integration cerberus     --quality present   --notes "internal/mcp/ exposes lifecycle and health as MCP tools" 2>/dev/null || true
$SE pattern link lifecycle-hooks cerberus     --quality present   --notes "Daemon health monitor loop with auto-restart" 2>/dev/null || true

# Nanite
$SE pattern link vector-search nanite         --quality partial  --notes "FTS5 full-text search but no vector embeddings yet" 2>/dev/null || true

# Sigil
$SE pattern link mcp-integration sigil        --quality present   --notes "internal/mcp/ for AI-assisted UI building" 2>/dev/null || true

# agentrc
$SE pattern link skill-composition agentrc    --quality exemplary --notes "Skills as composable procedures, agent config maps skills to roles" 2>/dev/null || true
$SE pattern link progressive-loading agentrc  --quality exemplary --notes "Boot prompts, role files, project context loaded on demand" 2>/dev/null || true
$SE pattern link enforcement-hooks agentrc    --quality present   --notes "hooks/ directory for enforcement hooks" 2>/dev/null || true

echo ""
echo "=== Summary ==="
$SE db stats
