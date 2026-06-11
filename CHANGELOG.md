# Changelog

All changes made to the MCP server in this workspace relative to its upstream origins.

---

## terraform-mcp-server

Forked from [hashicorp/terraform-mcp-server](https://github.com/hashicorp/terraform-mcp-server) (Go / mcp-go).  
All 47 original tools are preserved exactly as-is. This change is additive only.

### New: `state-inspection` Toolset

A new `state-inspection` toolset was added, enabled via `--toolsets=state-inspection` or `--toolsets=all`.

**Toolset registration** (`pkg/toolsets/toolsets.go`)
- Added `StateInspection = "state-inspection"` constant.
- Added `StateInspectionToolset` descriptor with name and description.
- Added to `AvailableToolsets()` so it appears in `--help` output and the toolset validation list.

**Tool→toolset mappings** (`pkg/toolsets/mapping.go`)
- Registered all 9 `tf_*` tool names under the `state-inspection` toolset key.

**Tool registration** (`pkg/tools/tools.go`)
- Imported `stateTools "github.com/hashicorp/terraform-mcp-server/pkg/tools/state"`.
- Added 9 `IsToolEnabled` + `AddTool` calls in `RegisterTools()`, following the identical pattern used for registry tools.

**Test update** (`pkg/toolsets/toolsets_test.go`)
- Updated `TestGetValidToolsetNames` expected count from 5 to 6 to account for the new toolset.

### New Package: `pkg/tools/state`

A new package (`package tools`) containing 14 files and 1,626 lines of Go implementing the 9 state inspection tools ported from the community Python server.

**`loader.go`** — `StateLoader` singleton with multi-backend state loading and caching

The `StateLoader` is initialized once (via `sync.Once`) from environment variables and shared across all tool calls. It supports four backends:

| Backend | Configuration | Mechanism |
|---|---|---|
| `local` | `TF_STATE_PATH` | `os.ReadFile` |
| `gcs` | `TF_STATE_BUCKET`, `TF_STATE_PREFIX` | `gsutil cat gs://…` subprocess |
| `s3` | `TF_STATE_BUCKET`, `TF_STATE_KEY` | `aws s3 cp s3://… -` subprocess |
| `tfc` | `TFE_TOKEN`, `TFE_ADDRESS` | `go-tfe` `StateVersions.ReadCurrent` + `StateVersions.Download` |

Cache: per-workspace TTL cache using `sync.RWMutex` + `map[string]*cacheEntry`. Entries expire after 5 minutes. When at capacity (default 500, controlled by `TF_STATE_CACHE_MAX_WORKSPACES`), expired entries are evicted first; if still full, one arbitrary entry is removed.

State size limit: enforced before JSON parsing, controlled by `TF_STATE_MAX_SIZE_MB` (default 50 MB).

`LoadStateFile()` is a cache-bypassing helper used by `tf_diff_state` to load a comparison state from a local path.

**`types.go`** — data model

`TerraformState`, `StateOutput`, `StateResource`, `StateInstance`, `ExtractedResource` — matching the Terraform state v4 JSON schema.

**`redact.go`** — sensitive attribute redaction

Two-layer redaction applied before any tool returns attribute data:
1. Manifest-driven: walks `sensitive_attributes` paths from each resource instance and replaces values with `[REDACTED - sensitive]`.
2. Pattern-driven: applies `TF_SENSITIVE_ATTR_PATTERN` regex (case-insensitive) against attribute keys recursively, replacing matches with `[REDACTED - pattern match]`.

`deepCopyMap` uses a JSON round-trip to ensure redaction never mutates cached state.

**`resources.go`** — resource extraction

`ExtractResources()` flattens all resource instances from state, applying both redaction layers and computing a canonical `address` for each instance (handles `count`, `for_each` string keys, and `for_each` numeric indices).

**`diff_state.go`** — `safeDiffPath()` path validation

Mirrors the Python implementation: (1) `os.Lstat` rejects symlinks before any path resolution; (2) `filepath.Abs` + `filepath.Clean` produces a lexically-canonical absolute path without following symlinks; (3) `.tfstate` extension enforced; (4) `filepath.Rel` confirms containment within `TF_STATE_DIFF_BASE_DIR`.

**`errors.go`** — `ToolError` / `ToolErrorf` helpers matching the pattern in `pkg/tools/tfe/errors.go`.

### Tool Reference (Go port)

All tools follow the same parameter contract as the Python originals. `organization` and `workspace` parameters default to `TF_CLOUD_ORG` / server-level config when omitted, enabling single-workspace deployments to work without passing parameters on every call.

| Tool | Parameters | Notes |
|---|---|---|
| `tf_list_workspaces` | `organization`*, `name_filter`, `page`, `page_size` | Requires TFE token; uses `go-tfe` Workspaces.List |
| `tf_list_resources` | `organization`, `workspace`, `type_filter`, `module_filter`, `response_format` | JSON or text output |
| `tf_get_resource` | `address`*, `organization`, `workspace` | Returns full attributes with redaction |
| `tf_search_attributes` | `query`*, `organization`, `workspace`, `resource_type` | Recursive substring search |
| `tf_get_outputs` | `organization`, `workspace`, `output_name` | Sensitive outputs redacted |
| `tf_dependency_graph` | `organization`, `workspace`, `resource_type` | ASCII tree sorted by address |
| `tf_diff_state` | `other_state_path`*, `organization`, `workspace` | Path confined to `TF_STATE_DIFF_BASE_DIR` |
| `tf_summary` | `organization`, `workspace` | Counts by type and module, lists outputs |
| `tf_refresh_cache` | `organization`, `workspace` | Invalidates one cache entry |

`*` required parameter

---

## Environment Variable Reference

Variables shared across both servers:

| Variable | Default | Description |
|---|---|---|
| `TF_STATE_BACKEND` | `local` | Backend type: `local`, `gcs`, `s3`, or `tfc` |
| `TF_STATE_PATH` | `terraform.tfstate` | Path to state file (local backend) |
| `TF_STATE_BUCKET` | — | GCS or S3 bucket name |
| `TF_STATE_KEY` | — | S3 object key |
| `TF_STATE_PREFIX` | — | GCS object prefix |
| `TF_CLOUD_ORG` / `TFE_ORG` | — | Default TFC/TFE organization |
| `TF_CLOUD_TOKEN` | — | TFC API token (community server) |
| `TFE_TOKEN` | — | TFE API token (official server) |
| `TFE_ADDRESS` | `https://app.terraform.io` | TFE instance URL (official server) |
| `TF_STATE_MAX_SIZE_MB` | `50` | Maximum state file size before rejection |
| `TF_STATE_CACHE_MAX_WORKSPACES` | `500` | Maximum cached workspaces (LRU eviction) |
| `TF_STATE_DIFF_BASE_DIR` | `.` (official) / parent of state file (community) | Directory to which `tf_diff_state` paths are confined |
| `TF_SENSITIVE_ATTR_PATTERN` | — | Regex applied as additional attribute-name blocklist |
