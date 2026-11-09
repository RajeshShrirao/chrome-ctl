package protocol

import "encoding/json"

const JSONRPC = "2.0"

const (
	MethodSystemReady   = "system.ready"
	MethodSystemPing    = "system.ping"
	MethodSessionStatus = "session.status"
	MethodWindowList    = "window.list"
	MethodWindowCreate  = "window.create"
	MethodWindowEnsure  = "window.ensure"
	MethodWindowUse     = "window.use"
	MethodWindowClose   = "window.close"
	MethodTabList       = "tab.list"
	MethodTabOpen       = "tab.open"
	MethodTabSwitch     = "tab.switch"
	MethodPageSnap      = "page.snap"
	MethodPageRead      = "page.read"
	MethodPageWait      = "page.wait"
	MethodPageEval      = "page.eval"
	MethodPageAttr      = "page.attr"
	MethodPageNet       = "page.net"
	MethodPageFormState = "page.formstate"
	MethodActionClick   = "action.click"
	MethodActionType    = "action.type"
	MethodActionPress   = "action.press"
	MethodActionScroll  = "action.scroll"
	MethodActionUpload  = "action.upload"
	MethodMediaShot     = "media.shot"
	MethodMediaRecStart = "media.rec_start"
	MethodMediaRecStop  = "media.rec_stop"
	MethodMediaCapture  = "media.capture"
	MethodAuthSSO       = "auth.sso"
	MethodPageDescribe  = "page.describe"
	MethodPageResolve   = "page.resolve"
	MethodMacroStart    = "macro.start"
	MethodMacroSave     = "macro.save"
	MethodMacroList     = "macro.list"
	MethodMacroRun      = "macro.run"
)

