// Package tui is the Bubble Tea terminal UI. The root model owns a shared API client
// and the screens (chat, agents, providers, kb, tasks, sessions); screen-specific code
// lives in its own file but shares this model. See .claude/skills/bubbletea-tui.
package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
	"gitlab.com/parendum/nexora/nexora-cli/internal/ws"
)

const (
	tabChat = iota
	tabSessions
	tabChannels
	tabProjects
	tabBoard
	tabTasks
	tabIssues
	tabSchedules
	tabAgents
	tabKB
	tabProviders
	tabMarket
	tabSettings
)

var tabNames = []string{"chat", "sessions", "channels", "projects", "board", "tasks", "issues", "schedules", "agents", "kb", "providers", "market", "settings"}

// simpleTabs is the reduced tab set shown in "simple" UI mode (the rest stay reachable
// via the ctrl+k palette). Keeps the main menu compact.
var simpleTabs = []int{tabChat, tabSessions, tabSettings}

// visibleTabs returns the tab indices shown in the header for the current UI mode. In
// simple mode that's the reduced set plus the active tab (so a tab reached via ctrl+k is
// still shown highlighted).
func (m *model) visibleTabs() []int {
	if m.uiMode != "simple" {
		all := make([]int, len(tabNames))
		for i := range tabNames {
			all[i] = i
		}
		return all
	}
	vis := append([]int{}, simpleTabs...)
	inSet := false
	for _, t := range vis {
		if t == m.activeTab {
			inSet = true
			break
		}
	}
	if !inSet {
		vis = append(vis, m.activeTab)
	}
	return vis
}

