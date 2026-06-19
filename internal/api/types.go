package api

import "time"

// ── auth ──────────────────────────────────────────────────────────────────────

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	// Present when 2FA is required instead of tokens.
	RequiresTOTP bool   `json:"requires_totp"`
	TOTPToken    string `json:"totp_token"`
}

type TotpLoginRequest struct {
	TOTPToken string `json:"totp_token"`
	Code      string `json:"code"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// ── device pairing (CLI can pair like the mobile app) ───────────────────────────

type DevicePairRequest struct {
	Code       string `json:"code"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
}

type DevicePairResponse struct {
	AccessToken string `json:"access_token"`
	DeviceToken string `json:"device_token"`
	DeviceID    string `json:"device_id"`
	OrgID       string `json:"org_id"`
	UserName    string `json:"user_name"`
	UserEmail   string `json:"user_email"`
}

// ── agents ──────────────────────────────────────────────────────────────────────

type Agent struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	AgentType    string   `json:"agent_type"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	IsActive     bool     `json:"is_active"`
	IsBuiltin    bool     `json:"is_builtin"`
	ModelPref    string   `json:"model_pref"`
	Temperature  float64  `json:"temperature"`
	Skills       []string `json:"skills"`
	Tools        []string `json:"tools"`
}

// ── channels (external integrations: telegram, …) ───────────────────────────────

type Integration struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	IntegrationType string         `json:"integration_type"`
	IsActive        bool           `json:"is_active"`
	Config          map[string]any `json:"config"`
}

// ChannelConv is one external conversation (backed by a real Nexora chat).
type ChannelConv struct {
	ChatID          string `json:"chat_id"`
	Title           string `json:"title"`
	AgentID         string `json:"agent_id"`
	LastMessage     string `json:"last_message"`
	LastMessageRole string `json:"last_message_role"`
	UpdatedAt       string `json:"updated_at"`
}

// ── chats / sessions ────────────────────────────────────────────────────────────

type Chat struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	AgentID      string    `json:"agent_id"`
	AgentName    string    `json:"agent_name"`
	ParentChatID string    `json:"parent_chat_id"`
	CreatedAt    time.Time `json:"created_at"`
}

type CreateChatRequest struct {
	Title   string `json:"title,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
}

// HierarchyNode is one chat in the /chats/{id}/hierarchy tree (ancestors depth<0, the
// anchor depth 0, descendant sub-chats depth>0).
type HierarchyNode struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	AgentName    string `json:"agent_name"`
	ParentChatID string `json:"parent_chat_id"`
	Depth        int    `json:"depth"`
	NodeType     string `json:"node_type"` // ancestor | current | descendant
	Status       string `json:"status"`
}

type hierarchyResp struct {
	Nodes []HierarchyNode `json:"nodes"`
}

type Message struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	AgentName string         `json:"agent_name"`
	UserName  string         `json:"user_name"`
	Provider  string         `json:"provider_used"`
	Metadata  map[string]any `json:"metadata_"`
	Excluded  bool           `json:"excluded"` // internal/injected msgs (watchdog, system_observation, nudges)
	CreatedAt time.Time      `json:"created_at"`
}

// ── tasks ─────────────────────────────────────────────────────────────────────

type Task struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Output          string `json:"output"`
	Status          string `json:"status"`
	Priority        string `json:"priority"`
	ParentID        string `json:"parent_id"`
	AssignedAgentID string `json:"assigned_agent_id"`
	AssignedAgent   string `json:"assigned_agent_name"`
	ChatID          string `json:"chat_id"`
}

// ── agents CRUD ──────────────────────────────────────────────────────────────

// AgentInput is the create/update payload (PATCH accepts the same fields, all optional).
type AgentInput struct {
	Name         string         `json:"name"`
	AgentType    string         `json:"agent_type,omitempty"`
	Description  string         `json:"description,omitempty"`
	SystemPrompt string         `json:"system_prompt,omitempty"`
	ModelPref    string         `json:"model_pref,omitempty"`
	Temperature  float64        `json:"temperature,omitempty"`
	Skills       []string       `json:"skills,omitempty"`
	Tools        []string       `json:"tools,omitempty"`
	Mcps         []any          `json:"mcps,omitempty"`
	Soul         map[string]any `json:"soul,omitempty"`
}

// CatalogItem is a selectable skill/tool/mcp.
type CatalogItem struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// McpTool is one callable function exposed by an MCP server.
type McpTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// McpServer mirrors the backend McpResponse (GET /mcp-servers).
type McpServer struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Desc       string    `json:"description"`
	URL        string    `json:"url"`
	AuthType   string    `json:"auth_type"`
	KnownTools []McpTool `json:"known_tools"`
}

