// Package api is the Nexora REST client used by the CLI. It handles bearer auth,
// transparent token refresh on 401, and JSON (de)serialization.
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TokenSink is called whenever tokens change (login/refresh) so the caller can persist them.
type TokenSink func(access, refresh string)

// Client talks to a single Nexora instance.
type Client struct {
	baseURL string // e.g. https://host  (no trailing /api)
	http    *http.Client

	tokMu   sync.RWMutex // guards access/refresh
	access  string
	refresh string
	apiKey  string // optional nxr_ key; if set, used instead of JWT

	refreshMu sync.Mutex // coalesces concurrent 401-triggered refreshes
	onTokens  TokenSink
}

// New builds a client. baseURL is the instance root (without /api). apiKey may be empty.
func New(baseURL, access, refresh, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
		access:  access,
		refresh: refresh,
		apiKey:  apiKey,
	}
}

func (c *Client) SetTokenSink(s TokenSink) { c.onTokens = s }

// BaseURL returns the configured instance root.
func (c *Client) BaseURL() string { return c.baseURL }

// WSToken returns the raw token to use for the chat WebSocket query param.
func (c *Client) WSToken() string {
	if c.apiKey != "" {
		return c.apiKey
	}
	c.tokMu.RLock()
	defer c.tokMu.RUnlock()
	return c.access
}

func (c *Client) bearer() string {
	if c.apiKey != "" {
		return c.apiKey
	}
	c.tokMu.RLock()
	defer c.tokMu.RUnlock()
	return c.access
}

func (c *Client) curRefresh() string {
	c.tokMu.RLock()
	defer c.tokMu.RUnlock()
	return c.refresh
}