// nextVisibleTab cycles to the next/prev visible tab from the active one.
func (m *model) nextVisibleTab(dir int) int {
	vis := m.visibleTabs()
	idx := 0
	for i, t := range vis {
		if t == m.activeTab {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(vis)) % len(vis)
	return vis[idx]
}

type model struct {
	client   *api.Client
	instName string
	version  string
	userName string
	theme    Theme

	width, height int
	ready         bool
	activeTab     int
	status        string
	connected     bool // false after a transport-level failure (server unreachable)

	spinner spinner.Model

	// data
	agents        []api.Agent
	chats         []api.Chat
	tasks         []api.Task
	providers     []api.Provider
	providerTypes []api.ProviderType
	chains        []api.Chain
	kbs           []api.KB
	kbFiles       []api.KBFile
	currentKB     *api.KB
	board         map[string][]api.Task
	boardCol      int
	boardRow      int
	boardOffset   int // scroll offset within the focused column
	issues        []api.Issue
	schedules     []api.Schedule
	projects      []api.Project
	market        []api.MarketItem
	marketType    string // "" = all, else skill|tool|agent|persona
	marketQuery   string
	marketTag     string
	marketView    string // "browse" | "installed" | "liked"
	installed     []api.Seed
	orgs          []api.Org
	devices       []api.Device
	envVars       []api.EnvVar
	envVarSel     int
	usage         *api.UsageSummary
	me            *api.Me
	mktKeySet     bool
	backupJobID   string
	backupStatus  string

	// chat state
	currentChat    *api.Chat
	activeAgentID  string // optional — empty = general/agentless chat
	activeModel    string // per-turn model override (set via /model)
	activeChainID  string // per-turn provider chain (set via /chain)
	messages       []api.Message
	renderedBlocks []string // cached rendered (styled) display blocks
	blockTexts     []string // plain text per block, parallel to renderedBlocks (for yank)

	// message-pick (yank) mode
	pickMode bool
	pickIdx  int

	// slash-command autocomplete popup
	slashOpen bool
	slashIdx  int
	slashHits []slashCmd

	// chat lateral panel
	sidebarOpen  bool
	sidebarPanel int
	chatUsage    *api.ChatUsage
	plans        []api.Plan
	logs         []api.LogEntry
	notes        []api.ChatNote
	chatFiles    []api.ChatFile

	wantChainPicker bool // open the chain picker once chains finish loading

	// agent editor selectors
	ms          *multiSelectModel
	msAgentID   string // agent being edited by the multi-select
	skillCat    []api.CatalogItem
	toolCat     []api.CatalogItem
	mcpCat      []api.CatalogItem
	personaCat  []api.PersonaItem

	// projects detail (subtabs: Overview/Agents/Resources/Tasks/Logs/Repository)
	currentProject *api.Project
	projSubtab     int
	projTasks      []api.Task
	projIssues     []api.Issue
	projAgents     []api.ProjectAgent
	projLogs       []api.LogEntry
	mcpServers     []api.McpServer  // full MCP records (known_tools) for resource editing
	pendingMcp     *api.McpServer   // server chosen in the picker, awaiting tool selection

	// local execution: run the agent's shell/file tools on THIS host instead of the
	// backend container. Off by default; the CLI is the gatekeeper.
	localExec     bool   // opt-in enabled
	localYolo     bool   // auto-approve every command (skip confirmation)
	localCwd      string // working directory tools run in
	pendingLocal  *localReq  // a tool_exec_request awaiting user confirmation
	localLog      []localCmd // feed of commands run on the host (shown in activity)
	localExpand   bool       // expand raw output of local commands (ctrl+l)
	saveLocal     func(le, yo bool)  // persist local-exec prefs to config
	uiMode        string             // "simple" | "advanced" — TUI complexity
	saveUI        func(mode string)  // persist ui mode to config
	settingsSub   int                // active settings subtab
	intSel        int                // cursor in the Integrations settings subtab
	reconnecting  bool              // a WS reconnect loop is in progress
	reconnectN    int               // backoff attempt counter
	chatStack     []*api.Chat         // back-stack for sub-chat navigation (parent ← sub)
	subSel        int                 // cursor in the Sub-agents sidebar panel
	subChats      []api.HierarchyNode // descendant sub-chats of the current chat (fetched)
	sentClientIDs map[string]bool     // client_message_ids we sent (to skip our own echo)
	hierNodes     []api.HierarchyNode // full chat hierarchy (ancestors+current+descendants)
	treeSel       int                 // cursor in the Tree panel
	projScroll     int // scroll offset for tasks/logs/agents lists
	// repository subtab
	repoTree     []api.GitTreeItem
	repoExpanded map[string]bool
	repoCursor   int
	repoFile     *api.GitFile
	repoFileLines []string // syntax-highlighted lines of the open file
	repoFileOff  int
	repoStatus   string
	assistantBuf   string
	streaming      bool
	pendingSend    string // first message to send once a freshly-created chat connects
	wsClient       *ws.Client
	events         chan ws.Frame

	// live turn activity
	toolActions []toolAct
	subAgents   []subAgentAct

	// markdown renderer (rebuilt on width change) + per-content render cache
	md      *glamour.TermRenderer
	mdWidth int
	mdCache map[string]string

	// components
	input        textinput.Model
	transcript   viewport.Model
	agentList    list.Model
	sessionList  list.Model
	providerList list.Model
	kbList       list.Model
	issueList    list.Model
	scheduleList list.Model
	marketList   list.Model
	projectList  list.Model

	// channels (external integrations: telegram, …)
	channelList    list.Model
	channels       []api.Integration
	currentChannel *api.Integration
	channelConvs   []api.ChannelConv
	channelConvSel int
	settingsVP   viewport.Model
	taskTable    table.Model
	kbFileTable  table.Model

	// overlays
	palette     list.Model
	paletteOpen bool
	form        *formModel
	picker      list.Model
	pickerOpen  bool
	pickerKind  string
	confirmOpen bool
	confirmKind string
	confirmID   string
	confirmID2  string
	confirmText string

	// pendingRiskURL holds the import URL awaiting risk acknowledgment (the confirm
	// overlay re-runs the import with acknowledge_risk=true).
	pendingRiskURL string
}

// Run starts the TUI against the given instance client.
func Run(client *api.Client, instName, version string, localExec, yolo bool, uiMode string, saveLocal func(le, yo bool), saveUI func(string)) error {
	m := newModel(client, instName, version)
	m.localExec = localExec
	m.localYolo = yolo && localExec
	m.saveLocal = saveLocal
	m.uiMode = uiMode
	if m.uiMode != "simple" {
		m.uiMode = "advanced"
	}
	m.saveUI = saveUI
	// Mouse capture enables wheel scrolling (we only act on wheel events, ignoring
	// clicks/motion). It does intercept the terminal's native drag-to-select, but every
	// modern terminal still lets you select text with SHIFT+drag while capture is on.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func newModel(client *api.Client, instName, version string) *model {
	th := DefaultTheme()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = th.Spinner

	ti := textinput.New()
	ti.Placeholder = "Message…  (enter to send · /help for commands)"
	ti.Prompt = "› "
	ti.CharLimit = 8000

	mkList := func(title string) list.Model {
		l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
		l.Title = title
		l.SetShowHelp(false)
		return l
	}

	pl := list.New(paletteItems(), list.NewDefaultDelegate(), 0, 0)
	pl.Title = "Command palette"
	pl.SetShowHelp(false)

	tbl := table.New(table.WithColumns([]table.Column{
		{Title: "Status", Width: 12}, {Title: "Task", Width: 40}, {Title: "Agent", Width: 20},
	}), table.WithFocused(true))

	kft := table.New(table.WithColumns([]table.Column{
		{Title: "Status", Width: 12}, {Title: "File", Width: 40}, {Title: "Chunks", Width: 8},
	}), table.WithFocused(true))

	return &model{
		client: client, instName: instName, version: version, theme: th,
		spinner: sp, input: ti,
		agentList: mkList("Agents"), sessionList: mkList("Sessions"),
		providerList: mkList("Providers"), kbList: mkList("Knowledge bases"),
		issueList: mkList("Issues"), scheduleList: mkList("Schedules"),
		marketList: mkList("Marketplace"), projectList: mkList("Projects"),
		channelList: mkList("Channels"),
		palette: pl, picker: mkList("Select"), taskTable: tbl, kbFileTable: kft,
		mdCache:    map[string]string{},
		connected:  true,
		status:     "Loading…",
		localCwd:   currentDir(),
	}
}

// persistLocal saves the local-exec preferences to config (survives restarts).
func (m *model) persistLocal() {
	if m.saveLocal != nil {
		m.saveLocal(m.localExec, m.localYolo)
	}
}

// localOperatorID returns the id of the builtin "Local Operator" agent if present.
func (m *model) localOperatorID() string {
	for _, a := range m.agents {
		if strings.EqualFold(a.Name, "Local Operator") {
			return a.ID
		}
	}
	return ""
}

// currentDir returns the CLI's working directory (where local-exec tools run).
func currentDir() string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return "."
}

func (m *model) Init() tea.Cmd {
	m.input.Focus()
	return tea.Batch(m.spinner.Tick, textinput.Blink, m.loadMe(), m.loadAgents(), m.loadChats(), loadChainsCmd(m.client))
}

func (m *model) loadMe() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		me, err := c.Me(context.Background())
		if err != nil {
			return nil
		}
		return meMsg{me}
	}
}

type meMsg struct{ me *api.Me }

// ── messages ────────────────────────────────────────────────────────────────────

type errMsg struct{ err error }
type okMsg struct{ note string }