const (
	ErrConnection             = -32000
	ErrElementNotFound        = -32001
	ErrElementNotInteractable = -32002
	ErrTabNotFound            = -32003
	ErrDebuggerDetached       = -32004
	ErrSSOTimeout             = -32005
	ErrUnauthorized           = -32006
	ErrNavigationFailed       = -32007
	ErrMacroStepFailed        = -32008
	ErrDispatchedUnconfirmed  = -32009
	ErrSnapshotStale          = -32010
	ErrFeatureDisabled        = -32011
	ErrMethodNotFound         = -32601
	ErrInvalidRequest         = -32600
	ErrInternal               = -32603
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *string         `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *string         `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type ActiveTab struct {
	TabID int    `json:"tabId"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type ExtensionStatus struct {
	ActiveTab        *ActiveTab `json:"activeTab"`
	TabCount         int        `json:"tabCount"`
	DebuggerAttached bool       `json:"debuggerAttached"`
}

type SessionStatus struct {
	Status             string     `json:"status"`
	ExtensionConnected bool       `json:"extensionConnected"`
	Version            string     `json:"version"`
	UptimeSec          int64      `json:"uptimeSec"`
	ActiveTab          *ActiveTab `json:"activeTab"`
	TabCount           int        `json:"tabCount"`
	DebuggerAttached   bool       `json:"debuggerAttached"`
}

type SystemReadyParams struct {
	Version         string `json:"version"`
	DaemonUptimeSec int64  `json:"daemonUptimeSec"`
	AttachedTabs    []int  `json:"attachedTabs"`
}

type PingParams struct {
	Timestamp int64 `json:"timestamp"`
}

type PongResult struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

type TabListParams struct {
	WindowID *int `json:"windowId,omitempty"`
}

type WindowInfo struct {
	WindowID   int    `json:"windowId"`
	Focused    bool   `json:"focused"`
	State      string `json:"state"`
	TabCount   int    `json:"tabCount"`
	Automation bool   `json:"automation"`
}

type WindowListResult struct {
	Windows []WindowInfo `json:"windows"`
}

// WindowCreateParams is shared by window.create and window.ensure. Focused
// defaults to false so the user's current window keeps keyboard focus.
type WindowCreateParams struct {
	URL     string `json:"url,omitempty"`
	Focused bool   `json:"focused,omitempty"`
}

type WindowResult struct {
	WindowID    int  `json:"windowId"`
	ActiveTabID int  `json:"activeTabId"`
	TabCount    int  `json:"tabCount"`
	Focused     bool `json:"focused"`
	Created     bool `json:"created"`
}

// WindowUseParams selects the automation window. Focused targets the window the
// user is currently using; no fields clears the selection.
type WindowUseParams struct {
	WindowID *int `json:"windowId,omitempty"`
	Focused  bool `json:"focused,omitempty"`
}

type WindowUseResult struct {
	WindowID    *int `json:"windowId,omitempty"`
	ActiveTabID int  `json:"activeTabId,omitempty"`
	TabCount    int  `json:"tabCount,omitempty"`
	Focused     bool `json:"focused,omitempty"`
	Cleared     bool `json:"cleared,omitempty"`
}

type WindowCloseParams struct {
	WindowID int `json:"windowId"`
}

type WindowCloseResult struct {
	Success  bool `json:"success"`
	WindowID int  `json:"windowId"`
}

type TabInfo struct {
	TabID  int    `json:"tabId"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

type TabListResult struct {
	Tabs []TabInfo `json:"tabs"`
}

type TabOpenParams struct {
	URL      string `json:"url"`
	NewTab   bool   `json:"newTab,omitempty"`
	WindowID *int   `json:"windowId,omitempty"`
}

type TabOpenResult struct {
	TabID  int    `json:"tabId"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Loaded bool   `json:"loaded"`
}

type TabSwitchParams struct {
	TabID int `json:"tabId"`
}

type TabSwitchResult struct {
	Success     bool `json:"success"`
	ActiveTabID int  `json:"activeTabId"`
}

type PageSnapParams struct {
	TabID       *int `json:"tabId,omitempty"`
	All         bool `json:"all,omitempty"`
	FormsOnly   bool `json:"formsOnly,omitempty"`
	Deep        bool `json:"deep,omitempty"`
	MaxElements int  `json:"maxElements,omitempty"`
	Frames      bool `json:"frames,omitempty"`
}

type ViewportInfo struct {
	ScrollY        float64 `json:"scrollY"`
	ViewportHeight float64 `json:"viewportHeight"`
	TotalHeight    float64 `json:"totalHeight"`
	Percent        float64 `json:"percent"`
}

type SnapElement struct {
	ID    int    `json:"id"`
	Tag   string `json:"tag"`
	Type  string `json:"type"`
	Label string `json:"label"`
	Value string `json:"value"`
	State string `json:"state"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	Frame string `json:"frame,omitempty"`
}

type SnapResult struct {
	TabID         int           `json:"tabId"`
	Title         string        `json:"title"`
	URL           string        `json:"url"`
	DocID         string        `json:"docId"`
	SnapshotID    int           `json:"snapshotId"`
	Viewport      ViewportInfo  `json:"viewport"`
	Elements      []SnapElement `json:"elements"`
	FormattedText string        `json:"formattedText"`
	Omitted       int           `json:"omitted,omitempty"`
	FramesSummary string        `json:"framesSummary,omitempty"`
}

type PageReadParams struct {
	TabID     *int   `json:"tabId,omitempty"`
	ID        *int   `json:"id,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Mode      string `json:"mode,omitempty"`
	MaxTables int    `json:"maxTables,omitempty"`
	MaxLinks  int    `json:"maxLinks,omitempty"`
	Forms     bool   `json:"forms,omitempty"`
}

// ReadTable is one table extracted in tables mode. Rows never include the
// header row; Headers is promoted from thead/th or from the first row.
type ReadTable struct {
	Index   int        `json:"index"`
	Caption string     `json:"caption,omitempty"`
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

type ReadLink struct {
	Text string `json:"text"`
	Href string `json:"href"`
}

type PageReadResult struct {
	Markdown  string      `json:"markdown,omitempty"`
	Tables    []ReadTable `json:"tables,omitempty"`
	Links     []ReadLink  `json:"links,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
}

type ActionClickParams struct {
	TabID       *int               `json:"tabId,omitempty"`
	ID          int                `json:"id,omitempty"`
	Selector    string             `json:"selector,omitempty"`
	Text        string             `json:"text,omitempty"`
	At          *ActionCoordinates `json:"at,omitempty"`
	DoubleClick bool               `json:"doubleClick,omitempty"`
	Snap        bool               `json:"snap,omitempty"`
	TimeoutMs   int                `json:"timeoutMs,omitempty"`
}

type ActionTarget struct {
	ID    int    `json:"id"`
	Tag   string `json:"tag"`
	Label string `json:"label"`
}

type ActionCoordinates struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type ActionClickResult struct {
	Success     bool               `json:"success"`
	Action      string             `json:"action"`
	Target      *ActionTarget      `json:"target,omitempty"`
	Coordinates *ActionCoordinates `json:"coordinates,omitempty"`
	LandedOn    string             `json:"landedOn,omitempty"`
	URLChanged  bool               `json:"urlChanged,omitempty"`
	Consequence string             `json:"consequence,omitempty"`
	DurationMs  int                `json:"durationMs"`
	DispatchMs  int                `json:"dispatchMs,omitempty"`
	SettleMs    int                `json:"settleMs,omitempty"`
	Snapshot    *SnapResult        `json:"snapshot,omitempty"`
}

type ActionTypeParams struct {
	TabID    *int     `json:"tabId,omitempty"`
	ID       int      `json:"id"`
	Text     string   `json:"text"`
	Enter    bool     `json:"enter,omitempty"`
	TypoRate *float64 `json:"typoRate,omitempty"`
	Snap     bool     `json:"snap,omitempty"`
}

type ActionTypeResult struct {
	Success       bool        `json:"success"`
	CharsTyped    int         `json:"charsTyped"`
	DurationMs    int         `json:"durationMs"`
	TyposInjected int         `json:"typosInjected"`
	StateDelta    string      `json:"stateDelta,omitempty"`
	Verified      bool        `json:"verified"`
	Verification  string      `json:"verification,omitempty"`
	ReadBack      string      `json:"readBack,omitempty"`
	Snapshot      *SnapResult `json:"snapshot,omitempty"`
}

type ActionPressParams struct {
	TabID     *int     `json:"tabId,omitempty"`
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type ActionPressResult struct {
	Success bool   `json:"success"`
	Key     string `json:"key"`
}

type ActionScrollParams struct {
	TabID      *int    `json:"tabId,omitempty"`
	Direction  string  `json:"direction,omitempty"`
	Amount     float64 `json:"amount,omitempty"`
	ToSelector string  `json:"toSelector,omitempty"`
}

type ActionScrollResult struct {
	ScrolledPixels  float64 `json:"scrolledPixels"`
	NewScrollY      float64 `json:"newScrollY"`
	ViewportPercent float64 `json:"viewportPercent"`
	Target          string  `json:"target,omitempty"`
	Frame           string  `json:"frame,omitempty"`
}

type ActionUploadParams struct {
	TabID    *int   `json:"tabId,omitempty"`
	ID       int    `json:"id"`
	FilePath string `json:"filePath"`
}

type ActionUploadResult struct {
	Success  bool   `json:"success"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
}

type PageWaitParams struct {
	TabID      *int   `json:"tabId,omitempty"`
	Selector   string `json:"selector,omitempty"`
	Idle       bool   `json:"idle,omitempty"`
	Text       string `json:"text,omitempty"`
	IgnoreCase *bool  `json:"ignoreCase,omitempty"`
	Network    bool   `json:"network,omitempty"`
	QuietMs    int    `json:"quietMs,omitempty"`
	TimeoutMs  int    `json:"timeoutMs,omitempty"`
	LongPollMs int    `json:"longPollMs,omitempty"`
}

type PageWaitResult struct {
	WaitedMs int    `json:"waitedMs"`
	Matched  bool   `json:"matched"`
	Reason   string `json:"reason,omitempty"`
	Inflight int    `json:"inflight,omitempty"`
	Requests int    `json:"requests,omitempty"`
}

type MediaShotParams struct {
	Som        bool   `json:"som,omitempty"`
	Gray       bool   `json:"gray,omitempty"`
	Quality    int    `json:"quality,omitempty"`
	MaxWidth   int    `json:"maxWidth,omitempty"`
	OutputPath string `json:"outputPath,omitempty"`
	Exact      bool   `json:"exact,omitempty"`
	Full       bool   `json:"full,omitempty"`
}

type MediaShotResult struct {
	Path             string  `json:"path"`
	FileSizeBytes    int64   `json:"fileSizeBytes"`
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	SomBadgeCount    int     `json:"somBadgeCount"`
	Gray             bool    `json:"gray"`
	CapturePath      string  `json:"capturePath,omitempty"`
	ViewportWidth    int     `json:"viewportWidth,omitempty"`
	ViewportHeight   int     `json:"viewportHeight,omitempty"`
	Scale            float64 `json:"scale,omitempty"`
	CaptureTime      string  `json:"captureTime,omitempty"`
	Hash             string  `json:"hash,omitempty"`
	IdenticalWarning bool    `json:"identicalWarning,omitempty"`
}

type MediaRecStartParams struct {
	FPS int `json:"fps,omitempty"`
}

type MediaRecStartResult struct {
	Started bool `json:"started"`
	FPS     int  `json:"fps"`
	TabID   int  `json:"tabId"`
}

type MediaRecStopParams struct {
	Grid       bool   `json:"grid,omitempty"`
	OutputPath string `json:"outputPath,omitempty"`
}

type MediaRecStopResult struct {
	Path           string `json:"path"`
	Format         string `json:"format"`
	FramesCaptured int    `json:"framesCaptured"`
	GridDimensions string `json:"gridDimensions,omitempty"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	FileSizeBytes  int64  `json:"fileSizeBytes"`
	DurationMs     int64  `json:"durationMs"`
}

// MediaCapture is the extension-facing half of the media pipeline: one raw
// PNG of the visible viewport plus the element boxes needed for SoM badges.
type MediaCaptureParams struct {
	TabID           *int `json:"tabId,omitempty"`
	IncludeElements bool `json:"includeElements,omitempty"`
	Full            bool `json:"full,omitempty"`
}

type MediaCaptureResult struct {
	TabID            int           `json:"tabId"`
	PNGBase64        string        `json:"pngBase64"`
	ViewportWidth    int           `json:"viewportWidth"`
	ViewportHeight   int           `json:"viewportHeight"`
	DevicePixelRatio float64       `json:"devicePixelRatio,omitempty"`
	CapturePath      string        `json:"capturePath,omitempty"`
	Elements         []SnapElement `json:"elements,omitempty"`
}

// EvalParams runs an expression in the page's main world through CDP.
type EvalParams struct {
	TabID        *int   `json:"tabId,omitempty"`
	Expression   string `json:"expression"`
	AwaitPromise bool   `json:"awaitPromise,omitempty"`
	TimeoutMs    int    `json:"timeoutMs,omitempty"`
}

type EvalResult struct {
	JSON      string `json:"json"`
	Type      string `json:"type,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// PageAttrParams resolves one element and returns its attributes. Exactly one
// of ID or Selector is expected; Selector accepts a bare CSS selector.
type PageAttrParams struct {
	TabID    *int   `json:"tabId,omitempty"`
	ID       *int   `json:"id,omitempty"`
	Selector string `json:"selector,omitempty"`
	Frame    string `json:"frame,omitempty"`
}

type FormControlState struct {
	Frame    string `json:"frame"`
	Tag      string `json:"tag"`
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	ID       string `json:"id,omitempty"`
	Label    string `json:"label,omitempty"`
	Value    string `json:"value,omitempty"`
	Checked  bool   `json:"checked,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type PageFormStateParams struct {
	TabID *int `json:"tabId,omitempty"`
}

type PageFormStateResult struct {
	Controls []FormControlState `json:"controls"`
}

type ElementRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type PageAttrResult struct {
	DocID      string            `json:"docId"`
	SnapshotID int               `json:"snapshotId"`
	Tag        string            `json:"tag"`
	Role       string            `json:"role,omitempty"`
	Type       string            `json:"type,omitempty"`
	Label      string            `json:"label,omitempty"`
	Value      string            `json:"value,omitempty"`
	State      string            `json:"state,omitempty"`
	Href       string            `json:"href,omitempty"`
	Src        string            `json:"src,omitempty"`
	Name       string            `json:"name,omitempty"`
	ElementID  string            `json:"elementId,omitempty"`
	Rect       ElementRect       `json:"rect"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Matches    int               `json:"matches"`
}

// NetEntry is one request observed through the CDP Network domain.
type NetEntry struct {
	RequestID  string `json:"requestId,omitempty"`
	URL        string `json:"url"`
	Method     string `json:"method,omitempty"`
	Type       string `json:"type,omitempty"`
	Status     int    `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  int64  `json:"startedAt,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

type PageNetParams struct {
	TabID      *int  `json:"tabId,omitempty"`
	ErrorsOnly bool  `json:"errorsOnly,omitempty"`
	Limit      int   `json:"limit,omitempty"`
	SinceMs    int64 `json:"sinceMs,omitempty"`
}

type PageNetResult struct {
	Entries   []NetEntry `json:"entries"`
	EnabledAt int64      `json:"enabledAt,omitempty"`
	Note      string     `json:"note,omitempty"`
}

// MacroTarget is the multi-tier resilient selector captured for an element.
// Tiers are attempted in this order: TestID, Role+AriaLabel, Text, XPath, CSS.
type MacroTarget struct {
	TestID      string `json:"testId,omitempty"`
	AriaLabel   string `json:"ariaLabel,omitempty"`
	Role        string `json:"role,omitempty"`
	Text        string `json:"text,omitempty"`
	Tag         string `json:"tag,omitempty"`
	Type        string `json:"type,omitempty"`
	XPath       string `json:"xpath,omitempty"`
	CSSSelector string `json:"cssSelector,omitempty"`
	FramePath   string `json:"framePath,omitempty"`
}

type PageDescribeParams struct {
	TabID *int `json:"tabId,omitempty"`
	ID    int  `json:"id"`
}

type PageDescribeResult struct {
	Target MacroTarget `json:"target"`
}

type PageResolveParams struct {
	TabID     *int        `json:"tabId,omitempty"`
	Target    MacroTarget `json:"target"`
	TimeoutMs int         `json:"timeoutMs,omitempty"`
}

type PageResolveResult struct {
	ID          int    `json:"id"`
	Tag         string `json:"tag"`
	Type        string `json:"type"`
	Label       string `json:"label"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	W           int    `json:"w"`
	H           int    `json:"h"`
	MatchedTier string `json:"matchedTier"`
}

type AuthSSOParams struct {
	Account   string `json:"account,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
}

type AuthSSOResult struct {
	Success           bool   `json:"success"`
	SelectedAccount   string `json:"selectedAccount"`
	RedirectCompleted bool   `json:"redirectCompleted"`
	ParentTabURL      string `json:"parentTabUrl"`
}

// MacroStep is one recorded action. Field names follow docs/MACRO_SCHEMA.md.
type MacroStep struct {
	StepID      int               `json:"step_id"`
	Description string            `json:"description,omitempty"`
	Action      string            `json:"action"`
	Target      *MacroTarget      `json:"target,omitempty"`
	URL         string            `json:"url,omitempty"`
	Value       string            `json:"value,omitempty"`
	Options     *MacroStepOptions `json:"options,omitempty"`
	OnFailure   string            `json:"on_failure,omitempty"`
	Condition   string            `json:"condition,omitempty"`
}

type MacroStepOptions struct {
	Enter     bool     `json:"enter,omitempty"`
	Double    bool     `json:"double,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Amount    float64  `json:"amount,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type MacroParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Default     string `json:"default,omitempty"`
}

// MacroFile is the persisted playbook written to ~/.chrome-ctl/macros.
type MacroFile struct {
	Schema       string           `json:"$schema,omitempty"`
	MacroVersion string           `json:"macro_version"`
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	CreatedAt    string           `json:"created_at,omitempty"`
	Parameters   []MacroParameter `json:"parameters"`
	Steps        []MacroStep      `json:"steps"`
}

type MacroStartParams struct {
	Name string `json:"name"`
}

type MacroStartResult struct {
	Name    string `json:"name"`
	Started bool   `json:"started"`
	Steps   int    `json:"steps"`
}

type MacroSaveParams struct {
	Params      []string `json:"params,omitempty"`
	Description string   `json:"description,omitempty"`
}

type MacroSaveResult struct {
	Path       string   `json:"path"`
	Name       string   `json:"name"`
	Steps      int      `json:"steps"`
	Parameters []string `json:"parameters"`
}

type MacroSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Parameters  []string `json:"parameters"`
	Steps       int      `json:"steps"`
	ModifiedAt  string   `json:"modifiedAt"`
}

type MacroListResult struct {
	Macros []MacroSummary `json:"macros"`
}

type MacroRunParams struct {
	Name string            `json:"name"`
	Vars map[string]string `json:"vars,omitempty"`
}

type MacroRunResult struct {
	Macro      string `json:"macro"`
	StepsRun   int    `json:"stepsRun"`
	DurationMs int64  `json:"durationMs"`
	Completed  bool   `json:"completed"`
}

// MacroStepFailure is attached as RPC error data when replay stops.
type MacroStepFailure struct {
	Status          string `json:"status"`
	Macro           string `json:"macro"`
	FailedStepID    int    `json:"failed_step_id"`
	Reason          string `json:"reason"`
	CurrentSnapshot string `json:"current_snapshot"`
	RemainingSteps  int    `json:"remaining_steps"`
}

func Success(id *string, result any) Response {
	raw, err := json.Marshal(result)
	if err != nil {
		return Failure(id, ErrInternal, "failed to encode result")
	}
	return Response{JSONRPC: JSONRPC, ID: id, Result: raw}
}

func Failure(id *string, code int, message string) Response {
	return Response{
		JSONRPC: JSONRPC,
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}