// OrgID returns the active organization id from the access-token JWT "org" claim
// (best-effort, no signature check — purely for UI display).
func (c *Client) OrgID() string {
	tok := c.bearer()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Org string `json:"org"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	return claims.Org
}

// ── core request helper (with 401 refresh+retry) ────────────────────────────────

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if err := c.doOnce(ctx, method, path, body, out, true); err != nil {
		return err
	}
	return nil
}

func (c *Client) doOnce(ctx context.Context, method, path string, body, out any, allowRefresh bool) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api"+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	usedTok := c.bearer()
	if usedTok != "" {
		req.Header.Set("Authorization", "Bearer "+usedTok)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && allowRefresh && c.apiKey == "" && c.curRefresh() != "" {
		if rerr := c.refreshOnce(ctx, usedTok); rerr == nil {
			return c.doOnce(ctx, method, path, body, out, false)
		}
	}
	if resp.StatusCode >= 300 {
		return apiError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// APIError carries the HTTP status + the server's clean detail message. RawDetail
// keeps the raw JSON of the `detail` field (when it was a JSON object/array) so
// callers can decode structured error bodies (e.g. the risk-ack 409) without
// losing them to the flattened string.
type APIError struct {
	Status    int
	Detail    string
	RawDetail json.RawMessage // raw `detail` JSON when it was an object/array
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return e.Detail // human-facing: just the detail (e.g. "Skill already installed")
	}
	return http.StatusText(e.Status)
}

// IsConflict reports a 409 (already-installed etc.) — callers can treat it as info.
func IsConflict(err error) bool {
	ae, ok := err.(*APIError)
	return ok && ae.Status == 409
}

// AsRiskAck inspects an error for the structured 409 risk-acknowledgment detail.
// It returns (detail, true) only when the body is the risk-ack object
// (error == "risk_acknowledgment_required"); all other 409s (e.g. "already
// installed") return (nil, false) so the existing info path stays intact.
func AsRiskAck(err error) (*RiskAckRequired, bool) {
	ae, ok := err.(*APIError)
	if !ok || ae.Status != 409 || len(ae.RawDetail) == 0 {
		return nil, false
	}
	var r RiskAckRequired
	if json.Unmarshal(ae.RawDetail, &r) != nil {
		return nil, false
	}
	if r.Error != "risk_acknowledgment_required" {
		return nil, false
	}
	return &r, true
}

func apiError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	var e struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(data, &e) == nil && len(e.Detail) > 0 {
		ae := &APIError{Status: resp.StatusCode}
		// If detail is a JSON string, unwrap it to the bare message; if it's an
		// object/array, keep the raw JSON for structured decoding and flatten a
		// readable string for Error().
		var s string
		if json.Unmarshal(e.Detail, &s) == nil {
			ae.Detail = s
		} else {
			ae.RawDetail = e.Detail
			ae.Detail = flattenDetail(e.Detail)
		}
		return ae
	}
	return &APIError{Status: resp.StatusCode, Detail: strings.TrimSpace(string(data))}
}

// flattenDetail turns a structured `detail` body into a human-readable string,
// preferring a `message` field when present, else the raw JSON.
func flattenDetail(raw json.RawMessage) string {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if msg, ok := obj["message"].(string); ok && msg != "" {
			return msg
		}
	}
	return strings.TrimSpace(string(raw))
}

// ── auth ──────────────────────────────────────────────────────────────────────

func (c *Client) Login(ctx context.Context, email, password string) (*TokenResponse, error) {
	var tr TokenResponse
	if err := c.doOnce(ctx, http.MethodPost, "/auth/login", LoginRequest{email, password}, &tr, false); err != nil {
		return nil, err
	}
	if !tr.RequiresTOTP {
		c.setTokens(tr.AccessToken, tr.RefreshToken)
	}
	return &tr, nil
}

func (c *Client) TotpLogin(ctx context.Context, totpToken, code string) (*TokenResponse, error) {
	var tr TokenResponse
	if err := c.doOnce(ctx, http.MethodPost, "/auth/totp-login", TotpLoginRequest{totpToken, code}, &tr, false); err != nil {
		return nil, err
	}
	c.setTokens(tr.AccessToken, tr.RefreshToken)
	return &tr, nil
}

// refreshOnce coalesces concurrent 401-triggered refreshes: if another goroutine already
// rotated the access token since `staleTok` was used, it returns nil (caller retries with
// the new token); otherwise it performs exactly one refresh. Prevents the token-storm that
// fired N parallel refreshes + N concurrent config saves (corrupting config.toml).
func (c *Client) refreshOnce(ctx context.Context, staleTok string) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if c.bearer() != staleTok {
		return nil // someone already refreshed
	}
	return c.Refresh(ctx)
}

func (c *Client) Refresh(ctx context.Context) error {
	rt := c.curRefresh()
	// Device-paired instances carry an nxd_ device token and refresh via the device endpoint
	// (returns only a new access token); password logins use the standard refresh flow.
	if strings.HasPrefix(rt, "nxd_") {
		var dr struct {
			AccessToken string `json:"access_token"`
		}
		if err := c.doOnce(ctx, http.MethodPost, "/auth/device/refresh", map[string]string{"device_token": rt}, &dr, false); err != nil {
			return err
		}
		c.setTokens(dr.AccessToken, "") // keep the device token
		return nil
	}
	var tr TokenResponse
	if err := c.doOnce(ctx, http.MethodPost, "/auth/refresh", refreshRequest{rt}, &tr, false); err != nil {
		return err
	}
	c.setTokens(tr.AccessToken, tr.RefreshToken)
	return nil
}

func (c *Client) DevicePair(ctx context.Context, code, name, platform string) (*DevicePairResponse, error) {
	var dp DevicePairResponse
	if err := c.doOnce(ctx, http.MethodPost, "/auth/device/pair", DevicePairRequest{code, name, platform}, &dp, false); err != nil {
		return nil, err
	}
	c.setTokens(dp.AccessToken, "") // device pairing returns an access JWT; refresh via device_token (stored elsewhere)
	return &dp, nil
}

func (c *Client) setTokens(access, refresh string) {
	c.tokMu.Lock()
	if access != "" {
		c.access = access
	}
	if refresh != "" {
		c.refresh = refresh
	}
	a, r := c.access, c.refresh
	c.tokMu.Unlock()
	if c.onTokens != nil {
		c.onTokens(a, r) // persist outside the lock; refreshMu already serializes refreshes
	}
}

// ── agents ──────────────────────────────────────────────────────────────────────

func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	var out []Agent
	err := c.do(ctx, http.MethodGet, "/agents?limit=500", nil, &out)
	return out, err
}

// ── chats ─────────────────────────────────────────────────────────────────────

func (c *Client) ListChats(ctx context.Context) ([]Chat, error) {
	var out []Chat
	err := c.do(ctx, http.MethodGet, "/chats", nil, &out)
	return out, err
}

func (c *Client) CreateChat(ctx context.Context, req CreateChatRequest) (*Chat, error) {
	var out Chat
	err := c.do(ctx, http.MethodPost, "/chats", req, &out)
	return &out, err
}

func (c *Client) DeleteChat(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/chats/"+url.PathEscape(id), nil, nil)
}

func (c *Client) Messages(ctx context.Context, chatID string) ([]Message, error) {
	var out []Message
	err := c.do(ctx, http.MethodGet, "/chats/"+url.PathEscape(chatID)+"/messages?limit=500", nil, &out)
	return out, err
}

// Hierarchy returns the chat tree (ancestors + this chat + descendant sub-chats).
func (c *Client) Hierarchy(ctx context.Context, chatID string) ([]HierarchyNode, error) {
	var out hierarchyResp
	err := c.do(ctx, http.MethodGet, "/chats/"+url.PathEscape(chatID)+"/hierarchy", nil, &out)
	return out.Nodes, err
}

// ── channels / integrations ──────────────────────────────────────────────────────

func (c *Client) ListIntegrations(ctx context.Context) ([]Integration, error) {
	var out []Integration
	err := c.do(ctx, http.MethodGet, "/integrations", nil, &out)
	return out, err
}

// CreateIntegration adds a provider account (e.g. a telegram bot token).
func (c *Client) CreateIntegration(ctx context.Context, name, typ string, config map[string]any) (*Integration, error) {
	var out Integration
	body := map[string]any{"name": name, "integration_type": typ, "config": config}
	err := c.do(ctx, http.MethodPost, "/integrations", body, &out)
	return &out, err
}

// UpdateIntegration patches name/config (config merges server-side). Used to assign the
// channel agent and to rename.
func (c *Client) UpdateIntegration(ctx context.Context, id string, body map[string]any) error {
	return c.do(ctx, http.MethodPatch, "/integrations/"+url.PathEscape(id), body, nil)
}

func (c *Client) DeleteIntegration(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/integrations/"+url.PathEscape(id), nil, nil)
}

// ChannelConversations lists an integration's external conversations.
func (c *Client) ChannelConversations(ctx context.Context, intID string) ([]ChannelConv, error) {
	var out []ChannelConv
	err := c.do(ctx, http.MethodGet, "/integrations/"+url.PathEscape(intID)+"/conversations", nil, &out)
	return out, err
}

// SetIntegrationActive starts/stops an integration's bot.
func (c *Client) SetIntegrationActive(ctx context.Context, intID string, active bool) error {
	path := "/integrations/" + url.PathEscape(intID) + "/stop-bot"
	if active {
		path = "/integrations/" + url.PathEscape(intID) + "/start-bot"
	}
	return c.do(ctx, http.MethodPost, path, map[string]any{}, nil)
}

// ── tasks ─────────────────────────────────────────────────────────────────────

func (c *Client) Plans(ctx context.Context, chatID string) ([]Plan, error) {
	var out []Plan
	err := c.do(ctx, http.MethodGet, "/plans?chat_id="+url.QueryEscape(chatID), nil, &out)
	return out, err
}

func (c *Client) Logs(ctx context.Context, chatID string) ([]LogEntry, error) {
	var out []LogEntry
	err := c.do(ctx, http.MethodGet, "/logs?chat_id="+url.QueryEscape(chatID), nil, &out)
	return out, err
}

func (c *Client) Notes(ctx context.Context, chatID string) ([]ChatNote, error) {
	var out chatNotesResp
	err := c.do(ctx, http.MethodGet, "/chats/"+url.PathEscape(chatID)+"/notes?page_size=100", nil, &out)
	return out.Items, err
}

func (c *Client) ChatFiles(ctx context.Context, chatID string) ([]ChatFile, error) {
	var out []ChatFile
	err := c.do(ctx, http.MethodGet, "/chats/"+url.PathEscape(chatID)+"/files", nil, &out)
	return out, err
}

// ChatUsage returns token/tool consumption for a single chat (recursive over sub-chats).
func (c *Client) ChatUsage(ctx context.Context, chatID string) (*ChatUsage, error) {
	var out ChatUsage
	err := c.do(ctx, http.MethodGet, "/chats/"+url.PathEscape(chatID)+"/usage", nil, &out)
	return &out, err
}

func (c *Client) Tasks(ctx context.Context, chatID string) ([]Task, error) {
	q := "/tasks?limit=500"
	if chatID != "" {
		q += "&chat_id=" + url.QueryEscape(chatID)
	}
	var out []Task
	err := c.do(ctx, http.MethodGet, q, nil, &out)
	return out, err
}

// ── agents CRUD ──────────────────────────────────────────────────────────────

func (c *Client) CreateAgent(ctx context.Context, in AgentInput) (*Agent, error) {
	var out Agent
	err := c.do(ctx, http.MethodPost, "/agents", in, &out)
	return &out, err
}

func (c *Client) UpdateAgent(ctx context.Context, id string, in AgentInput) (*Agent, error) {
	var out Agent
	err := c.do(ctx, http.MethodPatch, "/agents/"+url.PathEscape(id), in, &out)
	return &out, err
}

func (c *Client) DeleteAgent(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/agents/"+url.PathEscape(id), nil, nil)
}

// catalogs (builtin + org custom) for the agent editor selectors --------------------

func (c *Client) mergeCatalog(ctx context.Context, builtinPath, orgPath string) []CatalogItem {
	seen := map[string]bool{}
	var out []CatalogItem
	for _, p := range []string{builtinPath, orgPath} {
		var items []CatalogItem
		if err := c.do(ctx, http.MethodGet, p, nil, &items); err != nil {
			continue
		}
		for _, it := range items {
			if it.Key != "" && !seen[it.Key] {
				seen[it.Key] = true
				out = append(out, it)
			}
		}
	}
	return out
}

func (c *Client) SkillCatalog(ctx context.Context) []CatalogItem {
	return c.mergeCatalog(ctx, "/skills/builtin", "/skills")
}
func (c *Client) ToolCatalog(ctx context.Context) []CatalogItem {
	return c.mergeCatalog(ctx, "/tools/builtin", "/tools")
}
func (c *Client) PersonaCatalog(ctx context.Context) []PersonaItem {
	seen := map[string]bool{}
	var out []PersonaItem
	for _, p := range []string{"/personas/builtin", "/personas"} {
		var items []PersonaItem
		if err := c.do(ctx, http.MethodGet, p, nil, &items); err != nil {
			continue
		}
		for _, it := range items {
			if it.Key != "" && !seen[it.Key] {
				seen[it.Key] = true
				out = append(out, it)
			}
		}
	}
	return out
}
func (c *Client) McpCatalog(ctx context.Context) []CatalogItem {
	var items []CatalogItem
	_ = c.do(ctx, http.MethodGet, "/mcp-servers", nil, &items)
	return items
}

// McpServers returns full MCP server records (incl. known_tools) for project config.
func (c *Client) McpServers(ctx context.Context) ([]McpServer, error) {
	var out []McpServer
	err := c.do(ctx, http.MethodGet, "/mcp-servers", nil, &out)
	return out, err
}

// ── project detail ───────────────────────────────────────────────────────────────

func (c *Client) Project(ctx context.Context, id string) (*Project, error) {
	var out Project
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(id), nil, &out)
	return &out, err
}

func (c *Client) CreateProject(ctx context.Context, in ProjectInput) (*Project, error) {
	var out Project
	err := c.do(ctx, http.MethodPost, "/projects", in, &out)
	return &out, err
}

// UpdateProject PATCHes arbitrary project fields (tools/mcps/env_vars/name/…).
func (c *Client) UpdateProject(ctx context.Context, id string, body map[string]any) error {
	return c.do(ctx, http.MethodPatch, "/projects/"+url.PathEscape(id), body, nil)
}

func (c *Client) ProjectTasks(ctx context.Context, id string) ([]Task, error) {
	var out []Task
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(id)+"/tasks?limit=500", nil, &out)
	return out, err
}

func (c *Client) ProjectIssues(ctx context.Context, id string) ([]Issue, error) {
	var out []Issue
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(id)+"/issues", nil, &out)
	return out, err
}

func (c *Client) ProjectAgents(ctx context.Context, id string) ([]ProjectAgent, error) {
	var out []ProjectAgent
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(id)+"/agents", nil, &out)
	return out, err
}

func (c *Client) ProjectLogs(ctx context.Context, id string) ([]LogEntry, error) {
	var out []LogEntry
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(id)+"/logs?limit=200", nil, &out)
	return out, err
}

// GitTree fetches the recursive repo file tree via the git-proxy.
func (c *Client) GitTree(ctx context.Context, credID, repoURL, branch string) ([]GitTreeItem, error) {
	if branch == "" {
		branch = "main"
	}
	q := "?credential_id=" + url.QueryEscape(credID) + "&repo_url=" + url.QueryEscape(repoURL) + "&branch=" + url.QueryEscape(branch)
	var out []GitTreeItem
	err := c.do(ctx, http.MethodGet, "/git-proxy/tree"+q, nil, &out)
	return out, err
}

// GitFile fetches a single file's content via the git-proxy.
func (c *Client) GitFile(ctx context.Context, credID, repoURL, path, branch string) (*GitFile, error) {
	if branch == "" {
		branch = "main"
	}
	q := "?credential_id=" + url.QueryEscape(credID) + "&repo_url=" + url.QueryEscape(repoURL) +
		"&path=" + url.QueryEscape(path) + "&branch=" + url.QueryEscape(branch)
	var out GitFile
	err := c.do(ctx, http.MethodGet, "/git-proxy/file"+q, nil, &out)
	return &out, err
}

// ── providers ──────────────────────────────────────────────────────────────────

func (c *Client) ListProviders(ctx context.Context) ([]Provider, error) {
	var out []Provider
	err := c.do(ctx, http.MethodGet, "/providers", nil, &out)
	return out, err
}

func (c *Client) CreateProvider(ctx context.Context, in ProviderInput) (*Provider, error) {
	var out Provider
	err := c.do(ctx, http.MethodPost, "/providers", in, &out)
	return &out, err
}

func (c *Client) UpdateProvider(ctx context.Context, id string, in ProviderInput) error {
	return c.do(ctx, http.MethodPatch, "/providers/"+url.PathEscape(id), in, nil)
}

func (c *Client) DeleteProvider(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/providers/"+url.PathEscape(id), nil, nil)
}

func (c *Client) ListProviderTypes(ctx context.Context) ([]ProviderType, error) {
	var out []ProviderType
	err := c.do(ctx, http.MethodGet, "/provider-types", nil, &out)
	return out, err
}

func (c *Client) ListChains(ctx context.Context) ([]Chain, error) {
	var out []Chain
	err := c.do(ctx, http.MethodGet, "/providers/chains", nil, &out)
	return out, err
}

// ── knowledge bases ─────────────────────────────────────────────────────────────

func (c *Client) ListKBs(ctx context.Context) ([]KB, error) {
	var out []KB
	err := c.do(ctx, http.MethodGet, "/knowledge-bases", nil, &out)
	return out, err
}

func (c *Client) CreateKB(ctx context.Context, in KBInput) (*KB, error) {
	var out KB
	err := c.do(ctx, http.MethodPost, "/knowledge-bases", in, &out)
	return &out, err
}

func (c *Client) UpdateKB(ctx context.Context, id string, in KBInput) error {
	return c.do(ctx, http.MethodPatch, "/knowledge-bases/"+url.PathEscape(id), in, nil)
}

func (c *Client) DeleteKB(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/knowledge-bases/"+url.PathEscape(id), nil, nil)
}

func (c *Client) ListKBFiles(ctx context.Context, kbID string) ([]KBFile, error) {
	var out []KBFile
	err := c.do(ctx, http.MethodGet, "/knowledge-bases/"+url.PathEscape(kbID)+"/files", nil, &out)
	return out, err
}

func (c *Client) DeleteKBFile(ctx context.Context, kbID, fileID string) error {
	return c.do(ctx, http.MethodDelete, "/knowledge-bases/"+url.PathEscape(kbID)+"/files/"+url.PathEscape(fileID), nil, nil)
}

func (c *Client) IngestURL(ctx context.Context, kbID, ingestURL string) error {
	return c.do(ctx, http.MethodPost, "/knowledge-bases/"+url.PathEscape(kbID)+"/ingest-url", map[string]string{"url": ingestURL}, nil)
}

// ── current user ────────────────────────────────────────────────────────────────

func (c *Client) Me(ctx context.Context) (*Me, error) {
	var out Me
	err := c.do(ctx, http.MethodGet, "/users/me", nil, &out)
	return &out, err
}

// UpdateMe patches the current user's profile (name/avatar/AI-memory/contact).
func (c *Client) UpdateMe(ctx context.Context, in MeUpdate) (*Me, error) {
	var out Me
	err := c.do(ctx, http.MethodPatch, "/users/me", in, &out)
	return &out, err
}

// ── projects ───────────────────────────────────────────────────────────────────

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var out []Project
	err := c.do(ctx, http.MethodGet, "/projects", nil, &out)
	return out, err
}

// ── board ──────────────────────────────────────────────────────────────────────

// Board returns tasks grouped by status column.
func (c *Client) Board(ctx context.Context) (map[string][]Task, error) {
	var out map[string][]Task
	err := c.do(ctx, http.MethodGet, "/board", nil, &out)
	return out, err
}

// SetTaskStatus moves a task to another column.
func (c *Client) SetTaskStatus(ctx context.Context, taskID, status string) error {
	return c.do(ctx, http.MethodPatch, "/tasks/"+url.PathEscape(taskID), map[string]string{"status": status}, nil)
}

func (c *Client) UpdateTask(ctx context.Context, id string, in TaskInput) error {
	return c.do(ctx, http.MethodPatch, "/tasks/"+url.PathEscape(id), in, nil)
}

// ── issues ─────────────────────────────────────────────────────────────────────

func (c *Client) ListIssues(ctx context.Context, projectID string) ([]Issue, error) {
	q := "/issues?limit=500"
	if projectID != "" {
		q += "&project_id=" + url.QueryEscape(projectID)
	}
	var out IssueList
	err := c.do(ctx, http.MethodGet, q, nil, &out)
	return out.Items, err
}

func (c *Client) CreateIssue(ctx context.Context, in IssueInput) (*Issue, error) {
	var out Issue
	err := c.do(ctx, http.MethodPost, "/issues", in, &out)
	return &out, err
}

func (c *Client) UpdateIssue(ctx context.Context, id string, in IssueInput) (*Issue, error) {
	var out Issue
	err := c.do(ctx, http.MethodPatch, "/issues/"+url.PathEscape(id), in, &out)
	return &out, err
}

func (c *Client) DeleteIssue(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/issues/"+url.PathEscape(id), nil, nil)
}

func (c *Client) IssueComments(ctx context.Context, id string) ([]IssueComment, error) {
	var out []IssueComment
	err := c.do(ctx, http.MethodGet, "/issues/"+url.PathEscape(id)+"/comments", nil, &out)
	return out, err
}

func (c *Client) AddIssueComment(ctx context.Context, id, content string) error {
	return c.do(ctx, http.MethodPost, "/issues/"+url.PathEscape(id)+"/comments", map[string]any{"content": content}, nil)
}

// ── schedules ──────────────────────────────────────────────────────────────────

func (c *Client) ListSchedules(ctx context.Context) ([]Schedule, error) {
	var out []Schedule
	err := c.do(ctx, http.MethodGet, "/schedules", nil, &out)
	return out, err
}

func (c *Client) CreateSchedule(ctx context.Context, in ScheduleInput) (*Schedule, error) {
	var out Schedule
	err := c.do(ctx, http.MethodPost, "/schedules", in, &out)
	return &out, err
}

func (c *Client) UpdateSchedule(ctx context.Context, id string, in ScheduleInput) error {
	return c.do(ctx, http.MethodPatch, "/schedules/"+url.PathEscape(id), in, nil)
}

func (c *Client) DeleteSchedule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/schedules/"+url.PathEscape(id), nil, nil)
}

func (c *Client) ActivateSchedule(ctx context.Context, id string, active bool) error {
	action := "activate"
	if !active {
		action = "deactivate"
	}
	return c.do(ctx, http.MethodPost, "/schedules/"+url.PathEscape(id)+"/"+action, nil, nil)
}

func (c *Client) TriggerSchedule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/schedules/"+url.PathEscape(id)+"/trigger", nil, nil)
}

// ── marketplace ────────────────────────────────────────────────────────────────

func (c *Client) Marketplace(ctx context.Context, query, itemType string) ([]MarketItem, error) {
	q := "/marketplace?per_page=100"
	if query != "" {
		q += "&q=" + url.QueryEscape(query)
	}
	if itemType != "" {
		q += "&item_type=" + url.QueryEscape(itemType)
	}
	var out MarketList
	err := c.do(ctx, http.MethodGet, q, nil, &out)
	return out.Items, err
}

func (c *Client) InstallMarketItem(ctx context.Context, slug string) error {
	return c.do(ctx, http.MethodPost, "/marketplace/"+url.PathEscape(slug)+"/install", nil, nil)
}

// LikeRegistry toggles a like on a registry package.
func (c *Client) LikeRegistry(ctx context.Context, slug string) error {
	return c.do(ctx, http.MethodPost, "/marketplace/registry/"+url.PathEscape(slug)+"/like", nil, nil)
}

// InstalledSeeds lists the org's installed skills/tools/personas/agents (for the Installed view).
func (c *Client) InstalledSeeds(ctx context.Context) ([]Seed, error) {
	var out []Seed
	for _, k := range []struct{ path, kind string }{
		{"/skills", "skill"}, {"/tools", "tool"}, {"/personas", "persona"},
	} {
		var seeds []Seed
		if err := c.do(ctx, http.MethodGet, k.path, nil, &seeds); err != nil {
			continue
		}
		for i := range seeds {
			seeds[i].kind = k.kind
		}
		out = append(out, seeds...)
	}
	// agents (different shape — id/name)
	if agents, err := c.ListAgents(ctx); err == nil {
		for _, a := range agents {
			out = append(out, Seed{ID: a.ID, Name: a.Name, IsBuiltin: a.IsBuiltin, kind: "agent"})
		}
	}
	return out, nil
}

// UninstallSeed deletes an installed skill/tool/persona/agent by id.
func (c *Client) UninstallSeed(ctx context.Context, kind, id string) error {
	path := map[string]string{
		"skill": "/skills/", "tool": "/tools/", "persona": "/personas/", "agent": "/agents/",
	}[kind]
	if path == "" {
		return fmt.Errorf("unknown kind %q", kind)
	}
	return c.do(ctx, http.MethodDelete, path+url.PathEscape(id), nil, nil)
}

// SearchRegistry queries the live external NexoraMarketplace registry (public + the user's
// private packages), not the local instance catalog.
func (c *Client) SearchRegistry(ctx context.Context, query, itemType, tags string) ([]MarketItem, error) {
	q := "/marketplace/registry?per_page=100"
	if query != "" {
		q += "&q=" + url.QueryEscape(query)
	}
	if itemType != "" {
		q += "&item_type=" + url.QueryEscape(itemType)
	}
	if tags != "" {
		q += "&tags=" + url.QueryEscape(tags)
	}
	var out MarketList
	err := c.do(ctx, http.MethodGet, q, nil, &out)
	return out.Items, err
}

func (c *Client) ImportMarketURL(ctx context.Context, u string, acknowledgeRisk bool) error {
	_, err := c.ImportMarketURLFull(ctx, u, acknowledgeRisk)
	return err
}

// ToolEnvStatus describes a tool pack's Python requirements + whether the
// isolated per-pack venv is already provisioned.
type ToolEnvStatus struct {
	Requirements []string `json:"requirements"`
	EnvHash      string   `json:"env_hash"`
	Provisioned  bool     `json:"provisioned"`
	Enabled      bool     `json:"enabled"`
}

// RequiredEnvVar is an env var (API key/secret) a just-imported tool declares.
type RequiredEnvVar struct {
	Key   string   `json:"key"`
	Tools []string `json:"tools"`
}

// ImportResult is the parsed success body of a marketplace import: the Python
// requirements its tools need, the env vars (credentials) they declare, plus the
// risk-disclosure fields the backend echoes back for low-reputation packages.
type ImportResult struct {
	Name         string           `json:"name"`
	Requirements []ToolEnvStatus  `json:"python_requirements"`
	RequiredEnv  []RequiredEnvVar `json:"required_env_vars"`
	Disclaimer   string           `json:"disclaimer,omitempty"`
	WarningLevel string           `json:"warning_level,omitempty"`
	TrustTier    string           `json:"trust_tier,omitempty"`
}

// ImportMarketURLFull imports a package and returns the parsed success body. Pass
// acknowledgeRisk=true to clear the low-reputation gate; when false and the
// package is low-reputation, the backend replies 409 with a RiskAckRequired body
// (use AsRiskAck on the returned error).
func (c *Client) ImportMarketURLFull(ctx context.Context, u string, acknowledgeRisk bool) (*ImportResult, error) {
	var out ImportResult
	body := map[string]any{"url": u, "acknowledge_risk": acknowledgeRisk}
	err := c.do(ctx, http.MethodPost, "/marketplace/import", body, &out)
	return &out, err
}

// EnvVar is an org- or user-scoped environment variable (tool credential).
// Values are never returned by the API.
type EnvVar struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	OrgID       string `json:"org_id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	HasValue    bool   `json:"has_value"`
}