// riskAckMsg asks the user to acknowledge a low-reputation marketplace package
// (the import 409'd with a risk-ack body). importURL/name re-drive the import on
// acknowledge.
type riskAckMsg struct {
	importURL string
	name      string
	detail    *api.RiskAckRequired
}
type agentsMsg struct{ items []api.Agent }
type chatsMsg struct{ items []api.Chat }
type tasksMsg struct{ items []api.Task }
type providersMsg struct{ items []api.Provider }
type providerTypesMsg struct{ items []api.ProviderType }
type kbsMsg struct{ items []api.KB }
type kbFilesMsg struct{ items []api.KBFile }
type boardMsg struct{ cols map[string][]api.Task }
type issuesMsg struct{ items []api.Issue }
type schedulesMsg struct{ items []api.Schedule }
type projectsMsg struct{ items []api.Project }
type marketMsg struct{ items []api.MarketItem }
type installedMsg struct{ items []api.Seed }
type chatUsageMsg struct{ u *api.ChatUsage }
type plansMsg struct{ items []api.Plan }
type logsMsg struct{ items []api.LogEntry }
type notesMsg struct{ items []api.ChatNote }
type chatFilesMsg struct{ items []api.ChatFile }
type settingsMsg struct {
	me      *api.Me
	orgs    []api.Org
	usage   *api.UsageSummary
	devices []api.Device
	mktKey  bool
	envVars []api.EnvVar
}
type backupTickMsg struct{ job *api.BackupJob }
type wsReadyMsg struct {
	chat   *api.Chat
	client *ws.Client
	events chan ws.Frame
	msgs   []api.Message
}
type wsFrameMsg struct{ f ws.Frame }
type wsClosedMsg struct{}
type sentMsg struct{}

// ── load commands ───────────────────────────────────────────────────────────────

func (m *model) loadAgents() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		a, err := c.ListAgents(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return agentsMsg{a}
	}
}
func (m *model) loadChats() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ch, err := c.ListChats(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return chatsMsg{ch}
	}
}
func (m *model) loadTasks(chatID string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		t, err := c.Tasks(context.Background(), chatID)
		if err != nil {
			return errMsg{err}
		}
		return tasksMsg{t}
	}
}
func (m *model) loadProviders() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		p, err := c.ListProviders(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return providersMsg{p}
	}
}
func (m *model) loadProviderTypes() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		t, err := c.ListProviderTypes(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return providerTypesMsg{t}
	}
}
func (m *model) loadKBs() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		k, err := c.ListKBs(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return kbsMsg{k}
	}
}
func (m *model) loadKBFiles(kbID string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		f, err := c.ListKBFiles(context.Background(), kbID)
		if err != nil {
			return errMsg{err}
		}
		return kbFilesMsg{f}
	}
}
func (m *model) loadBoard() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		b, err := c.Board(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return boardMsg{b}
	}
}
func (m *model) loadIssues() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		i, err := c.ListIssues(context.Background(), "")
		if err != nil {
			return errMsg{err}
		}
		return issuesMsg{i}
	}
}
func (m *model) loadSchedules() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		s, err := c.ListSchedules(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return schedulesMsg{s}
	}
}
func (m *model) loadProjects() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		p, err := c.ListProjects(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return projectsMsg{p}
	}
}
func (m *model) loadMarket(query string) tea.Cmd {
	c := m.client
	m.marketQuery = query
	itemType := m.marketType
	tag := m.marketTag
	return func() tea.Msg {
		// Live registry search (public + the user's private packages via the stored key).
		items, err := c.SearchRegistry(context.Background(), query, itemType, tag)
		if err != nil {
			// Fall back to the local instance catalog if the registry is unreachable.
			if local, lerr := c.Marketplace(context.Background(), query, itemType); lerr == nil {
				return marketMsg{local}
			}
			return errMsg{err}
		}
		return marketMsg{items}
	}
}
type catalogsMsg struct {
	skills   []api.CatalogItem
	tools    []api.CatalogItem
	mcps     []api.CatalogItem
	personas []api.PersonaItem
}

func (m *model) loadCatalogs() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		return catalogsMsg{
			skills:   c.SkillCatalog(context.Background()),
			tools:    c.ToolCatalog(context.Background()),
			mcps:     c.McpCatalog(context.Background()),
			personas: c.PersonaCatalog(context.Background()),
		}
	}
}

func (m *model) loadProjectDetail(id string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		p, err := c.Project(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		tasks, _ := c.ProjectTasks(context.Background(), id)
		issues, _ := c.ProjectIssues(context.Background(), id)
		agents, _ := c.ProjectAgents(context.Background(), id)
		logs, _ := c.ProjectLogs(context.Background(), id)
		return projectDetailMsg{p, tasks, issues, agents, logs}
	}
}

type projectDetailMsg struct {
	p      *api.Project
	tasks  []api.Task
	issues []api.Issue
	agents []api.ProjectAgent
	logs   []api.LogEntry
}

// loadRepoTree fetches the project's git file tree via the proxy.
func (m *model) loadRepoTree(p api.Project) tea.Cmd {
	c := m.client
	cred, repo, branch := p.RepoCredID, p.RepoURL, p.RepoBranch
	return func() tea.Msg {
		items, err := c.GitTree(context.Background(), cred, repo, branch)
		return repoTreeMsg{items, err}
	}
}

func (m *model) loadRepoFile(p api.Project, path string) tea.Cmd {
	c := m.client
	cred, repo, branch := p.RepoCredID, p.RepoURL, p.RepoBranch
	return func() tea.Msg {
		f, err := c.GitFile(context.Background(), cred, repo, path, branch)
		return repoFileMsg{f, err}
	}
}

// loadHierarchy fetches the current chat's descendant sub-chats (persisted, so the
// Sub-agents panel survives navigation — not just this turn's live frames).
func (m *model) loadHierarchy(id string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		nodes, err := c.Hierarchy(context.Background(), id)
		if err != nil {
			return hierarchyMsg{nil}
		}
		return hierarchyMsg{nodes}
	}
}

type hierarchyMsg struct{ nodes []api.HierarchyNode }

func (m *model) loadMcpServers() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		items, _ := c.McpServers(context.Background())
		return mcpServersMsg{items}
	}
}

type mcpServersMsg struct{ items []api.McpServer }

type repoTreeMsg struct {
	items []api.GitTreeItem
	err   error
}
type repoFileMsg struct {
	file *api.GitFile
	err  error
}

func (m *model) loadInstalled() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		seeds, err := c.InstalledSeeds(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return installedMsg{seeds}
	}
}