// PersonaItem is a selectable persona (carries soul + system prompt to apply on pick).
type PersonaItem struct {
	Key          string         `json:"key"`
	Name         string         `json:"name"`
	Soul         map[string]any `json:"soul"`
	SystemPrompt string         `json:"system_prompt"`
}

// ── providers ──────────────────────────────────────────────────────────────────

type Provider struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProviderType string `json:"provider_type"`
	AuthType     string `json:"auth_type"`
	BaseURL      string `json:"base_url"`
	ModelName    string `json:"model_name"`
	IsActive     bool   `json:"is_active"`
	Priority     int    `json:"priority"`
	LastError    string `json:"last_error"`
}

type ProviderInput struct {
	Name         string         `json:"name"`
	ProviderType string         `json:"provider_type"`
	AuthType     string         `json:"auth_type,omitempty"`
	Credentials  map[string]any `json:"credentials,omitempty"`
	BaseURL      string         `json:"base_url,omitempty"`
	ModelName    string         `json:"model_name,omitempty"`
}

type ProviderType struct {
	Key             string   `json:"key"`
	Name            string   `json:"name"`
	Category        string   `json:"category"`
	AuthType        string   `json:"auth_type"`
	BaseURL         string   `json:"base_url"`
	RequiresBaseURL bool     `json:"requires_base_url"`
	DefaultModel    string   `json:"default_model"`
	Models          []string `json:"models"`
}

type ChainStep struct {
	Position     int    `json:"position"`
	ProviderType string `json:"provider_type"`
	ModelName    string `json:"model_name"`
}

type Chain struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	IsDefault bool        `json:"is_default"`
	Steps     []ChainStep `json:"steps"`
}

// ── knowledge bases ─────────────────────────────────────────────────────────────

type KB struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	FileCount   int    `json:"file_count"`
}

type KBInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type KBFile struct {
	ID         string `json:"id"`
	Filename   string `json:"filename"`
	Status     string `json:"status"`
	ChunkCount int    `json:"chunk_count"`
	SizeBytes  int    `json:"size_bytes"`
	Error      string `json:"error"`
}

// ── current user ────────────────────────────────────────────────────────────────

type Me struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	FullName       string `json:"full_name"`
	AvatarEmoji    string `json:"avatar_emoji"`
	TelegramUserID string `json:"telegram_user_id"`
	Notes          string `json:"notes"`        // AI memory
	ContactInfo    string `json:"contact_info"` // JSON array of {label,value}
	IsSuperuser    bool   `json:"is_superuser"`
}

// MeUpdate is a partial profile patch (only non-nil fields are sent).
type MeUpdate struct {
	FullName    *string `json:"full_name,omitempty"`
	AvatarEmoji *string `json:"avatar_emoji,omitempty"`
	Notes       *string `json:"notes,omitempty"`
	ContactInfo *string `json:"contact_info,omitempty"`
}

// ContactRow is one contact-info entry (matches the web's {key,value}).
type ContactRow struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ── projects ───────────────────────────────────────────────────────────────────

type Project struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Status       string            `json:"status"`
	RepoURL      string            `json:"repo_url"`
	RepoType     string            `json:"repo_type"`
	RepoBranch   string            `json:"repo_branch"`
	RepoCredID   string            `json:"repo_credential_id"`
	IsPrivate    bool              `json:"is_private"`
	PMAgentName  string            `json:"pm_agent_name"`
	Tools        []string          `json:"tools"`
	Mcps         []any             `json:"mcps"`
	EnvVars      map[string]string `json:"env_vars"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
}

// ProjectAgent — GET /projects/{id}/agents.
type ProjectAgent struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	AgentType string   `json:"agent_type"`
	Desc      string   `json:"description"`
	Skills    []string `json:"skills"`
	Tools     []string `json:"tools"`
	Mcps      []string `json:"mcps"`
	IsPM      bool     `json:"is_pm"`
	TaskCount int      `json:"task_count"`
}

// GitTreeItem — GET /git-proxy/tree (flat list, build hierarchy client-side).
type GitTreeItem struct {
	Path string `json:"path"`
	Type string `json:"type"` // "file" | "dir"
}

// GitFile — GET /git-proxy/file.
type GitFile struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type ProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	RepoURL     string `json:"repo_url,omitempty"`
	RepoType    string `json:"repo_type,omitempty"`
}

// ── issues ─────────────────────────────────────────────────────────────────────

type Issue struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	ProjectName   string   `json:"project_name"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	Labels        []string `json:"labels"`
	AssignedAgent string   `json:"assigned_agent_name"`
	CommentCount  int      `json:"comment_count"`
}

type IssueList struct {
	Items []Issue `json:"items"`
	Total int     `json:"total"`
}