// ListEnvVars returns the caller's accessible env vars (org + personal).
func (c *Client) ListEnvVars(ctx context.Context) ([]EnvVar, error) {
	var out struct {
		EnvVars []EnvVar `json:"env_vars"`
	}
	err := c.do(ctx, http.MethodGet, "/env-vars", nil, &out)
	return out.EnvVars, err
}

// CreateEnvVar stores a new env var (scope "user" or "org").
func (c *Client) CreateEnvVar(ctx context.Context, scope, orgID, key, name, value, description string) (EnvVar, error) {
	body := map[string]string{"scope": scope, "key": key, "name": name, "value": value}
	if orgID != "" {
		body["org_id"] = orgID
	}
	if description != "" {
		body["description"] = description
	}
	var out EnvVar
	err := c.do(ctx, http.MethodPost, "/env-vars", body, &out)
	return out, err
}

// DeleteEnvVar removes an env var by id.
func (c *Client) DeleteEnvVar(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/env-vars/"+id, nil, nil)
}

// EnvVarConfigured reports, per requested key, the variables already set for it.
type EnvVarConfigured struct {
	Key        string `json:"key"`
	Configured []struct {
		ID    string `json:"id"`
		Scope string `json:"scope"`
		Name  string `json:"name"`
	} `json:"configured"`
}