func (m *model) loadSettings() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		// best-effort aggregate; ignore individual errors
		me, _ := c.Me(context.Background())
		orgs, _ := c.ListOrgs(context.Background())
		u, _ := c.Usage(context.Background())
		devs, _ := c.ListDevices(context.Background())
		mktKey, _ := c.MarketplaceKeyConfigured(context.Background())
		evs, _ := c.ListEnvVars(context.Background())
		return settingsMsg{me: me, orgs: orgs, usage: u, devices: devs, mktKey: mktKey, envVars: evs}
	}
}

// ── chat commands ───────────────────────────────────────────────────────────────

func (m *model) openChat(chat *api.Chat) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		msgs, err := c.Messages(context.Background(), chat.ID)
		if err != nil {
			return errMsg{err}
		}
		wsc, err := ws.Dial(context.Background(), c.BaseURL(), chat.ID, c.WSToken())
		if err != nil {
			return errMsg{err}
		}
		ch := make(chan ws.Frame, 64)
		go wsc.ReadInto(ch)
		return wsReadyMsg{chat: chat, client: wsc, events: ch, msgs: msgs}
	}
}
// newChat creates a chat (agentID may be empty for a general/agentless chat) and opens it.
func (m *model) newChat(agentID, title string) tea.Cmd {
	c := m.client
	open := m.openChat
	if title == "" {
		title = "New chat"
	}
	return func() tea.Msg {
		chat, err := c.CreateChat(context.Background(), api.CreateChatRequest{Title: title, AgentID: agentID})
		if err != nil {
			return errMsg{err}
		}
		return open(chat)()
	}
}
func waitForFrame(ch chan ws.Frame) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-ch
		if !ok {
			return wsClosedMsg{}
		}
		return wsFrameMsg{f}
	}
}

// ── auto-reconnect ────────────────────────────────────────────────────────────────

type reconnectAttemptMsg struct{ chat *api.Chat }
type reconnectFailedMsg struct{ chat *api.Chat }
type authExpiredMsg struct{}
type wsReconnectedMsg struct {
	chatID string
	client *ws.Client
	events chan ws.Frame
	msgs   []api.Message
}

// reconnectTick schedules the next reconnect attempt: a fixed 1s interval, retried
// indefinitely until the socket comes back (no backoff, no cap).
func reconnectTick(chat *api.Chat) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return reconnectAttemptMsg{chat: chat}
	})
}

// reconnectWS re-dials the chat socket and reloads messages to catch up on anything that
// happened while disconnected. Failure schedules another backoff attempt.
func (m *model) reconnectWS(chat *api.Chat) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		msgs, err := c.Messages(context.Background(), chat.ID)
		if err != nil {
			if ae, ok := err.(*api.APIError); ok && ae.Status == 401 {
				return authExpiredMsg{} // refresh failed — re-login required
			}
			return reconnectFailedMsg{chat: chat} // transient — retry
		}
		wsc, err := ws.Dial(context.Background(), c.BaseURL(), chat.ID, c.WSToken())
		if err != nil {
			return reconnectFailedMsg{chat: chat}
		}
		ch := make(chan ws.Frame, 64)
		go wsc.ReadInto(ch)
		return wsReconnectedMsg{chatID: chat.ID, client: wsc, events: ch, msgs: msgs}
	}
}
// genClientID makes a unique per-message id for idempotency + own-echo detection.
func genClientID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "cli-fallback"
	}
	return "cli-" + hex.EncodeToString(b)
}

func (m *model) sendTurn(content string) tea.Cmd {
	wsc := m.wsClient
	// Tag this send so we can recognise (and skip) its own user_message echo broadcast back
	// to us — while still showing the SAME message arriving from another client (web/telegram).
	cid := genClientID()
	if m.sentClientIDs == nil {
		m.sentClientIDs = map[string]bool{}
	}
	m.sentClientIDs[cid] = true
	opts := ws.SendOpts{
		AgentID:         m.activeAgentID,
		ModelName:       m.activeModel,
		ChainID:         m.activeChainID,
		EnableAgent:     m.activeAgentID != "",
		ClientMessageID: cid,
		LocalExec:       m.localExec,
		Cwd:             m.localCwd,
		ClientOS:        runtime.GOOS,
	}
	return func() tea.Msg {
		if wsc == nil {
			return errMsg{fmt.Errorf("no open chat")}
		}
		if err := wsc.Send(content, opts); err != nil {
			return errMsg{err}
		}
		return sentMsg{}
	}
}