type IssueInput struct {
	ProjectID   string   `json:"project_id,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

type TaskInput struct {
	Title           string `json:"title,omitempty"`
	Description     string `json:"description,omitempty"`
	Status          string `json:"status,omitempty"`
	Priority        string `json:"priority,omitempty"`
	AssignedAgentID string `json:"assigned_agent_id,omitempty"`
}

type IssueComment struct {
	ID         string `json:"id"`
	AuthorName string `json:"author_user_name"`
	AuthorAgent string `json:"author_agent_name"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at"`
}

// ── schedules ──────────────────────────────────────────────────────────────────

type Schedule struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	CronExpr        string `json:"cron_expr"`
	IntervalMinutes int    `json:"interval_minutes"`
	AgentID         string `json:"agent_id"`
	Prompt          string `json:"prompt"`
	IsActive        bool   `json:"is_active"`
	LastRunAt       string `json:"last_run_at"`
	NextRunAt       string `json:"next_run_at"`
}

type ScheduleInput struct {
	Name            string `json:"name,omitempty"`
	Description     string `json:"description,omitempty"`
	CronExpr        string `json:"cron_expr,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
}

// ── marketplace ────────────────────────────────────────────────────────────────

type MarketItem struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Description  string   `json:"description"`
	Author       string   `json:"author"`
	Version      string   `json:"version"`
	Tags         []string `json:"tags"`
	InstallCount int      `json:"install_count"`
	Icon         string   `json:"icon"`
	Visibility   string   `json:"visibility"`
	Installed    bool     `json:"installed"`
	Liked        bool     `json:"liked"`
	ImportURL    string   `json:"import_url"` // set for registry results — feed to /import
}

// Seed is an installed skill/tool/persona/agent (for the Installed view).
type Seed struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	IsBuiltin bool   `json:"is_builtin"`
	kind      string // set client-side: skill|tool|persona|agent
}

func (s Seed) Kind() string { return s.kind }

type MarketList struct {
	Items []MarketItem `json:"items"`
	Total int          `json:"total"`
}

// RiskAckRequired is the structured 409 detail the core backend returns when a
// low-reputation marketplace package is imported without acknowledge_risk:true.
// Distinguished from other 409s (e.g. "already installed") by Error ==
// "risk_acknowledgment_required".
type RiskAckRequired struct {
	Error                  string `json:"error"`
	Slug                   string `json:"slug"`
	Type                   string `json:"type"`
	WarningLevel           string `json:"warning_level"`
	TrustTier              string `json:"trust_tier"`
	BelowLikeThreshold     bool   `json:"below_like_threshold"`
	BelowDownloadThreshold bool   `json:"below_download_threshold"`
	Disclaimer             string `json:"disclaimer"`
	Message                string `json:"message"`
}

// ── orgs ───────────────────────────────────────────────────────────────────────

type Org struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Role        string `json:"role"`
	IsPersonal  bool   `json:"is_personal"`
	MemberCount int    `json:"member_count"`
}

// ── usage ──────────────────────────────────────────────────────────────────────

type UsageSummary struct {
	TotalInputTokens  int `json:"total_input_tokens"`
	TotalOutputTokens int `json:"total_output_tokens"`
	TotalToolCalls    int `json:"total_tool_calls"`
	ByProvider        []struct {
		Provider     string `json:"provider"`
		InputTokens  int    `json:"input_tokens"`
		OutputTokens int    `json:"output_tokens"`
	} `json:"by_provider"`
}

// ── per-chat usage ───────────────────────────────────────────────────────────────

type ChatUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	ToolCalls    int `json:"tool_calls"`
	ByProvider   []struct {
		Provider     string `json:"provider"`
		InputTokens  int    `json:"input_tokens"`
		OutputTokens int    `json:"output_tokens"`
	} `json:"by_provider"`
	ByTool []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"by_tool"`
}

// ── chat side panels (plan / logs / notes / files) ──────────────────────────────

type PlanStep struct {
	Position int    `json:"position"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Note     string `json:"note"`
}
type Plan struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Status string     `json:"status"`
	Steps  []PlanStep `json:"steps"`
}

type LogEntry struct {
	AgentName string `json:"agent_name"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

type ChatNote struct {
	Content     string `json:"content"`
	Description string `json:"description"`
	Author      string `json:"author"`
	CreatedAt   string `json:"created_at"`
}
type chatNotesResp struct {
	Items []ChatNote `json:"items"`
}

type ChatFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SizeBytes   int    `json:"size_bytes"`
	ContentType string `json:"content_type"`
}

// ── devices ────────────────────────────────────────────────────────────────────

type Device struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	LastSeenAt string `json:"last_seen_at"`
}

// ── backup ─────────────────────────────────────────────────────────────────────

type BackupJob struct {
	JobID       string `json:"job_id"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
	Error       string `json:"error"`
	DownloadURL string `json:"download_url"`
}