// ResolveEnvVars reports which of the given keys already have a configured value.
func (c *Client) ResolveEnvVars(ctx context.Context, keys []string) ([]EnvVarConfigured, error) {
	var out struct {
		Keys []EnvVarConfigured `json:"keys"`
	}
	err := c.do(ctx, http.MethodPost, "/env-vars/resolve", map[string]any{"keys": keys}, &out)
	return out.Keys, err
}

// ProvisionToolEnv installs a tool pack's Python requirements into an isolated
// per-pack venv (idempotent). Returns ok + any server error message.
func (c *Client) ProvisionToolEnv(ctx context.Context, requirements []string) (bool, string, error) {
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	err := c.do(ctx, http.MethodPost, "/tool-envs/provision", map[string]any{"requirements": requirements}, &out)
	return out.OK, out.Error, err
}

// MarketplaceKeyConfigured reports whether a per-user marketplace API key is stored.
func (c *Client) MarketplaceKeyConfigured(ctx context.Context) (bool, error) {
	var out struct {
		Configured bool `json:"configured"`
	}
	err := c.do(ctx, http.MethodGet, "/users/me/marketplace-key", nil, &out)
	return out.Configured, err
}

// SetMarketplaceKey stores (or clears, with "") the per-user marketplace API key.
func (c *Client) SetMarketplaceKey(ctx context.Context, key string) error {
	return c.do(ctx, http.MethodPut, "/users/me/marketplace-key", map[string]string{"key": key}, nil)
}