// ── update ────────────────────────────────────────────────────────────────────

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		return m, nil

	case tea.MouseMsg:
		// Only react to the wheel; ignore clicks/motion so SHIFT+drag text selection still
		// works. Wheel scrolls the active view a few lines at a time (smooth, not paginated).
		delta := 0
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			delta = -3
		case tea.MouseButtonWheelDown:
			delta = 3
		}
		if delta == 0 {
			return m, nil
		}
		switch m.activeTab {
		case tabChat:
			if m.currentChat != nil {
				m.transcript.SetYOffset(m.transcript.YOffset + delta)
			}
			return m, nil
		case tabSettings:
			m.settingsVP.SetYOffset(m.settingsVP.YOffset + delta)
			return m, nil
		default:
			return m.updateActive(msg) // lists scroll via their own mouse handling
		}

	case tea.KeyMsg:
		// overlays capture keys first
		if m.ms != nil {
			switch r := m.ms.update(msg).(type) {
			case msCancelMsg:
				m.ms = nil
				return m, nil
			case msSubmitMsg:
				return m.onMultiSelect(r)
			}
			return m, nil
		}
		if m.form != nil {
			return m.handleFormKey(msg)
		}
		if m.pickerOpen {
			return m.handlePickerKey(msg)
		}
		if m.confirmOpen {
			return m.handleConfirmKey(msg)
		}
		if m.paletteOpen {
			return m.updatePalette(msg)
		}
		switch msg.String() {
		case "ctrl+c":
			m.cleanup()
			return m, tea.Quit
		case "ctrl+k":
			m.paletteOpen = true
			return m, nil
		case "tab":
			m.activeTab = m.nextVisibleTab(1)
			return m, m.onTabChange()
		case "shift+tab":
			m.activeTab = m.nextVisibleTab(-1)
			return m, m.onTabChange()
		}
		return m.updateActive(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case errMsg:
		// An APIError means the server replied (we're connected) → show its clean detail.
		// Any other error is transport-level (server unreachable) → just flip to Disconnected,
		// no scary dial string in the UI.
		if ae, ok := msg.err.(*api.APIError); ok {
			m.connected = true
			if ae.Status == 401 {
				// token rejected and refresh couldn't recover (e.g. password changed)
				m.status = "Session expired — quit (ctrl+c) and run `nexora login` to re-authenticate"
			} else {
				m.status = ae.Error()
			}
		} else {
			m.connected = false
			m.status = ""
		}
		return m, nil
	case okMsg:
		m.connected = true
		m.status = msg.note
		return m, nil

	case riskAckMsg:
		m.connected = true
		m.pendingRiskURL = msg.importURL
		m.openConfirm("market_risk_ack", msg.importURL, msg.name, m.riskAckText(msg.name, msg.detail))
		return m, nil

	case chainedMsg:
		// process the immediate msg, then run the follow-up command (e.g. a reload)
		updated, cmd := m.Update(msg.now)
		return updated, tea.Batch(cmd, msg.next)

	case agentsMsg:
		m.connected = true
		m.agents = msg.items
		m.agentList.SetItems(toAgentItems(msg.items))
		// Pick a default agent only when none is chosen yet — never override an explicit
		// pick. With local-exec on, the default is the Local Operator (direct, terse);
		// otherwise the first agent. The user can /agent to any agent and it sticks — the
		// local_exec_env prompt lets any agent run local tools and decide on delegation.
		if m.activeAgentID == "" {
			if m.localExec {
				if id := m.localOperatorID(); id != "" {
					m.activeAgentID = id
				}
			}
			if m.activeAgentID == "" && len(msg.items) > 0 {
				m.activeAgentID = msg.items[0].ID
			}
		}
		m.status = m.readyStatus()
		return m, nil
	case chatsMsg:
		m.connected = true
		m.chats = msg.items
		m.sessionList.SetItems(toChatItems(msg.items))
		m.status = m.readyStatus()
		return m, nil
	case tasksMsg:
		m.tasks = msg.items
		m.taskTable.SetRows(toTaskRows(msg.items, m.theme))
		return m, nil
	case providersMsg:
		m.providers = msg.items
		m.providerList.SetItems(toProviderItems(msg.items))
		return m, nil
	case providerTypesMsg:
		m.providerTypes = msg.items
		return m, nil
	case kbsMsg:
		m.kbs = msg.items
		m.kbList.SetItems(toKBItems(msg.items))
		return m, nil
	case kbFilesMsg:
		m.kbFiles = msg.items
		m.kbFileTable.SetRows(toKBFileRows(msg.items, m.theme))
		return m, nil
	case boardMsg:
		m.board = msg.cols
		m.clampBoardCursor()
		return m, nil
	case issuesMsg:
		m.issues = msg.items
		m.issueList.SetItems(toIssueItems(msg.items))
		return m, nil
	case schedulesMsg:
		m.schedules = msg.items
		m.scheduleList.SetItems(toScheduleItems(msg.items))
		return m, nil
	case projectsMsg:
		m.projects = msg.items
		m.projectList.SetItems(toProjectItems(msg.items))
		return m, nil
	case channelsMsg:
		m.connected = true
		m.channels = msg.items
		m.channelList.SetItems(toChannelItems(msg.items))
		if m.activeTab == tabSettings {
			m.settingsVP.SetContent(m.settingsContent())
		}
		return m, nil
	case channelConvsMsg:
		m.connected = true
		m.channelConvs = msg.items
		return m, nil
	case chainsMsg:
		m.chains = msg.items
		if m.wantChainPicker {
			m.wantChainPicker = false
			m.openChainPicker()
			m.status = m.readyStatus()
		}
		return m, nil
	case meMsg:
		if msg.me != nil {
			m.me = msg.me
			m.userName = orDefault(msg.me.FullName, msg.me.Email)
			m.rebuildBlocks() // re-label existing "you" blocks
			if m.activeTab == tabSettings {
				m.settingsVP.SetContent(m.settingsContent())
			}
		}
		return m, nil
	case marketMsg:
		m.market = msg.items
		m.applyMarketFilter()
		return m, nil
	case installedMsg:
		m.installed = msg.items
		m.applyMarketFilter()
		return m, nil
	case usagePollMsg:
		return m.onUsagePoll()
	case chatUsageMsg:
		m.connected = true
		m.chatUsage = msg.u
		return m, nil
	case plansMsg:
		m.plans = msg.items
		return m, nil
	case logsMsg:
		m.logs = msg.items
		return m, nil
	case notesMsg:
		m.notes = msg.items
		return m, nil
	case chatFilesMsg:
		m.chatFiles = msg.items
		return m, nil
	case catalogsMsg:
		m.skillCat, m.toolCat, m.mcpCat, m.personaCat = msg.skills, msg.tools, msg.mcps, msg.personas
		return m, nil
	case projectDetailMsg:
		m.currentProject = msg.p
		m.projTasks = msg.tasks
		m.projIssues = msg.issues
		m.projAgents = msg.agents
		m.projLogs = msg.logs
		m.connected = true
		return m, nil
	case mcpServersMsg:
		m.mcpServers = msg.items
		return m, nil
	case hierarchyMsg:
		m.hierNodes = msg.nodes
		// derive descendant sub-chats for the Sub-agents panel
		m.subChats = m.subChats[:0]
		for _, n := range msg.nodes {
			if n.Depth > 0 {
				m.subChats = append(m.subChats, n)
			}
		}
		m.renderTranscript()
		return m, nil
	case localDoneMsg:
		if msg.idx >= 0 && msg.idx < len(m.localLog) {
			if msg.ok {
				m.localLog[msg.idx].state = "ok"
			} else {
				m.localLog[msg.idx].state = "err"
			}
			m.localLog[msg.idx].output = msg.output
		}
		m.status = msg.note
		m.renderTranscript()
		return m, nil
	case repoTreeMsg:
		if msg.err != nil {
			m.repoStatus = "tree: " + cleanErr(msg.err)
			return m, nil
		}
		m.repoTree = msg.items
		m.repoStatus = ""
		if len(msg.items) == 0 {
			m.repoStatus = "empty tree (check repo URL / credential in web)"
		}
		return m, nil
	case repoFileMsg:
		if msg.err != nil {
			m.repoStatus = "file: " + cleanErr(msg.err)
			return m, nil
		}
		m.repoFile = msg.file
		m.repoFileLines = highlightCode(msg.file.Path, msg.file.Content)
		m.repoFileOff = 0
		m.repoStatus = ""
		return m, nil
	case msSubmitMsg:
		return m.onMultiSelect(msg)
	case msCancelMsg:
		m.ms = nil
		return m, nil
	case settingsMsg:
		if msg.me != nil {
			m.me = msg.me
		}
		m.orgs = msg.orgs
		m.usage = msg.usage
		m.devices = msg.devices
		m.mktKeySet = msg.mktKey
		m.envVars = msg.envVars
		m.settingsVP.SetContent(m.settingsContent())
		return m, nil
	case backupTickMsg:
		if msg.job != nil {
			m.backupStatus = msg.job.Status
			if msg.job.Status == "done" {
				return m, m.downloadBackup()
			}
			if msg.job.Status == "failed" {
				m.status = "backup failed: " + msg.job.Error
				return m, nil
			}
			return m, m.pollBackup()
		}
		return m, nil

	case formSubmitMsg:
		return m.onFormSubmit(msg)
	case formCancelMsg:
		m.form = nil
		return m, nil

	case wsReadyMsg:
		m.connected = true
		m.reconnecting = false
		m.reconnectN = 0
		m.subSel = 0
		m.treeSel = 0
		m.closeWS()
		m.currentChat = msg.chat
		m.wsClient = msg.client
		m.events = msg.events
		m.messages = msg.msgs
		m.assistantBuf = ""
		m.toolActions = nil
		m.subAgents = nil
		m.rebuildBlocks()
		m.activeTab = tabChat
		m.input.Focus()
		m.subChats = nil
		cmds := []tea.Cmd{waitForFrame(m.events), m.loadTasks(msg.chat.ID), m.loadHierarchy(msg.chat.ID), textinput.Blink}
		// flush a queued first message (general-chat "type to start" flow)
		if m.pendingSend != "" {
			content := m.pendingSend
			m.pendingSend = ""
			m.appendMessage(api.Message{Role: "user", Content: content, UserName: "you"})
			m.streaming = true
			cmds = append(cmds, m.sendTurn(content))
			m.status = "Connected · " + msg.chat.Title
		} else {
			m.status = "Connected · " + msg.chat.Title
		}
		m.renderTranscript()
		return m, tea.Batch(cmds...)
	case wsFrameMsg:
		return m.handleFrame(msg.f)
	case wsClosedMsg:
		m.streaming = false
		// NOTE: don't flip the connection dot here — that tracks API reachability, not the
		// chat WebSocket (which may be absent when no chat is open). WS reconnect is shown
		// via the status line instead.
		if m.currentChat == nil {
			m.status = "chat stream closed"
			return m, nil
		}
		// Auto-reconnect (e.g. the server restarted). Start one loop only.
		if m.reconnecting {
			return m, nil
		}
		m.reconnecting = true
		m.reconnectN = 0
		m.status = "reconnecting…"
		return m, reconnectTick(m.currentChat)
	case authExpiredMsg:
		// refresh token rejected (e.g. password changed → token_version bumped). No amount
		// of retrying helps; tell the user to re-authenticate and stop the reconnect loop.
		m.reconnecting = false
		m.status = "Session expired — quit (ctrl+c) and run `nexora login` to re-authenticate"
		return m, nil
	case reconnectAttemptMsg:
		if m.currentChat == nil || msg.chat.ID != m.currentChat.ID {
			m.reconnecting = false
			return m, nil
		}
		return m, m.reconnectWS(msg.chat)
	case reconnectFailedMsg:
		if m.currentChat == nil || msg.chat.ID != m.currentChat.ID {
			m.reconnecting = false
			return m, nil
		}
		m.reconnectN++
		m.status = "reconnecting… (retry " + itoa(m.reconnectN) + ")"
		return m, reconnectTick(m.currentChat)
	case wsReconnectedMsg:
		if m.currentChat == nil || msg.chatID != m.currentChat.ID {
			_ = msg.client.Close()
			return m, nil
		}
		m.closeWS()
		m.wsClient = msg.client
		m.events = msg.events
		m.connected = true
		m.reconnecting = false
		m.reconnectN = 0
		m.status = "reconnected"
		if len(msg.msgs) > 0 {
			m.messages = msg.msgs
			m.rebuildBlocks()
			m.renderTranscript()
		}
		return m, waitForFrame(m.events)
	case sentMsg:
		return m, nil
	}

	return m.updateActive(msg)
}

func (m *model) updateActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	// per-screen key intents
	if k, ok := msg.(tea.KeyMsg); ok {
		switch m.activeTab {
		case tabAgents:
			if cmd, handled := m.agentsKey(k); handled {
				return m, cmd
			}
		case tabProviders:
			if cmd, handled := m.providersKey(k); handled {
				return m, cmd
			}
		case tabKB:
			if cmd, handled := m.kbKey(k); handled {
				return m, cmd
			}
		case tabBoard:
			if cmd, handled := m.boardKey(k); handled {
				return m, cmd
			}
		case tabIssues:
			if cmd, handled := m.issuesKey(k); handled {
				return m, cmd
			}
		case tabSchedules:
			if cmd, handled := m.schedulesKey(k); handled {
				return m, cmd
			}
		case tabProjects:
			if cmd, handled := m.projectsKey(k); handled {
				return m, cmd
			}
		case tabChannels:
			if cmd, handled := m.channelsKey(k); handled {
				return m, cmd
			}
		case tabMarket:
			if cmd, handled := m.marketKey(k); handled {
				return m, cmd
			}
		case tabSettings:
			if cmd, handled := m.settingsKey(k); handled {
				m.settingsVP.SetContent(m.settingsContent()) // re-render after subtab/toggle
				return m, cmd
			}
		case tabSessions:
			if m.sessionList.FilterState() != list.Filtering {
				switch k.String() {
				case "enter":
					return m.selectSession()
				case "d":
					if it, ok := m.sessionList.SelectedItem().(chatItem); ok {
						m.openConfirm("chat_delete", it.c.ID, "", "Delete session \""+orDefault(it.c.Title, "(untitled)")+"\"?")
					}
					return m, nil
				}
			}
		}
	}
	var cmd tea.Cmd
	switch m.activeTab {
	case tabChat:
		return m.updateChat(msg)
	case tabAgents:
		m.agentList, cmd = m.agentList.Update(msg)
	case tabProviders:
		m.providerList, cmd = m.providerList.Update(msg)
	case tabKB:
		if m.currentKB != nil {
			m.kbFileTable, cmd = m.kbFileTable.Update(msg)
		} else {
			m.kbList, cmd = m.kbList.Update(msg)
		}
	case tabProjects:
		m.projectList, cmd = m.projectList.Update(msg)
	case tabChannels:
		if m.currentChannel == nil { // list mode → delegate to the list (filter/scroll)
			m.channelList, cmd = m.channelList.Update(msg)
		}
	case tabTasks:
		m.taskTable, cmd = m.taskTable.Update(msg)
	case tabIssues:
		m.issueList, cmd = m.issueList.Update(msg)
	case tabSchedules:
		m.scheduleList, cmd = m.scheduleList.Update(msg)
	case tabMarket:
		m.marketList, cmd = m.marketList.Update(msg)
	case tabSettings:
		m.settingsVP, cmd = m.settingsVP.Update(msg)
	case tabSessions:
		m.sessionList, cmd = m.sessionList.Update(msg)
	}
	return m, cmd
}