// ── orgs ───────────────────────────────────────────────────────────────────────

func (c *Client) ListOrgs(ctx context.Context) ([]Org, error) {
	var out []Org
	err := c.do(ctx, http.MethodGet, "/orgs", nil, &out)
	return out, err
}

func (c *Client) SwitchOrg(ctx context.Context, orgID string) error {
	var tr TokenResponse
	if err := c.do(ctx, http.MethodPost, "/orgs/switch", map[string]string{"org_id": orgID}, &tr); err != nil {
		return err
	}
	c.setTokens(tr.AccessToken, tr.RefreshToken)
	return nil
}

// ── usage / devices ──────────────────────────────────────────────────────────────

func (c *Client) Usage(ctx context.Context) (*UsageSummary, error) {
	var out UsageSummary
	err := c.do(ctx, http.MethodGet, "/usage/summary?period_days=30", nil, &out)
	return &out, err
}

func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	var out []Device
	err := c.do(ctx, http.MethodGet, "/auth/device", nil, &out)
	return out, err
}

func (c *Client) RevokeDevice(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/auth/device/"+url.PathEscape(id), nil, nil)
}

// ── platform backup (superuser) ──────────────────────────────────────────────────

func (c *Client) StartBackup(ctx context.Context, scope string, includeVectors bool) (string, error) {
	var out BackupJob
	body := map[string]any{"scope": scope, "include_vectors": includeVectors}
	err := c.do(ctx, http.MethodPost, "/platform-backup/export", body, &out)
	return out.JobID, err
}