func (m *model) onTabChange() tea.Cmd {
	switch m.activeTab {
	case tabProjects:
		return m.loadProjects()
	case tabChannels:
		return m.loadChannels()
	case tabAgents:
		return tea.Batch(m.loadAgents(), m.loadCatalogs())
	case tabTasks:
		if m.currentChat != nil {
			return m.loadTasks(m.currentChat.ID)
		}
	case tabSessions:
		return m.loadChats()
	case tabProviders:
		return tea.Batch(m.loadProviders(), m.loadProviderTypes())
	case tabKB:
		if m.currentKB != nil {
			return m.loadKBFiles(m.currentKB.ID)
		}
		return m.loadKBs()
	case tabBoard:
		return m.loadBoard()
	case tabIssues:
		return tea.Batch(m.loadIssues(), m.loadProjects())
	case tabSchedules:
		return m.loadSchedules()
	case tabMarket:
		if m.marketView == "" {
			m.marketView = "browse"
		}
		if m.marketView == "installed" {
			return m.loadInstalled()
		}
		return m.loadMarket(m.marketQuery)
	case tabSettings:
		return tea.Batch(m.loadSettings(), m.loadMe())
	}
	return nil
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m *model) View() string {
	if !m.ready {
		return "starting…"
	}
	header := m.renderHeader()
	var body string
	switch m.activeTab {
	case tabChat:
		body = m.viewChat()
	case tabAgents:
		body = m.agentList.View()
	case tabProviders:
		body = m.viewProviders()
	case tabKB:
		body = m.viewKB()
	case tabProjects:
		body = m.viewProjects()
	case tabChannels:
		body = m.viewChannels()
	case tabTasks:
		body = m.viewTasks()
	case tabBoard:
		body = m.viewBoard()
	case tabIssues:
		body = m.viewIssues()
	case tabSchedules:
		body = m.viewSchedules()
	case tabMarket:
		body = m.viewMarket()
	case tabSettings:
		body = m.settingsVP.View()
	case tabSessions:
		body = m.sessionList.View()
	}
	footer := m.renderFooter()
	view := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)

	switch {
	case m.ms != nil:
		return m.overlay(view, m.ms.view(m.theme, min(96, m.width-6), min(22, m.height-4)))
	case m.form != nil:
		return m.overlay(view, m.form.view(m.theme, min(70, m.width-4)))
	case m.pickerOpen:
		return m.overlay(view, m.picker.View())
	case m.confirmOpen:
		return m.overlay(view, m.confirmView())
	case m.paletteOpen:
		return m.overlay(view, m.palette.View())
	}
	return view
}

func (m *model) renderHeader() string {
	t := m.theme
	vis := m.visibleTabs()
	tabs := make([]string, 0, len(vis))
	for _, i := range vis {
		name := tabNames[i]
		if i == m.activeTab {
			tabs = append(tabs, t.TabActive.Render(name))
		} else {
			tabs = append(tabs, t.TabInactive.Render(name))
		}
	}
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("#0B0B10")).Background(t.Accent).
		Bold(true).Padding(0, 1).Render("◆ " + t.Title)
	// connection dot: green = connected, red = disconnected
	dotColor := t.Good
	if !m.connected {
		dotColor = t.Bad
	}
	title += " " + lipgloss.NewStyle().Foreground(dotColor).Bold(true).Render("●")
	row := lipgloss.JoinHorizontal(lipgloss.Center, title, " ", strings.Join(tabs, " "))
	rule := lipgloss.NewStyle().Foreground(t.Subtle).Render(strings.Repeat("─", max(0, m.width)))
	return row + "\n" + rule
}