func (c *Client) BackupStatus(ctx context.Context, jobID string) (*BackupJob, error) {
	var out BackupJob
	err := c.do(ctx, http.MethodGet, "/platform-backup/"+url.PathEscape(jobID), nil, &out)
	return &out, err
}

// DownloadBackup writes the backup ZIP to destPath.
func (c *Client) DownloadBackup(ctx context.Context, jobID, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/platform-backup/"+url.PathEscape(jobID)+"/download", nil)
	if err != nil {
		return err
	}
	if tok := c.bearer(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return apiError(resp)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// ImportBackup uploads a backup ZIP to this instance's restore endpoint and returns the
// import summary. Used by `nexora migrate` to push a source instance's backup into a target.
func (c *Client) ImportBackup(ctx context.Context, zipPath, mode string, reembed, allowSecretLoss bool) (map[string]any, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filepath.Base(zipPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, err
	}
	_ = mw.WriteField("mode", mode)
	_ = mw.WriteField("reembed", strconv.FormatBool(reembed))
	_ = mw.WriteField("allow_secret_loss", strconv.FormatBool(allowSecretLoss))
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/platform-backup/import", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if tok := c.bearer(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	// Restore can be slow (re-embedding, big insert) — override the default 60s client timeout.
	hc := &http.Client{Timeout: 30 * time.Minute}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, apiError(resp)
	}
	var out struct {
		Status  string         `json:"status"`
		Summary map[string]any `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Summary, nil
}

// UploadKBFile streams a local file to the KB as multipart/form-data (field "file").
func (c *Client) UploadKBFile(ctx context.Context, kbID, filePath string) (*KBFile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, err
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/knowledge-bases/"+url.PathEscape(kbID)+"/files", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if tok := c.bearer(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, apiError(resp)
	}
	var out KBFile
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