func (m *model) renderFooter() string {
	t := m.theme
	// connection moved to the header dot — footer just shows local-exec chips + status/help.
	left := ""
	if m.localExec {
		left += lipgloss.NewStyle().Foreground(t.Accent2).Bold(true).Render(" LOCAL")
		if m.localYolo {
			left += lipgloss.NewStyle().Foreground(t.Bad).Bold(true).Render("  YOLO")
		}
	}
	help := t.Help.Render(m.contextHelp() + " ")
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(help))
	return left + strings.Repeat(" ", gap) + help
}

func (m *model) contextHelp() string {
	switch m.activeTab {
	case tabChat:
		if m.pickMode {
			return "pick · ↑↓ move · y copy · esc cancel"
		}
		if m.slashOpen {
			return "↑↓ choose · tab complete · enter run · esc close"
		}
		if m.currentChat != nil && len(m.chatStack) > 0 {
			return "ctrl+g sub-chats / ← back · ctrl+b panel · / commands"
		}
		if m.sidebarOpen {
			return "ctrl+b close panel · ctrl+o switch panel · ctrl+g sub-chats · / commands"
		}
		return "/ commands · ctrl+b panel · ctrl+g sub-chats · wheel scroll · shift+drag select"
	case tabAgents:
		return "[n]ew [e]dit [d]el · [s]kills [o]tools [M]cps [p]ersona · [tab] [ctrl+k]"
	case tabProviders:
		return "[n]ew [e]dit [d]el · [tab] switch [ctrl+k]"
	case tabKB:
		if m.currentKB != nil {
			return "[u]pload [i]ngest-url [d]el [esc]back · [tab] [ctrl+k]"
		}
		return "[n]ew [e]dit [d]el [enter]files · [tab] [ctrl+k]"
	case tabBoard:
		return "[←→] col [↑↓] task [</>] move [e]dit [r]efresh · [tab] [ctrl+k]"
	case tabIssues:
		return "[n]ew [e]dit [d]el [c]lose [o]pen [r]efresh · [tab] [ctrl+k]"
	case tabSchedules:
		return "[n]ew [e]dit [space] on/off [t]rigger [d]el · [tab] [ctrl+k]"
	case tabMarket:
		if m.marketView == "installed" {
			return "[b/I/L] view · [1-5] type · [u]ninstall · [r]efresh · [tab] [ctrl+k]"
		}
		return "[b/I/L] view · [1-5] type · [enter]install [l]ike · [/]search [t]ag [i]import"
	case tabSettings:
		return "[1-9]/[ ] subtab · [e] edit · [space] interface · [d] device · [o]rg [k]ey [b]ackup · [r]"
	case tabSessions:
		return "[enter] open · [d]elete · [/] filter · [tab] [ctrl+k]"
	case tabProjects:
		if m.currentProject != nil {
			if m.projSubtab == subRepo {
				return "repo · ↑↓ move · →/enter open · ← collapse · [1-6] subtab · [esc] back"
			}
			if m.projSubtab == subResources {
				return "[t] tools · [M] MCP · [e] env vars · [1-6] subtab · [esc] back"
			}
			return "[1-6]/[ ] subtab · ↑↓ scroll · [esc] back · [r]efresh"
		}
		return "[enter] open · [n]ew (git import) · [r]efresh · [/] filter · [tab] [ctrl+k]"
	case tabChannels:
		if m.currentChannel != nil {
			return "↑↓ select · [enter] open conversation · [esc] back · [r]efresh"
		}
		return "[enter] conversations · [n]ew channel · [a] start/stop · [r]efresh"
	case tabTasks:
		return "tasks for current chat · board tab = kanban · [tab] [ctrl+k]"
	default:
		return "[tab] switch  [ctrl+k] palette  [ctrl+c] quit"
	}
}

func (m *model) layout() {
	bodyH := max(3, m.height-4)
	mainW := m.width - m.sidebarWidth()
	m.transcript.Width = max(20, mainW)
	m.transcript.Height = max(3, bodyH-3)
	m.input.Width = max(10, mainW-4)
	for _, l := range []*list.Model{&m.agentList, &m.sessionList, &m.providerList, &m.kbList, &m.issueList, &m.scheduleList, &m.marketList, &m.projectList, &m.channelList} {
		l.SetSize(m.width, bodyH)
	}
	// market view prepends a 2-line header (views + type rows) + blank separator,
	// so its list must be shorter or it overflows and scrolls the tab bar off-screen.
	m.marketList.SetSize(m.width, max(3, bodyH-3))
	m.settingsVP.Width = m.width
	m.settingsVP.Height = bodyH
	m.picker.SetSize(min(60, m.width-4), min(14, bodyH))
	m.palette.SetSize(min(60, m.width-4), min(14, bodyH))
	m.taskTable.SetHeight(bodyH - 1)
	m.kbFileTable.SetHeight(bodyH - 1)
	// markdown is width-bound — re-render cached blocks when the transcript resizes
	if m.currentChat != nil && len(m.messages) > 0 {
		m.rebuildBlocks()
	}
}

func (m *model) readyStatus() string {
	return fmt.Sprintf("%s · %d agents · %d sessions  (ctrl+k for actions)", m.instName, len(m.agents), len(m.chats))
}

func (m *model) cleanup() { m.closeWS() }

// cleanErr renders an APIError's clean detail, or a short generic for transport errors.
func cleanErr(err error) string {
	if err == nil {
		return ""
	}
	if ae, ok := err.(*api.APIError); ok {
		return ae.Error()
	}
	return "unreachable"
}
func (m *model) closeWS() {
	if m.wsClient != nil {
		_ = m.wsClient.Close()
		m.wsClient = nil
	}
}

// overlay centers a box over the background view.
func (m *model) overlay(background, content string) string {
	box := m.theme.Border.BorderForeground(m.theme.Accent).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// ctxBg is a short alias for context.Background() used by fire-and-forget action cmds.
func ctxBg() context.Context { return context.Background() }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
