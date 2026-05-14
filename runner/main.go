package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var (
	errProjectNotFound      = errors.New("project not found")
	errClusterNotFound      = errors.New("cluster not found")
	errUnknownAgentProfile  = errors.New("unknown agent profile")
	errActiveIssueSession   = errors.New("issue already has an active session")
	errSessionActive        = errors.New("session is still active")
	errUnsafeSessionWorkdir = errors.New("session workdir is outside the mspace workdir root")
	errUnauthorized         = errors.New("unauthorized")
	errForbidden            = errors.New("forbidden")
	errInvalidRuntimeMode   = errors.New("runtimeMode must be local or team")
)

const defaultImportedClusterImageRegistryPrefix = "crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter"
const defaultHumanActorName = "mlhiter"
const systemActorName = "mspace"
const agentTokenPrefix = "mspace-agent-"
const maxIssueAttachmentBytes = 10 * 1024 * 1024
const runnerProtocolVersion = 1
const projectRunbookArtifactName = "project-runbook.md"
const projectRunbookMaxBytes = 128 * 1024
const projectRunbookPromptLimit = 24 * 1024

var checklistItemPattern = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+\[([ xX])\]\s+(.+?)\s*$`)
var pullRequestURLPattern = regexp.MustCompile(`github\.com/[^/]+/[^/]+/pull/(\d+)`)

var allowedCommentReactions = map[string]bool{
	"thumbs_up":   true,
	"thumbs_down": true,
	"laugh":       true,
	"hooray":      true,
	"confused":    true,
	"heart":       true,
	"rocket":      true,
	"eyes":        true,
}

var commentReactionOrder = []string{"thumbs_up", "thumbs_down", "laugh", "hooray", "confused", "heart", "rocket", "eyes"}

func runnerHealthPayload() map[string]any {
	return map[string]any{
		"ok":             true,
		"version":        "0.1.0",
		"runnerProtocol": runnerProtocolVersion,
		"capabilities": map[string]bool{
			"issueAttachments": true,
			"issueHandoffs":    true,
		},
	}
}

type cluster struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	KubeContext         string `json:"kubeContext"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	ExposureMode        string `json:"exposureMode"`
	NodeHost            string `json:"nodeHost"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	Status              string `json:"status"`
	LastCheckedAt       string `json:"lastCheckedAt"`
	ProjectCount        int    `json:"projectCount"`
	EnvironmentCount    int    `json:"environmentCount"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
}

type project struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	RepoPath               string `json:"repoPath"`
	SourceType             string `json:"sourceType"`
	RemoteURL              string `json:"remoteUrl"`
	GitProvider            string `json:"gitProvider"`
	GitOwner               string `json:"gitOwner"`
	GitRepo                string `json:"gitRepo"`
	DefaultBranch          string `json:"defaultBranch"`
	DeployCommand          string `json:"deployCommand"`
	ValidationCommand      string `json:"validationCommand"`
	KubeContext            string `json:"kubeContext"`
	KubeconfigPath         string `json:"kubeconfigPath"`
	Namespace              string `json:"namespace"`
	ImageRegistryPrefix    string `json:"imageRegistryPrefix"`
	PreviewDomain          string `json:"previewDomain"`
	IngressClass           string `json:"ingressClass"`
	NodeHost               string `json:"nodeHost"`
	DefaultClusterID       string `json:"defaultClusterId"`
	RunbookStatus          string `json:"runbookStatus"`
	RunbookUpdatedAt       string `json:"runbookUpdatedAt"`
	RunbookSource          string `json:"runbookSource"`
	RunbookSourceSessionID string `json:"runbookSourceSessionId"`
	IssueCount             int    `json:"issueCount"`
	SessionCount           int    `json:"sessionCount"`
	LatestIssueUpdatedAt   string `json:"latestIssueUpdatedAt"`
	CreatedAt              string `json:"createdAt"`
	UpdatedAt              string `json:"updatedAt"`
}

type projectRunbook struct {
	ProjectID       string `json:"projectId"`
	Content         string `json:"content"`
	Status          string `json:"status"`
	Source          string `json:"source"`
	SourceSessionID string `json:"sourceSessionId"`
	ContentHash     string `json:"contentHash"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type issue struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	ParentIssueID  string `json:"parentIssueId"`
	SortOrder      int    `json:"sortOrder"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Status         string `json:"status"`
	CloseReason    string `json:"closeReason"`
	TriageStatus   string `json:"triageStatus"`
	Assignee       string `json:"assignee"`
	AssigneeType   string `json:"assigneeType"`
	CreatorName    string `json:"creatorName"`
	CreatorAvatar  string `json:"creatorAvatarUrl"`
	EnvironmentURL string `json:"environmentUrl"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type issueActor struct {
	Kind      string
	Name      string
	AvatarURL string
	UserID    string
	SessionID string
	IssueID   string
}

type inboxItem struct {
	ID           string `json:"id"`
	IssueID      string `json:"issueId"`
	ProjectID    string `json:"projectId"`
	ProjectName  string `json:"projectName"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Assignee     string `json:"assignee"`
	AssigneeType string `json:"assigneeType"`
	Unread       bool   `json:"unread"`
	UpdatedAt    string `json:"updatedAt"`
}

type issueListItem struct {
	ID                       string       `json:"id"`
	ProjectID                string       `json:"projectId"`
	ProjectName              string       `json:"projectName"`
	ParentIssueID            string       `json:"parentIssueId"`
	SortOrder                int          `json:"sortOrder"`
	Title                    string       `json:"title"`
	Body                     string       `json:"body"`
	Status                   string       `json:"status"`
	CloseReason              string       `json:"closeReason"`
	TriageStatus             string       `json:"triageStatus"`
	Assignee                 string       `json:"assignee"`
	AssigneeType             string       `json:"assigneeType"`
	Labels                   []issueLabel `json:"labels"`
	Unread                   bool         `json:"unread"`
	SessionCount             int          `json:"sessionCount"`
	ChildIssueCount          int          `json:"childIssueCount"`
	CompletedChildIssueCount int          `json:"completedChildIssueCount"`
	UpdatedAt                string       `json:"updatedAt"`
	CreatedAt                string       `json:"createdAt"`
}

type comment struct {
	ID           string                   `json:"id"`
	IssueID      string                   `json:"issueId"`
	AuthorType   string                   `json:"authorType"`
	AuthorUserID string                   `json:"authorUserId"`
	AuthorName   string                   `json:"authorName"`
	AuthorAvatar string                   `json:"authorAvatarUrl"`
	Body         string                   `json:"body"`
	CreatedAt    string                   `json:"createdAt"`
	UpdatedAt    string                   `json:"updatedAt"`
	EditedAt     string                   `json:"editedAt"`
	Reactions    []commentReactionSummary `json:"reactions"`
}

type commentReactionSummary struct {
	Reaction    string `json:"reaction"`
	Count       int    `json:"count"`
	ReactedByMe bool   `json:"reactedByMe"`
}

type issueAttachment struct {
	ID             string `json:"id"`
	IssueID        string `json:"issueId"`
	CommentID      string `json:"commentId"`
	Filename       string `json:"filename"`
	ContentType    string `json:"contentType"`
	SizeBytes      int64  `json:"sizeBytes"`
	StorageBackend string `json:"storageBackend"`
	URL            string `json:"url"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type issueLabel struct {
	ID        string `json:"id"`
	IssueID   string `json:"issueId"`
	LabelID   string `json:"labelId"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Color     string `json:"color"`
	SortOrder int    `json:"sortOrder"`
	CreatedAt string `json:"createdAt"`
}

type issueLabelDefinition struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Color     string `json:"color"`
	SortOrder int    `json:"sortOrder"`
	BuiltIn   bool   `json:"builtIn"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type agentProfile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Mention      string `json:"mention"`
	Provider     string `json:"provider"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Enabled      bool   `json:"enabled"`
	BuiltIn      bool   `json:"builtIn"`
	SortOrder    int    `json:"sortOrder"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type agentSession struct {
	ID               string `json:"id"`
	IssueID          string `json:"issueId"`
	Provider         string `json:"provider"`
	AgentProfile     string `json:"agentProfile"`
	RuntimeMode      string `json:"runtimeMode"`
	RuntimeTaskID    string `json:"runtimeTaskId"`
	Command          string `json:"command"`
	Status           string `json:"status"`
	Branch           string `json:"branch"`
	Workdir          string `json:"workdir"`
	CodexThreadID    string `json:"codexThreadId"`
	CodexTurnID      string `json:"codexTurnId"`
	AgentStatus      string `json:"agentStatus"`
	ArtifactDir      string `json:"artifactDir"`
	SourceSessionID  string `json:"sourceSessionId"`
	SourceCommitSHA  string `json:"sourceCommitSha"`
	TriggerCommentID string `json:"triggerCommentId"`
	AgentToken       string `json:"-"`
	CleanupStatus    string `json:"cleanupStatus"`
	CleanedAt        string `json:"cleanedAt"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type sessionLog struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

type deploymentEvidence struct {
	ID        string `json:"id"`
	IssueID   string `json:"issueId"`
	SessionID string `json:"sessionId"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Summary   string `json:"summary"`
	Details   string `json:"details"`
	CreatedAt string `json:"createdAt"`
}

type sessionFailure struct {
	ID                 string `json:"id"`
	IssueID            string `json:"issueId"`
	SessionID          string `json:"sessionId"`
	Phase              string `json:"phase"`
	Status             string `json:"status"`
	FailedCommand      string `json:"failedCommand"`
	ErrorSummary       string `json:"errorSummary"`
	ErrorExcerpt       string `json:"errorExcerpt"`
	Cluster            string `json:"cluster"`
	Namespace          string `json:"namespace"`
	ResourceKind       string `json:"resourceKind"`
	ResourceName       string `json:"resourceName"`
	EvidenceID         string `json:"evidenceId"`
	ReviewEvidenceID   string `json:"reviewEvidenceId"`
	RetrySessionID     string `json:"retrySessionId"`
	ContinuedSessionID string `json:"continuedSessionId"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

type reviewEvidenceCommand struct {
	Command   string `json:"command"`
	Status    string `json:"status"`
	Category  string `json:"category"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"createdAt"`
}

type reviewEvidenceCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type reviewEvidenceResult struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Details string `json:"details"`
}

type sessionReviewEvidence struct {
	ID               string                  `json:"id"`
	IssueID          string                  `json:"issueId"`
	SessionID        string                  `json:"sessionId"`
	SourceSessionID  string                  `json:"sourceSessionId"`
	SourceCommitSHA  string                  `json:"sourceCommitSha"`
	Branch           string                  `json:"branch"`
	AgentSummary     string                  `json:"agentSummary"`
	CommandsRun      []reviewEvidenceCommand `json:"commandsRun"`
	Tests            []reviewEvidenceCheck   `json:"tests"`
	BuildResult      reviewEvidenceResult    `json:"buildResult"`
	DeploymentResult reviewEvidenceResult    `json:"deploymentResult"`
	Risks            []string                `json:"risks"`
	FollowUps        []string                `json:"followUps"`
	PreviewURL       string                  `json:"previewUrl"`
	Cluster          string                  `json:"cluster"`
	Namespace        string                  `json:"namespace"`
	NamespaceStatus  string                  `json:"namespaceStatus"`
	CleanupStatus    string                  `json:"cleanupStatus"`
	CreatedAt        string                  `json:"createdAt"`
	UpdatedAt        string                  `json:"updatedAt"`
}

type issueTestEnvironment struct {
	IssueID              string `json:"issueId"`
	ClusterID            string `json:"clusterId"`
	Namespace            string `json:"namespace"`
	NamespaceStatus      string `json:"namespaceStatus"`
	CleanupStatus        string `json:"cleanupStatus"`
	PreviewURL           string `json:"previewUrl"`
	ImageRegistryPrefix  string `json:"imageRegistryPrefix"`
	KubeconfigPath       string `json:"kubeconfigPath"`
	KubeContext          string `json:"kubeContext"`
	ExposureMode         string `json:"exposureMode"`
	PreviewDomain        string `json:"previewDomain"`
	IngressClass         string `json:"ingressClass"`
	NodeHost             string `json:"nodeHost"`
	LastDeploySessionID  string `json:"lastDeploySessionId"`
	LastCleanupSessionID string `json:"lastCleanupSessionId"`
	SourceSessionID      string `json:"sourceSessionId"`
	SourceCommitSHA      string `json:"sourceCommitSha"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
}

type issueTestEnvironmentResources struct {
	IssueID         string                         `json:"issueId"`
	ClusterID       string                         `json:"clusterId"`
	ClusterName     string                         `json:"clusterName"`
	Context         string                         `json:"context"`
	Namespace       string                         `json:"namespace"`
	NamespaceStatus string                         `json:"namespaceStatus"`
	CleanupStatus   string                         `json:"cleanupStatus"`
	ExposureMode    string                         `json:"exposureMode"`
	PreviewURL      string                         `json:"previewUrl"`
	NodeHost        string                         `json:"nodeHost"`
	RefreshedAt     string                         `json:"refreshedAt"`
	Pods            []kubernetesPodResource        `json:"pods"`
	Services        []kubernetesServiceResource    `json:"services"`
	Deployments     []kubernetesDeploymentResource `json:"deployments"`
	Ingresses       []kubernetesIngressResource    `json:"ingresses"`
	Events          []kubernetesEventResource      `json:"events"`
	Errors          []kubernetesResourceFetchError `json:"errors"`
}

type kubernetesResourceFetchError struct {
	Section string `json:"section"`
	Message string `json:"message"`
}

type kubernetesPodResource struct {
	Name            string                   `json:"name"`
	Phase           string                   `json:"phase"`
	ReadyContainers int                      `json:"readyContainers"`
	TotalContainers int                      `json:"totalContainers"`
	Restarts        int32                    `json:"restarts"`
	NodeName        string                   `json:"nodeName"`
	PodIP           string                   `json:"podIp"`
	HostIP          string                   `json:"hostIp"`
	CreatedAt       string                   `json:"createdAt"`
	Containers      []kubernetesPodContainer `json:"containers"`
}

type kubernetesPodContainer struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state"`
	Reason       string `json:"reason"`
}

type kubernetesServiceResource struct {
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	ClusterIP  string                  `json:"clusterIp"`
	ExternalIP string                  `json:"externalIp"`
	CreatedAt  string                  `json:"createdAt"`
	Ports      []kubernetesServicePort `json:"ports"`
}

type kubernetesServicePort struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"targetPort"`
	NodePort   int32  `json:"nodePort"`
	URL        string `json:"url"`
}

type kubernetesDeploymentResource struct {
	Name              string                `json:"name"`
	Replicas          int32                 `json:"replicas"`
	ReadyReplicas     int32                 `json:"readyReplicas"`
	UpdatedReplicas   int32                 `json:"updatedReplicas"`
	AvailableReplicas int32                 `json:"availableReplicas"`
	CreatedAt         string                `json:"createdAt"`
	Conditions        []kubernetesCondition `json:"conditions"`
}

type kubernetesCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type kubernetesIngressResource struct {
	Name      string   `json:"name"`
	ClassName string   `json:"className"`
	Hosts     []string `json:"hosts"`
	Addresses []string `json:"addresses"`
	CreatedAt string   `json:"createdAt"`
}

type kubernetesEventResource struct {
	Type         string `json:"type"`
	Reason       string `json:"reason"`
	Message      string `json:"message"`
	InvolvedKind string `json:"involvedKind"`
	InvolvedName string `json:"involvedName"`
	Count        int32  `json:"count"`
	FirstSeen    string `json:"firstSeen"`
	LastSeen     string `json:"lastSeen"`
	CreatedAt    string `json:"createdAt"`
}

type issueChangeNode struct {
	ID             string            `json:"id"`
	IssueID        string            `json:"issueId"`
	SessionID      string            `json:"sessionId"`
	CommitSHA      string            `json:"commitSha"`
	ShortCommitSHA string            `json:"shortCommitSha"`
	Branch         string            `json:"branch"`
	Subject        string            `json:"subject"`
	FilesChanged   int               `json:"filesChanged"`
	Changes        []workspaceChange `json:"changes"`
	DiffPreview    string            `json:"diffPreview"`
	DiffTruncated  bool              `json:"diffTruncated"`
	Error          string            `json:"error"`
	Source         string            `json:"source"`
	RemoteWorkdir  string            `json:"remoteWorkdir"`
	ArtifactDir    string            `json:"artifactDir"`
	CreatedAt      string            `json:"createdAt"`
}

type issueHandoffCommit struct {
	SHA      string `json:"sha"`
	ShortSHA string `json:"shortSha"`
	Subject  string `json:"subject"`
}

type issueHandoff struct {
	ID              string               `json:"id"`
	IssueID         string               `json:"issueId"`
	SourceSessionID string               `json:"sourceSessionId"`
	SourceCommitSHA string               `json:"sourceCommitSha"`
	Branch          string               `json:"branch"`
	HeadCommitSHA   string               `json:"headCommitSha"`
	Commits         []issueHandoffCommit `json:"commits"`
	Kind            string               `json:"kind"`
	PRURL           string               `json:"prUrl"`
	PRNumber        int                  `json:"prNumber"`
	PRState         string               `json:"prState"`
	PRTitle         string               `json:"prTitle"`
	PreviewURL      string               `json:"previewUrl"`
	EvidenceSummary string               `json:"evidenceSummary"`
	CreatedVia      string               `json:"createdVia"`
	LastCheckedAt   string               `json:"lastCheckedAt"`
	Error           string               `json:"error"`
	CreatedAt       string               `json:"createdAt"`
	UpdatedAt       string               `json:"updatedAt"`
}

type issueDetail struct {
	Issue           issue                   `json:"issue"`
	Project         project                 `json:"project"`
	TestEnvironment *issueTestEnvironment   `json:"testEnvironment"`
	ChildIssues     []issueListItem         `json:"childIssues"`
	Labels          []issueLabel            `json:"labels"`
	Comments        []comment               `json:"comments"`
	Sessions        []agentSession          `json:"sessions"`
	Evidence        []deploymentEvidence    `json:"evidence"`
	Failures        []sessionFailure        `json:"failures"`
	ChangeNodes     []issueChangeNode       `json:"changeNodes"`
	ReviewEvidence  []sessionReviewEvidence `json:"reviewEvidence"`
	Handoffs        []issueHandoff          `json:"handoffs"`
}

type sessionDetail struct {
	Session   agentSession         `json:"session"`
	Issue     issue                `json:"issue"`
	Project   project              `json:"project"`
	Logs      []sessionLog         `json:"logs"`
	Evidence  []deploymentEvidence `json:"evidence"`
	Failures  []sessionFailure     `json:"failures"`
	Workspace workspaceSnapshot    `json:"workspace"`
}

type runtimeTask struct {
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	Result            json.RawMessage `json:"result"`
	Error             string          `json:"error"`
	ClaimedByWorkerID string          `json:"claimedByWorkerId"`
	StartedAt         string          `json:"startedAt"`
	FinishedAt        string          `json:"finishedAt"`
}

type runtimeTaskLog struct {
	ID        string `json:"id"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

type runtimeTaskResult struct {
	ThreadID    string `json:"threadId"`
	TurnID      string `json:"turnId"`
	Status      string `json:"status"`
	CompletedAt string `json:"completedAt"`
	DryRun      bool   `json:"dryRun"`
	Workdir     string `json:"workdir"`
	ArtifactDir string `json:"artifactDir"`
	Source      struct {
		CommitSHA      string            `json:"commitSha"`
		ShortCommitSHA string            `json:"shortCommitSha"`
		Branch         string            `json:"branch"`
		Subject        string            `json:"subject"`
		FilesChanged   int               `json:"filesChanged"`
		Changes        []workspaceChange `json:"changes"`
		DiffPreview    string            `json:"diffPreview"`
		DiffTruncated  bool              `json:"diffTruncated"`
	} `json:"source"`
}

type cancelRuntimeTaskInput struct {
	Reason string `json:"reason"`
}

type controlPlaneWorkspaceRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type activeWorkItem struct {
	IssueID         string `json:"issueId"`
	ProjectID       string `json:"projectId"`
	ProjectName     string `json:"projectName"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	Namespace       string `json:"namespace"`
	NamespaceStatus string `json:"namespaceStatus"`
	CleanupStatus   string `json:"cleanupStatus"`
	SessionStatus   string `json:"sessionStatus"`
	UpdatedAt       string `json:"updatedAt"`
}

type workspaceSettings struct {
	AutoCreateDraftPR bool   `json:"autoCreateDraftPr"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

type workspaceSettingsInput struct {
	AutoCreateDraftPR bool `json:"autoCreateDraftPr"`
}

type workspaceSnapshot struct {
	Exists          bool                `json:"exists"`
	IsGitRepository bool                `json:"isGitRepository"`
	HasChanges      bool                `json:"hasChanges"`
	ChangedFiles    int                 `json:"changedFiles"`
	UntrackedFiles  int                 `json:"untrackedFiles"`
	Head            string              `json:"head"`
	ShortHead       string              `json:"shortHead"`
	Branch          string              `json:"branch"`
	StatusLines     []string            `json:"statusLines"`
	Changes         []workspaceChange   `json:"changes"`
	DiffPreview     string              `json:"diffPreview"`
	DiffTruncated   bool                `json:"diffTruncated"`
	Comparison      workspaceComparison `json:"comparison"`
	Error           string              `json:"error"`
}

type workspaceChange struct {
	StatusCode   string `json:"statusCode"`
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
}

type workspaceComparison struct {
	BaseRef        string            `json:"baseRef"`
	MergeBase      string            `json:"mergeBase"`
	MergeBaseShort string            `json:"mergeBaseShort"`
	AheadCount     int               `json:"aheadCount"`
	BehindCount    int               `json:"behindCount"`
	CommitLines    []string          `json:"commitLines"`
	Changes        []workspaceChange `json:"changes"`
	DiffPreview    string            `json:"diffPreview"`
	DiffTruncated  bool              `json:"diffTruncated"`
	Error          string            `json:"error"`
}

type eventBroker struct {
	mu          sync.Mutex
	subscribers map[string]map[chan []byte]struct{}
}

func newEventBroker() *eventBroker {
	return &eventBroker{
		subscribers: map[string]map[chan []byte]struct{}{},
	}
}

func (b *eventBroker) subscribe(sessionID string) (chan []byte, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan []byte, 32)
	if _, ok := b.subscribers[sessionID]; !ok {
		b.subscribers[sessionID] = map[chan []byte]struct{}{}
	}
	b.subscribers[sessionID][ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers[sessionID], ch)
		close(ch)
		if len(b.subscribers[sessionID]) == 0 {
			delete(b.subscribers, sessionID)
		}
	}
}

func (b *eventBroker) publish(sessionID string, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for ch := range b.subscribers[sessionID] {
		select {
		case ch <- data:
		default:
		}
	}
}

type app struct {
	db                      *sql.DB
	logger                  *slog.Logger
	workdir                 string
	repoRoot                string
	broker                  *eventBroker
	mu                      sync.Mutex
	cancellers              map[string]sessionCanceller
	controlPlaneBaseURL     string
	controlPlaneToken       string
	controlPlaneWorkspaceID string
}

type sessionCanceller struct {
	cancel context.CancelFunc
	actor  issueActor
}

type projectInput struct {
	Name                string `json:"name"`
	SourceType          string `json:"sourceType"`
	RepoPath            string `json:"repoPath"`
	RepoURL             string `json:"repoUrl"`
	DefaultBranch       string `json:"defaultBranch"`
	DeployCommand       string `json:"deployCommand"`
	ValidationCommand   string `json:"validationCommand"`
	KubeContext         string `json:"kubeContext"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	Namespace           string `json:"namespace"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	NodeHost            string `json:"nodeHost"`
	DefaultClusterID    string `json:"defaultClusterId"`
}

type projectRunbookInput struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

type clusterInput struct {
	Name                string `json:"name"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	KubeContext         string `json:"kubeContext"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	ExposureMode        string `json:"exposureMode"`
	NodeHost            string `json:"nodeHost"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	Status              string `json:"status"`
}

type kubeconfigImportRequest struct {
	Path  string   `json:"path"`
	Paths []string `json:"paths"`
}

type kubeconfigImportResult struct {
	Imported []cluster              `json:"imported"`
	Skipped  []kubeconfigImportSkip `json:"skipped"`
}

type kubeconfigDiscoveryResult struct {
	Candidates []kubeconfigCandidate  `json:"candidates"`
	Skipped    []kubeconfigImportSkip `json:"skipped"`
}

type kubeconfigCandidate struct {
	Path     string   `json:"path"`
	Contexts []string `json:"contexts"`
}

type kubeconfigImportSkip struct {
	Path    string `json:"path"`
	Context string `json:"context"`
	Reason  string `json:"reason"`
}

type sessionRequest struct {
	Provider         string `json:"provider"`
	AgentProfile     string `json:"agentProfile"`
	RuntimeMode      string `json:"runtimeMode"`
	Command          string `json:"command"`
	Branch           string `json:"branch"`
	SourceSessionID  string `json:"sourceSessionId"`
	SourceCommitSHA  string `json:"sourceCommitSha"`
	TriggerCommentID string `json:"triggerCommentId"`
}

type serverIssueSessionRequest struct {
	WorkspaceID     string          `json:"workspaceId"`
	IssueID         string          `json:"issueId"`
	CommentID       string          `json:"commentId"`
	Provider        string          `json:"provider"`
	AgentProfile    string          `json:"agentProfile"`
	RuntimeMode     string          `json:"runtimeMode"`
	Command         string          `json:"command"`
	Branch          string          `json:"branch"`
	SourceSessionID string          `json:"sourceSessionId"`
	SourceCommitSHA string          `json:"sourceCommitSha"`
	Issue           issue           `json:"issue"`
	Project         project         `json:"project"`
	Comments        []comment       `json:"comments"`
	ChildIssues     []issueListItem `json:"childIssues"`
	Labels          []issueLabel    `json:"labels"`
}

type issueTaskInput struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	Completed bool   `json:"completed"`
}

type issueTaskDraft struct {
	Title  string
	Body   string
	Status string
}

type testDeployRequest struct {
	AgentProfile    string `json:"agentProfile"`
	ClusterID       string `json:"clusterId"`
	ExposureMode    string `json:"exposureMode"`
	PreviewDomain   string `json:"previewDomain"`
	IngressClass    string `json:"ingressClass"`
	NodeHost        string `json:"nodeHost"`
	SourceSessionID string `json:"sourceSessionId"`
	SourceCommitSHA string `json:"sourceCommitSha"`
}

type issueHandoffRequest struct {
	SourceSessionID string `json:"sourceSessionId"`
	SourceCommitSHA string `json:"sourceCommitSha"`
	Branch          string `json:"branch"`
	PRURL           string `json:"prUrl"`
	PRTitle         string `json:"prTitle"`
	EvidenceSummary string `json:"evidenceSummary"`
	Kind            string `json:"kind"`
}

type issuePullRequestRequest struct {
	SourceSessionID string `json:"sourceSessionId"`
	SourceCommitSHA string `json:"sourceCommitSha"`
	Title           string `json:"title"`
	Draft           bool   `json:"draft"`
}

type agentProfileInput struct {
	Name         string `json:"name"`
	Mention      string `json:"mention"`
	Provider     string `json:"provider"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Enabled      *bool  `json:"enabled"`
}

type gitRemoteInfo struct {
	Provider string
	Owner    string
	Repo     string
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	port := os.Getenv("MSPACE_PORT")
	if port == "" {
		port = "7788"
	}

	rootDir := filepath.Join(userHomeDir(), ".mspace")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		logger.Error("failed to create app dir", "error", err)
		os.Exit(1)
	}
	dbPath := filepath.Join(rootDir, "mspace.db")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath))
	if err != nil {
		logger.Error("failed to open sqlite", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		logger.Error("failed to enable foreign keys", "error", err)
		os.Exit(1)
	}

	application := &app{
		db:         db,
		logger:     logger,
		workdir:    filepath.Join(rootDir, "workdirs"),
		repoRoot:   filepath.Join(rootDir, "repos"),
		broker:     newEventBroker(),
		cancellers: map[string]sessionCanceller{},
	}

	if err := os.MkdirAll(application.workdir, 0o755); err != nil {
		logger.Error("failed to create workdir root", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(application.repoRoot, 0o755); err != nil {
		logger.Error("failed to create repo root", "error", err)
		os.Exit(1)
	}

	if err := application.migrate(); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	if err := application.reconcileInterruptedSessionsOnStartup(); err != nil {
		logger.Error("failed to reconcile interrupted sessions", "error", err)
		os.Exit(1)
	}

	router := chi.NewRouter()
	router.Use(jsonMiddleware)
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, runnerHealthPayload())
	})
	router.Post("/api/control-plane/session", application.handleConfigureControlPlaneSession)
	router.Get("/api/workspace/settings", application.handleGetWorkspaceSettings)
	router.Put("/api/workspace/settings", application.handleUpdateWorkspaceSettings)
	router.Get("/api/inbox", application.handleListInbox)
	router.Post("/api/inbox/issues/{issueID}/read", application.handleMarkInboxIssueRead)
	router.Get("/api/inbox/stream", application.handleInboxStream)
	router.Get("/api/active-work", application.handleListActiveWork)
	router.Get("/api/agents", application.handleListAgentProfiles)
	router.Post("/api/agents", application.handleCreateAgentProfile)
	router.Put("/api/agents/{agentID}", application.handleUpdateAgentProfile)
	router.Get("/api/clusters", application.handleListClusters)
	router.Post("/api/clusters", application.handleCreateCluster)
	router.Get("/api/clusters/discover-defaults", application.handleDiscoverDefaultClusters)
	router.Post("/api/clusters/import", application.handleImportClusters)
	router.Post("/api/clusters/import-defaults", application.handleImportDefaultClusters)
	router.Put("/api/clusters/{clusterID}", application.handleUpdateCluster)
	router.Delete("/api/clusters/{clusterID}", application.handleDeleteCluster)
	router.Post("/api/attachments", application.handleUploadAttachment)
	router.Get("/api/attachments/{attachmentID}", application.handleGetAttachment)
	router.Get("/api/projects", application.handleListProjects)
	router.Post("/api/projects", application.handleCreateProject)
	router.Put("/api/projects/{projectID}", application.handleUpdateProject)
	router.Get("/api/projects/{projectID}/runbook", application.handleGetProjectRunbook)
	router.Put("/api/projects/{projectID}/runbook", application.handleUpdateProjectRunbook)
	router.Delete("/api/projects/{projectID}", application.handleDeleteProject)
	router.Get("/api/issue-label-definitions", application.handleListIssueLabelDefinitions)
	router.Get("/api/issues", application.handleListIssues)
	router.Post("/api/issues", application.handleCreateIssue)
	router.Get("/api/issues/{issueID}", application.handleGetIssue)
	router.Put("/api/issues/{issueID}", application.handleUpdateIssue)
	router.Post("/api/issues/{issueID}/tasks", application.handleCreateIssueTask)
	router.Delete("/api/issues/{issueID}/tasks/{taskID}", application.handleDeleteIssueTask)
	router.Put("/api/issues/{issueID}/labels", application.handleUpdateIssueLabels)
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)
	router.Put("/api/issues/{issueID}/comments/{commentID}", application.handleUpdateComment)
	router.Put("/api/issues/{issueID}/comments/{commentID}/reactions/{reaction}", application.handleSetCommentReaction)
	router.Delete("/api/issues/{issueID}/comments/{commentID}/reactions/{reaction}", application.handleDeleteCommentReaction)
	router.Post("/api/issues/{issueID}/assign-agent", application.handleAssignIssueToAgent)
	router.Post("/api/issues/{issueID}/sessions", application.handleCreateSession)
	router.Post("/api/server-issues/{issueID}/team-session", application.handleCreateServerIssueTeamSession)
	router.Post("/api/issues/{issueID}/test-deploy", application.handleStartIssueTestDeploy)
	router.Post("/api/issues/{issueID}/test-environment/cleanup", application.handleRequestIssueTestEnvironmentCleanup)
	router.Post("/api/issues/{issueID}/test-environment/retain", application.handleRetainIssueTestEnvironment)
	router.Get("/api/issues/{issueID}/test-environment/resources", application.handleListIssueTestEnvironmentResources)
	router.Post("/api/issues/{issueID}/test-environment/probe", application.handleProbeIssueTestEnvironment)
	router.Post("/api/issues/{issueID}/handoffs/create-pr", application.handleCreateIssuePullRequest)
	router.Post("/api/issues/{issueID}/handoffs/{handoffID}/refresh", application.handleRefreshIssueHandoff)
	router.Get("/api/sessions/{sessionID}", application.handleGetSession)
	router.Post("/api/sessions/{sessionID}/cancel", application.handleCancelSession)
	router.Post("/api/sessions/{sessionID}/cleanup", application.handleCleanupSessionWorktree)
	router.Get("/api/sessions/{sessionID}/stream", application.handleSessionStream)

	logger.Info("starting mspace local runner", "port", port, "db", dbPath)
	if err := http.ListenAndServe("127.0.0.1:"+port, router); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func (a *app) migrate() error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return err
		}
		if _, err := a.db.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("apply %s: %w", entry.Name(), err)
		}
	}
	if err := a.ensureClusterTables(); err != nil {
		return err
	}
	if err := a.ensureProjectColumns(); err != nil {
		return err
	}
	if err := a.ensureProjectRunbookTables(); err != nil {
		return err
	}
	if err := a.ensureWorkspaceSettingsTables(); err != nil {
		return err
	}
	if err := a.ensureIssueColumns(); err != nil {
		return err
	}
	if err := a.ensureCommentColumns(); err != nil {
		return err
	}
	if err := a.ensureCommentReactionTables(); err != nil {
		return err
	}
	if err := a.ensureIssueAttachmentTables(); err != nil {
		return err
	}
	if err := a.ensureIssueLabelTables(); err != nil {
		return err
	}
	if err := a.ensureIssueTestEnvironmentTables(); err != nil {
		return err
	}
	if err := a.ensureAgentProfileTables(); err != nil {
		return err
	}
	if err := a.ensureSessionColumns(); err != nil {
		return err
	}
	if err := a.ensureIssueChangeNodeTables(); err != nil {
		return err
	}
	if err := a.ensureSessionReviewEvidenceTables(); err != nil {
		return err
	}
	if err := a.ensureSessionFailureTables(); err != nil {
		return err
	}
	if err := a.backfillSessionFailures(); err != nil {
		return err
	}
	if err := a.ensureIssueHandoffTables(); err != nil {
		return err
	}
	if err := a.migrateClosedIssueStatuses(); err != nil {
		return err
	}
	return nil
}

func (a *app) reconcileInterruptedSessionsOnStartup() error {
	rows, err := a.db.Query(`
		SELECT id, issue_id, provider, agent_profile, runtime_mode, runtime_task_id, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, source_session_id, source_commit_sha, trigger_comment_id, agent_token, cleanup_status, cleaned_at, created_at, updated_at
		FROM agent_sessions
		WHERE status IN ('queued', 'running')
		ORDER BY updated_at ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	sessions := make([]agentSession, 0)
	for rows.Next() {
		var session agentSession
		if err := rows.Scan(&session.ID, &session.IssueID, &session.Provider, &session.AgentProfile, &session.RuntimeMode, &session.RuntimeTaskID, &session.Command, &session.Status, &session.Branch, &session.Workdir, &session.CodexThreadID, &session.CodexTurnID, &session.AgentStatus, &session.ArtifactDir, &session.SourceSessionID, &session.SourceCommitSHA, &session.TriggerCommentID, &session.AgentToken, &session.CleanupStatus, &session.CleanedAt, &session.CreatedAt, &session.UpdatedAt); err != nil {
			return err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, session := range sessions {
		a.markSessionInterruptedByRunnerRestart(session)
	}
	return nil
}

func (a *app) markSessionInterruptedByRunnerRestart(session agentSession) {
	reason := "The local runner restarted before this session completed, so the attached Codex app-server process is no longer running. Start a new session to continue from the last recorded log."
	a.updateSessionStatus(session.ID, "failed")
	a.updateSessionAgentStatus(session.ID, "interrupted")
	session.Status = "failed"
	session.AgentStatus = "interrupted"
	a.appendSessionLog(session.ID, "system", reason)
	a.updateIssueStatus(session.IssueID, "blocked")
	if detail, err := a.loadSessionDetail(session.ID); err == nil {
		if a.isIssueTestDeploySession(session) {
			a.reconcileIssueTestEnvironmentForSession(session, detail.Project, "interrupted")
			a.recordSessionReviewEvidence(session, detail.Project, nil, false)
		}
		a.recordSessionFailure(session, detail.Project, errors.New(reason), nil, "agent_interrupted", "open")
	} else {
		a.markIssueTestEnvironmentSessionInterrupted(session)
	}
	a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` was interrupted.\n\n%s", shortID(session.ID), reason))
}

func (a *app) backfillSessionFailures() error {
	rows, err := a.db.Query(`
		SELECT s.id
		FROM agent_sessions s
		LEFT JOIN session_failures f ON f.session_id = s.id
		WHERE s.status IN ('failed', 'cancelled') AND f.id IS NULL
		ORDER BY s.updated_at ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	sessionIDs := []string{}
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return err
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, sessionID := range sessionIDs {
		detail, err := a.loadSessionDetail(sessionID)
		if err != nil {
			return fmt.Errorf("load failed session %s: %w", shortID(sessionID), err)
		}
		message := latestFailureLogMessage(detail.Logs)
		if message == "" {
			if detail.Session.Status == "cancelled" {
				message = "Session was stopped before it completed."
			} else {
				message = "Session did not finish successfully."
			}
		}
		evidence, err := a.latestDeploymentEvidenceForSession(sessionID)
		if err != nil {
			return fmt.Errorf("load failure evidence for session %s: %w", shortID(sessionID), err)
		}
		phase := ""
		status := "open"
		if detail.Session.Status == "cancelled" {
			phase = "agent_interrupted"
			status = "stopped"
		}
		failure := a.buildSessionFailure(detail.Session, detail.Project, errors.New(message), evidence, phase, status)
		if _, err := a.storeSessionFailureRecord(failure); err != nil {
			return fmt.Errorf("backfill session failure %s: %w", shortID(sessionID), err)
		}
	}
	return nil
}

func (a *app) ensureClusterTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS clusters (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kubeconfig_path TEXT NOT NULL,
			kube_context TEXT NOT NULL DEFAULT '',
			image_registry_prefix TEXT NOT NULL DEFAULT '',
			exposure_mode TEXT NOT NULL DEFAULT 'nodeport',
			node_host TEXT NOT NULL DEFAULT '',
			preview_domain TEXT NOT NULL DEFAULT '',
			ingress_class TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'configured',
			last_checked_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create clusters: %w", err)
	}
	if err := a.ensureTableColumns("clusters", map[string]string{
		"kube_context":          "TEXT NOT NULL DEFAULT ''",
		"image_registry_prefix": "TEXT NOT NULL DEFAULT ''",
		"exposure_mode":         "TEXT NOT NULL DEFAULT 'nodeport'",
		"node_host":             "TEXT NOT NULL DEFAULT ''",
		"preview_domain":        "TEXT NOT NULL DEFAULT ''",
		"ingress_class":         "TEXT NOT NULL DEFAULT ''",
		"status":                "TEXT NOT NULL DEFAULT 'configured'",
		"last_checked_at":       "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_clusters_updated_at ON clusters(updated_at)
	`); err != nil {
		return fmt.Errorf("create clusters updated index: %w", err)
	}
	return nil
}

func (a *app) ensureProjectColumns() error {
	rows, err := a.db.Query(`PRAGMA table_info(projects)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	requiredColumns := map[string]string{
		"source_type":           "TEXT NOT NULL DEFAULT 'local'",
		"remote_url":            "TEXT NOT NULL DEFAULT ''",
		"git_provider":          "TEXT NOT NULL DEFAULT ''",
		"git_owner":             "TEXT NOT NULL DEFAULT ''",
		"git_repo":              "TEXT NOT NULL DEFAULT ''",
		"kubeconfig_path":       "TEXT NOT NULL DEFAULT ''",
		"image_registry_prefix": "TEXT NOT NULL DEFAULT ''",
		"preview_domain":        "TEXT NOT NULL DEFAULT ''",
		"ingress_class":         "TEXT NOT NULL DEFAULT ''",
		"node_host":             "TEXT NOT NULL DEFAULT ''",
		"default_cluster_id":    "TEXT NOT NULL DEFAULT ''",
	}
	for name, definition := range requiredColumns {
		if existing[name] {
			continue
		}
		if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE projects ADD COLUMN %s %s", name, definition)); err != nil {
			return fmt.Errorf("add projects.%s: %w", name, err)
		}
	}
	return nil
}

func (a *app) ensureProjectRunbookTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS project_runbooks (
			project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
			content TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'empty',
			source TEXT NOT NULL DEFAULT '',
			source_session_id TEXT NOT NULL DEFAULT '',
			content_hash TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create project_runbooks: %w", err)
	}
	if err := a.ensureTableColumns("project_runbooks", map[string]string{
		"content":           "TEXT NOT NULL DEFAULT ''",
		"status":            "TEXT NOT NULL DEFAULT 'empty'",
		"source":            "TEXT NOT NULL DEFAULT ''",
		"source_session_id": "TEXT NOT NULL DEFAULT ''",
		"content_hash":      "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS project_runbook_revisions (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL DEFAULT '',
			author_type TEXT NOT NULL,
			author_name TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			content_hash TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'learned',
			created_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create project_runbook_revisions: %w", err)
	}
	if err := a.ensureTableColumns("project_runbook_revisions", map[string]string{
		"session_id":   "TEXT NOT NULL DEFAULT ''",
		"author_name":  "TEXT NOT NULL DEFAULT ''",
		"content":      "TEXT NOT NULL DEFAULT ''",
		"content_hash": "TEXT NOT NULL DEFAULT ''",
		"status":       "TEXT NOT NULL DEFAULT 'learned'",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_project_runbook_revisions_project_created ON project_runbook_revisions(project_id, created_at DESC)
	`); err != nil {
		return fmt.Errorf("create project_runbook_revisions project index: %w", err)
	}
	return nil
}

func (a *app) ensureWorkspaceSettingsTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS workspace_settings (
			id TEXT PRIMARY KEY,
			auto_create_draft_pr INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			CHECK(id = 'default')
		)
	`); err != nil {
		return fmt.Errorf("create workspace_settings: %w", err)
	}
	if err := a.ensureTableColumns("workspace_settings", map[string]string{
		"auto_create_draft_pr": "INTEGER NOT NULL DEFAULT 0",
		"created_at":           "TEXT NOT NULL DEFAULT ''",
		"updated_at":           "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	now := nowString()
	if _, err := a.db.Exec(`
		INSERT INTO workspace_settings (id, auto_create_draft_pr, created_at, updated_at)
		VALUES ('default', 0, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, now, now); err != nil {
		return fmt.Errorf("seed workspace_settings: %w", err)
	}
	return nil
}

func (a *app) ensureIssueColumns() error {
	rows, err := a.db.Query(`PRAGMA table_info(issues)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	requiredColumns := map[string]string{
		"parent_issue_id":    "TEXT REFERENCES issues(id) ON DELETE CASCADE",
		"sort_order":         "INTEGER NOT NULL DEFAULT 0",
		"close_reason":       "TEXT NOT NULL DEFAULT ''",
		"assignee_type":      "TEXT NOT NULL DEFAULT 'human'",
		"triage_status":      "TEXT NOT NULL DEFAULT 'none'",
		"creator_name":       "TEXT NOT NULL DEFAULT ''",
		"creator_avatar_url": "TEXT NOT NULL DEFAULT ''",
	}
	hadCloseReason := existing["close_reason"]
	for name, definition := range requiredColumns {
		if existing[name] {
			continue
		}
		if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE issues ADD COLUMN %s %s", name, definition)); err != nil {
			return fmt.Errorf("add issues.%s: %w", name, err)
		}
	}
	if !hadCloseReason {
		if err := a.resetLegacyOutcomeIssueStatuses(); err != nil {
			return err
		}
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_parent_issue_id ON issues(parent_issue_id)
	`); err != nil {
		return fmt.Errorf("create issues parent index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_project_parent_updated ON issues(project_id, parent_issue_id, updated_at)
	`); err != nil {
		return fmt.Errorf("create issues project parent updated index: %w", err)
	}
	if _, err := a.db.Exec(`
		UPDATE issues
		SET creator_name = ?
		WHERE creator_name = ''
	`, defaultHumanActorName); err != nil {
		return fmt.Errorf("backfill issue creators: %w", err)
	}
	if _, err := a.db.Exec(`
		UPDATE issues
		SET assignee = ?
		WHERE assignee_type = 'human' AND (assignee = '' OR assignee = 'me')
	`, defaultHumanActorName); err != nil {
		return fmt.Errorf("backfill human assignees: %w", err)
	}
	return nil
}

func (a *app) resetLegacyOutcomeIssueStatuses() error {
	legacyStatuses := "'test_passed', 'test_failed', 'failed', 'cancelled'"
	if _, err := a.db.Exec(fmt.Sprintf(`
		UPDATE issues
		SET status = 'open', close_reason = '', updated_at = ?
		WHERE status IN (%s)
	`, legacyStatuses), nowString()); err != nil {
		return fmt.Errorf("reset legacy issue outcome statuses: %w", err)
	}
	if !a.tableExists("inbox_items") {
		return nil
	}
	if _, err := a.db.Exec(fmt.Sprintf(`
		UPDATE inbox_items
		SET status = 'open', updated_at = ?
		WHERE status IN (%s)
	`, legacyStatuses), nowString()); err != nil {
		return fmt.Errorf("reset legacy inbox outcome statuses: %w", err)
	}
	return nil
}

func (a *app) tableExists(table string) bool {
	var name string
	err := a.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	return err == nil
}

func (a *app) migrateClosedIssueStatuses() error {
	if _, err := a.db.Exec(`
		UPDATE issues
		SET status = CASE status
				WHEN 'completed' THEN 'closed'
				WHEN 'done' THEN 'closed'
				WHEN 'review' THEN 'needs_review'
				WHEN 'testing' THEN 'needs_review'
				WHEN 'test_in_progress' THEN 'needs_review'
				WHEN 'queued' THEN 'open'
				WHEN 'running' THEN 'open'
				WHEN 'in_progress' THEN 'open'
				ELSE status
			END
			WHERE status IN ('completed', 'done', 'review', 'testing', 'test_in_progress', 'queued', 'running', 'in_progress')
		`); err != nil {
		return fmt.Errorf("migrate completed issue statuses: %w", err)
	}
	if _, err := a.db.Exec(`
		UPDATE inbox_items
		SET status = CASE status
				WHEN 'completed' THEN 'closed'
				WHEN 'done' THEN 'closed'
				WHEN 'review' THEN 'needs_review'
				WHEN 'testing' THEN 'needs_review'
				WHEN 'test_in_progress' THEN 'needs_review'
				WHEN 'queued' THEN 'open'
				WHEN 'running' THEN 'open'
				WHEN 'in_progress' THEN 'open'
				ELSE status
			END
			WHERE status IN ('completed', 'done', 'review', 'testing', 'test_in_progress', 'queued', 'running', 'in_progress')
		`); err != nil {
		return fmt.Errorf("migrate completed inbox statuses: %w", err)
	}
	return nil
}

func (a *app) ensureCommentColumns() error {
	if err := a.ensureTableColumns("comments", map[string]string{
		"author_user_id":    "TEXT NOT NULL DEFAULT ''",
		"author_name":       "TEXT NOT NULL DEFAULT ''",
		"author_avatar_url": "TEXT NOT NULL DEFAULT ''",
		"updated_at":        "TEXT NOT NULL DEFAULT ''",
		"edited_at":         "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		UPDATE comments
		SET author_name = ?
		WHERE author_type = 'human' AND author_name = ''
	`, defaultHumanActorName); err != nil {
		return fmt.Errorf("backfill human comment authors: %w", err)
	}
	if _, err := a.db.Exec(`
		UPDATE comments
		SET author_name = ?
		WHERE author_type = 'system' AND author_name = ''
	`, systemActorName); err != nil {
		return fmt.Errorf("backfill system comment authors: %w", err)
	}
	if _, err := a.db.Exec(`
		UPDATE comments
		SET updated_at = created_at
		WHERE updated_at = ''
	`); err != nil {
		return fmt.Errorf("backfill comment updated_at: %w", err)
	}
	return nil
}

func (a *app) ensureCommentReactionTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS comment_reactions (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			comment_id TEXT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
			reaction TEXT NOT NULL CHECK(reaction IN ('thumbs_up', 'thumbs_down', 'laugh', 'hooray', 'confused', 'heart', 'rocket', 'eyes')),
			user_id TEXT NOT NULL,
			actor_name TEXT NOT NULL DEFAULT '',
			actor_avatar_url TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE(comment_id, user_id, reaction)
		)
	`); err != nil {
		return fmt.Errorf("create comment reactions: %w", err)
	}
	if err := a.ensureTableColumns("comment_reactions", map[string]string{
		"issue_id":         "TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE",
		"comment_id":       "TEXT NOT NULL REFERENCES comments(id) ON DELETE CASCADE",
		"reaction":         "TEXT NOT NULL DEFAULT 'thumbs_up'",
		"user_id":          "TEXT NOT NULL DEFAULT ''",
		"actor_name":       "TEXT NOT NULL DEFAULT ''",
		"actor_avatar_url": "TEXT NOT NULL DEFAULT ''",
		"created_at":       "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_comment_reactions_issue_comment ON comment_reactions(issue_id, comment_id)
	`); err != nil {
		return fmt.Errorf("create comment reactions issue index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_comment_reactions_user ON comment_reactions(user_id)
	`); err != nil {
		return fmt.Errorf("create comment reactions user index: %w", err)
	}
	return nil
}

func (a *app) ensureIssueAttachmentTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_attachments (
			id TEXT PRIMARY KEY,
			issue_id TEXT REFERENCES issues(id) ON DELETE CASCADE,
			comment_id TEXT REFERENCES comments(id) ON DELETE SET NULL,
			filename TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
			storage_backend TEXT NOT NULL DEFAULT 'sqlite_blob',
			storage_key TEXT NOT NULL DEFAULT '',
			content BLOB,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			bound_at TEXT NOT NULL DEFAULT ''
		)
	`); err != nil {
		return fmt.Errorf("create issue attachments: %w", err)
	}
	if err := a.ensureTableColumns("issue_attachments", map[string]string{
		"issue_id":        "TEXT REFERENCES issues(id) ON DELETE CASCADE",
		"comment_id":      "TEXT REFERENCES comments(id) ON DELETE SET NULL",
		"filename":        "TEXT NOT NULL DEFAULT ''",
		"content_type":    "TEXT NOT NULL DEFAULT ''",
		"size_bytes":      "INTEGER NOT NULL DEFAULT 0",
		"storage_backend": "TEXT NOT NULL DEFAULT 'sqlite_blob'",
		"storage_key":     "TEXT NOT NULL DEFAULT ''",
		"content":         "BLOB",
		"updated_at":      "TEXT NOT NULL DEFAULT ''",
		"bound_at":        "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_attachments_issue_id ON issue_attachments(issue_id)
	`); err != nil {
		return fmt.Errorf("create issue attachments issue index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_attachments_comment_id ON issue_attachments(comment_id)
	`); err != nil {
		return fmt.Errorf("create issue attachments comment index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_attachments_created_at ON issue_attachments(created_at)
	`); err != nil {
		return fmt.Errorf("create issue attachments created index: %w", err)
	}
	return nil
}

func (a *app) ensureIssueLabelTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_label_definitions (
			id TEXT PRIMARY KEY,
			key TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			dimension TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			built_in INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create issue_label_definitions: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_label_definitions_dimension ON issue_label_definitions(dimension, sort_order)
	`); err != nil {
		return fmt.Errorf("create issue_label_definitions dimension index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_labels (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			label_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE(issue_id, name),
			UNIQUE(issue_id, label_id)
		)
	`); err != nil {
		return fmt.Errorf("create issue_labels: %w", err)
	}
	if err := a.ensureTableColumns("issue_labels", map[string]string{
		"label_id": "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_labels_issue_id ON issue_labels(issue_id)
	`); err != nil {
		return fmt.Errorf("create issue_labels issue index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_labels_label_id ON issue_labels(label_id)
	`); err != nil {
		return fmt.Errorf("create issue_labels label index: %w", err)
	}
	if err := a.seedIssueLabelDefinitions(); err != nil {
		return err
	}
	return a.backfillIssueLabelIDs()
}

func (a *app) ensureIssueTestEnvironmentTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_test_environments (
			issue_id TEXT PRIMARY KEY REFERENCES issues(id) ON DELETE CASCADE,
			namespace TEXT NOT NULL,
			namespace_status TEXT NOT NULL DEFAULT 'planned',
			cleanup_status TEXT NOT NULL DEFAULT 'retained',
			preview_url TEXT NOT NULL DEFAULT '',
			cluster_id TEXT NOT NULL DEFAULT '',
			image_registry_prefix TEXT NOT NULL DEFAULT '',
			kubeconfig_path TEXT NOT NULL DEFAULT '',
			kube_context TEXT NOT NULL DEFAULT '',
			exposure_mode TEXT NOT NULL DEFAULT 'nodeport',
			preview_domain TEXT NOT NULL DEFAULT '',
			ingress_class TEXT NOT NULL DEFAULT '',
			node_host TEXT NOT NULL DEFAULT '',
			last_deploy_session_id TEXT NOT NULL DEFAULT '',
			last_cleanup_session_id TEXT NOT NULL DEFAULT '',
			source_session_id TEXT NOT NULL DEFAULT '',
			source_commit_sha TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create issue_test_environments: %w", err)
	}
	if err := a.ensureTableColumns("issue_test_environments", map[string]string{
		"namespace_status":        "TEXT NOT NULL DEFAULT 'planned'",
		"cleanup_status":          "TEXT NOT NULL DEFAULT 'retained'",
		"preview_url":             "TEXT NOT NULL DEFAULT ''",
		"cluster_id":              "TEXT NOT NULL DEFAULT ''",
		"image_registry_prefix":   "TEXT NOT NULL DEFAULT ''",
		"kubeconfig_path":         "TEXT NOT NULL DEFAULT ''",
		"kube_context":            "TEXT NOT NULL DEFAULT ''",
		"exposure_mode":           "TEXT NOT NULL DEFAULT 'nodeport'",
		"preview_domain":          "TEXT NOT NULL DEFAULT ''",
		"ingress_class":           "TEXT NOT NULL DEFAULT ''",
		"node_host":               "TEXT NOT NULL DEFAULT ''",
		"last_deploy_session_id":  "TEXT NOT NULL DEFAULT ''",
		"last_cleanup_session_id": "TEXT NOT NULL DEFAULT ''",
		"source_session_id":       "TEXT NOT NULL DEFAULT ''",
		"source_commit_sha":       "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_test_environments_namespace ON issue_test_environments(namespace)
	`); err != nil {
		return fmt.Errorf("create issue_test_environments namespace index: %w", err)
	}
	return nil
}

func (a *app) ensureAgentProfileTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			mention TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL DEFAULT 'codex',
			description TEXT NOT NULL DEFAULT '',
			instructions TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			built_in INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create agent_profiles: %w", err)
	}
	if err := a.ensureTableColumns("agent_profiles", map[string]string{
		"sort_order": "INTEGER NOT NULL DEFAULT 0",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_agent_profiles_enabled ON agent_profiles(enabled)
	`); err != nil {
		return fmt.Errorf("create agent_profiles enabled index: %w", err)
	}
	return a.seedDefaultAgentProfiles()
}

func (a *app) ensureTableColumns(table string, requiredColumns map[string]string) error {
	rows, err := a.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for name, definition := range requiredColumns {
		if existing[name] {
			continue
		}
		if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, name, definition)); err != nil {
			return fmt.Errorf("add %s.%s: %w", table, name, err)
		}
	}
	return nil
}

func (a *app) ensureSessionColumns() error {
	rows, err := a.db.Query(`PRAGMA table_info(agent_sessions)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	requiredColumns := map[string]string{
		"codex_thread_id":    "TEXT NOT NULL DEFAULT ''",
		"codex_turn_id":      "TEXT NOT NULL DEFAULT ''",
		"agent_status":       "TEXT NOT NULL DEFAULT ''",
		"artifact_dir":       "TEXT NOT NULL DEFAULT ''",
		"agent_profile":      "TEXT NOT NULL DEFAULT ''",
		"runtime_task_id":    "TEXT NOT NULL DEFAULT ''",
		"source_session_id":  "TEXT NOT NULL DEFAULT ''",
		"source_commit_sha":  "TEXT NOT NULL DEFAULT ''",
		"trigger_comment_id": "TEXT NOT NULL DEFAULT ''",
		"agent_token":        "TEXT NOT NULL DEFAULT ''",
		"cleanup_status":     "TEXT NOT NULL DEFAULT 'retained'",
		"cleaned_at":         "TEXT NOT NULL DEFAULT ''",
	}
	for name, definition := range requiredColumns {
		if existing[name] {
			continue
		}
		if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE agent_sessions ADD COLUMN %s %s", name, definition)); err != nil {
			return fmt.Errorf("add agent_sessions.%s: %w", name, err)
		}
	}
	return nil
}

func (a *app) ensureIssueChangeNodeTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_change_nodes (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
			commit_sha TEXT NOT NULL,
			branch TEXT NOT NULL DEFAULT '',
			subject TEXT NOT NULL DEFAULT '',
			files_changed INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			UNIQUE(session_id),
			UNIQUE(issue_id, commit_sha)
		)
	`); err != nil {
		return fmt.Errorf("create issue_change_nodes: %w", err)
	}
	if err := a.ensureTableColumns("issue_change_nodes", map[string]string{
		"branch":         "TEXT NOT NULL DEFAULT ''",
		"subject":        "TEXT NOT NULL DEFAULT ''",
		"files_changed":  "INTEGER NOT NULL DEFAULT 0",
		"changes_json":   "TEXT NOT NULL DEFAULT '[]'",
		"diff_preview":   "TEXT NOT NULL DEFAULT ''",
		"diff_truncated": "INTEGER NOT NULL DEFAULT 0",
		"source":         "TEXT NOT NULL DEFAULT 'local'",
		"remote_workdir": "TEXT NOT NULL DEFAULT ''",
		"artifact_dir":   "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_change_nodes_issue_created ON issue_change_nodes(issue_id, created_at DESC)
	`); err != nil {
		return fmt.Errorf("create issue_change_nodes issue index: %w", err)
	}
	return nil
}

func (a *app) ensureSessionReviewEvidenceTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS session_review_evidence (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
			source_session_id TEXT NOT NULL DEFAULT '',
			source_commit_sha TEXT NOT NULL DEFAULT '',
			branch TEXT NOT NULL DEFAULT '',
			agent_summary TEXT NOT NULL DEFAULT '',
			commands_json TEXT NOT NULL DEFAULT '[]',
			tests_json TEXT NOT NULL DEFAULT '[]',
			build_result_json TEXT NOT NULL DEFAULT '{}',
			deployment_result_json TEXT NOT NULL DEFAULT '{}',
			risks_json TEXT NOT NULL DEFAULT '[]',
			follow_ups_json TEXT NOT NULL DEFAULT '[]',
			preview_url TEXT NOT NULL DEFAULT '',
			cluster TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
			namespace_status TEXT NOT NULL DEFAULT '',
			cleanup_status TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(session_id)
		)
	`); err != nil {
		return fmt.Errorf("create session_review_evidence: %w", err)
	}
	if err := a.ensureTableColumns("session_review_evidence", map[string]string{
		"source_session_id":      "TEXT NOT NULL DEFAULT ''",
		"source_commit_sha":      "TEXT NOT NULL DEFAULT ''",
		"branch":                 "TEXT NOT NULL DEFAULT ''",
		"agent_summary":          "TEXT NOT NULL DEFAULT ''",
		"commands_json":          "TEXT NOT NULL DEFAULT '[]'",
		"tests_json":             "TEXT NOT NULL DEFAULT '[]'",
		"build_result_json":      "TEXT NOT NULL DEFAULT '{}'",
		"deployment_result_json": "TEXT NOT NULL DEFAULT '{}'",
		"risks_json":             "TEXT NOT NULL DEFAULT '[]'",
		"follow_ups_json":        "TEXT NOT NULL DEFAULT '[]'",
		"preview_url":            "TEXT NOT NULL DEFAULT ''",
		"cluster":                "TEXT NOT NULL DEFAULT ''",
		"namespace":              "TEXT NOT NULL DEFAULT ''",
		"namespace_status":       "TEXT NOT NULL DEFAULT ''",
		"cleanup_status":         "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_session_review_evidence_issue_created ON session_review_evidence(issue_id, created_at DESC)
	`); err != nil {
		return fmt.Errorf("create session_review_evidence issue index: %w", err)
	}
	return nil
}

func (a *app) ensureSessionFailureTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS session_failures (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
			phase TEXT NOT NULL DEFAULT 'unknown',
			status TEXT NOT NULL DEFAULT 'open',
			failed_command TEXT NOT NULL DEFAULT '',
			error_summary TEXT NOT NULL DEFAULT '',
			error_excerpt TEXT NOT NULL DEFAULT '',
			cluster TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
			resource_kind TEXT NOT NULL DEFAULT '',
			resource_name TEXT NOT NULL DEFAULT '',
			evidence_id TEXT NOT NULL DEFAULT '',
			review_evidence_id TEXT NOT NULL DEFAULT '',
			retry_session_id TEXT NOT NULL DEFAULT '',
			continued_session_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(session_id)
		)
	`); err != nil {
		return fmt.Errorf("create session_failures: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_session_failures_issue_created ON session_failures(issue_id, created_at DESC)
	`); err != nil {
		return fmt.Errorf("create session_failures issue index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_session_failures_status ON session_failures(status)
	`); err != nil {
		return fmt.Errorf("create session_failures status index: %w", err)
	}
	return nil
}

func (a *app) ensureIssueHandoffTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_handoffs (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			source_session_id TEXT NOT NULL DEFAULT '',
			source_commit_sha TEXT NOT NULL DEFAULT '',
			branch TEXT NOT NULL DEFAULT '',
			head_commit_sha TEXT NOT NULL DEFAULT '',
			commits_json TEXT NOT NULL DEFAULT '[]',
			kind TEXT NOT NULL DEFAULT 'branch',
			pr_url TEXT NOT NULL DEFAULT '',
			pr_number INTEGER NOT NULL DEFAULT 0,
			pr_state TEXT NOT NULL DEFAULT '',
			pr_title TEXT NOT NULL DEFAULT '',
			preview_url TEXT NOT NULL DEFAULT '',
			evidence_summary TEXT NOT NULL DEFAULT '',
			created_via TEXT NOT NULL DEFAULT 'manual',
			last_checked_at TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create issue_handoffs: %w", err)
	}
	if err := a.ensureTableColumns("issue_handoffs", map[string]string{
		"source_session_id": "TEXT NOT NULL DEFAULT ''",
		"source_commit_sha": "TEXT NOT NULL DEFAULT ''",
		"branch":            "TEXT NOT NULL DEFAULT ''",
		"head_commit_sha":   "TEXT NOT NULL DEFAULT ''",
		"commits_json":      "TEXT NOT NULL DEFAULT '[]'",
		"kind":              "TEXT NOT NULL DEFAULT 'branch'",
		"pr_url":            "TEXT NOT NULL DEFAULT ''",
		"pr_number":         "INTEGER NOT NULL DEFAULT 0",
		"pr_state":          "TEXT NOT NULL DEFAULT ''",
		"pr_title":          "TEXT NOT NULL DEFAULT ''",
		"preview_url":       "TEXT NOT NULL DEFAULT ''",
		"evidence_summary":  "TEXT NOT NULL DEFAULT ''",
		"created_via":       "TEXT NOT NULL DEFAULT 'manual'",
		"last_checked_at":   "TEXT NOT NULL DEFAULT ''",
		"error":             "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_handoffs_issue_updated ON issue_handoffs(issue_id, updated_at DESC)
	`); err != nil {
		return fmt.Errorf("create issue_handoffs issue index: %w", err)
	}
	return nil
}

func (a *app) handleListInbox(w http.ResponseWriter, _ *http.Request) {
	items := make([]inboxItem, 0)
	rows, err := a.db.Query(`
		SELECT ii.id, ii.issue_id, ii.project_id, p.name, ii.title, ii.status, i.assignee, i.assignee_type, ii.unread, ii.updated_at
		FROM inbox_items ii
		JOIN issues i ON i.id = ii.issue_id
		JOIN projects p ON p.id = ii.project_id
		WHERE ii.unread = 1
		ORDER BY ii.updated_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item inboxItem
		var unread int
		if err := rows.Scan(&item.ID, &item.IssueID, &item.ProjectID, &item.ProjectName, &item.Title, &item.Status, &item.Assignee, &item.AssigneeType, &unread, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		item.Unread = unread == 1
		items = append(items, item)
	}
	writeJSON(w, items)
}

func (a *app) handleConfigureControlPlaneSession(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ServerBaseURL string `json:"serverBaseUrl"`
		Token         string `json:"token"`
		WorkspaceID   string `json:"workspaceId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.ServerBaseURL = strings.TrimRight(strings.TrimSpace(input.ServerBaseURL), "/")
	input.Token = strings.TrimSpace(input.Token)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	a.mu.Lock()
	a.controlPlaneBaseURL = input.ServerBaseURL
	a.controlPlaneToken = input.Token
	a.controlPlaneWorkspaceID = input.WorkspaceID
	a.mu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleGetWorkspaceSettings(w http.ResponseWriter, _ *http.Request) {
	settings, err := a.loadWorkspaceSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, settings)
}

func (a *app) handleUpdateWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	var input workspaceSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	settings, err := a.updateWorkspaceSettings(input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, settings)
}

func (a *app) loadWorkspaceSettings() (workspaceSettings, error) {
	settings := workspaceSettings{}
	var autoCreateDraftPR int
	err := a.db.QueryRow(`
		SELECT auto_create_draft_pr, created_at, updated_at
		FROM workspace_settings
		WHERE id = 'default'
	`).Scan(&autoCreateDraftPR, &settings.CreatedAt, &settings.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if ensureErr := a.ensureWorkspaceSettingsTables(); ensureErr != nil {
			return workspaceSettings{}, ensureErr
		}
		err = a.db.QueryRow(`
			SELECT auto_create_draft_pr, created_at, updated_at
			FROM workspace_settings
			WHERE id = 'default'
		`).Scan(&autoCreateDraftPR, &settings.CreatedAt, &settings.UpdatedAt)
	}
	if err != nil {
		return workspaceSettings{}, err
	}
	settings.AutoCreateDraftPR = autoCreateDraftPR == 1
	return settings, nil
}

func (a *app) updateWorkspaceSettings(input workspaceSettingsInput) (workspaceSettings, error) {
	now := nowString()
	if _, err := a.db.Exec(`
		INSERT INTO workspace_settings (id, auto_create_draft_pr, created_at, updated_at)
		VALUES ('default', ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			auto_create_draft_pr = excluded.auto_create_draft_pr,
			updated_at = excluded.updated_at
	`, boolToInt(input.AutoCreateDraftPR), now, now); err != nil {
		return workspaceSettings{}, err
	}
	return a.loadWorkspaceSettings()
}

func (a *app) handleMarkInboxIssueRead(w http.ResponseWriter, r *http.Request) {
	issueID := strings.TrimSpace(chi.URLParam(r, "issueID"))
	if issueID == "" {
		writeError(w, http.StatusBadRequest, errors.New("issueID is required"))
		return
	}
	if _, err := a.db.Exec(`UPDATE inbox_items SET unread = 0 WHERE issue_id = ?`, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.publishInboxEvent(issueID, "read")
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleListActiveWork(w http.ResponseWriter, _ *http.Request) {
	items := make([]activeWorkItem, 0)
	rows, err := a.db.Query(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			i.title,
			i.status,
			COALESCE(e.namespace, '') AS namespace,
			COALESCE(e.namespace_status, '') AS namespace_status,
			COALESCE(e.cleanup_status, '') AS cleanup_status,
			COALESCE(s.status, '') AS session_status,
			CASE
				WHEN s.updated_at IS NOT NULL AND s.updated_at > i.updated_at THEN s.updated_at
				ELSE i.updated_at
			END AS work_updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN issue_test_environments e ON e.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.id = (
			SELECT id
			FROM agent_sessions latest
			WHERE latest.issue_id = i.id
			ORDER BY latest.updated_at DESC
			LIMIT 1
		)
		WHERE COALESCE(i.parent_issue_id, '') = ''
		ORDER BY
			CASE
				WHEN s.status IN ('queued', 'running') THEN 0
				WHEN e.namespace_status IN ('deploying', 'cleanup_requested') THEN 1
				WHEN e.cleanup_status = 'retained' AND e.namespace != '' THEN 2
				ELSE 3
			END,
			work_updated_at DESC
		LIMIT 6
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item activeWorkItem
		if err := rows.Scan(&item.IssueID, &item.ProjectID, &item.ProjectName, &item.Title, &item.Status, &item.Namespace, &item.NamespaceStatus, &item.CleanupStatus, &item.SessionStatus, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, items)
}

func (a *app) handleListIssues(w http.ResponseWriter, _ *http.Request) {
	items := make([]issueListItem, 0)
	rows, err := a.db.Query(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			COALESCE(i.parent_issue_id, '') AS parent_issue_id,
			i.sort_order,
			i.title,
			i.body,
			i.status,
			i.close_reason,
			i.triage_status,
			i.assignee,
			i.assignee_type,
			COALESCE(ii.unread, 0) AS unread,
			COUNT(DISTINCT s.id) AS session_count,
			COUNT(DISTINCT child.id) AS child_issue_count,
			COUNT(DISTINCT CASE WHEN child.status IN ('closed', 'completed') THEN child.id END) AS completed_child_issue_count,
			i.updated_at,
			i.created_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN inbox_items ii ON ii.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		LEFT JOIN issues child ON child.parent_issue_id = i.id
		WHERE COALESCE(i.parent_issue_id, '') = ''
		GROUP BY i.id
		ORDER BY i.updated_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item issueListItem
		var unread int
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.ParentIssueID, &item.SortOrder, &item.Title, &item.Body, &item.Status, &item.CloseReason, &item.TriageStatus, &item.Assignee, &item.AssigneeType, &unread, &item.SessionCount, &item.ChildIssueCount, &item.CompletedChildIssueCount, &item.UpdatedAt, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		item.Unread = unread == 1
		labels, err := a.listIssueLabels(item.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		item.Labels = labels
		items = append(items, item)
	}
	writeJSON(w, items)
}

func (a *app) handleListIssueLabelDefinitions(w http.ResponseWriter, _ *http.Request) {
	definitions, err := a.listIssueLabelDefinitions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, definitions)
}

func (a *app) handleInboxStream(w http.ResponseWriter, r *http.Request) {
	a.streamEvents(w, r, "inbox")
}

func (a *app) handleListAgentProfiles(w http.ResponseWriter, _ *http.Request) {
	profiles, err := a.listAgentProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, profiles)
}

func (a *app) handleCreateAgentProfile(w http.ResponseWriter, r *http.Request) {
	var input agentProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	profile, err := normalizeAgentProfileInput(agentProfile{}, input, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	now := nowString()
	profile.CreatedAt = now
	profile.UpdatedAt = now

	_, err = a.db.Exec(`
		INSERT INTO agent_profiles (id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, profile.ID, profile.Name, profile.Mention, profile.Provider, profile.Description, profile.Instructions, boolToInt(profile.Enabled), boolToInt(profile.BuiltIn), profile.SortOrder, profile.CreatedAt, profile.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("create agent profile: %w", err))
		return
	}
	writeJSON(w, profile)
}

func (a *app) handleUpdateAgentProfile(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	existing, err := a.loadAgentProfile(agentID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("agent profile not found")
		}
		writeError(w, status, err)
		return
	}

	var input agentProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	updated, err := normalizeAgentProfileInput(existing, input, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updated.UpdatedAt = nowString()

	_, err = a.db.Exec(`
		UPDATE agent_profiles
		SET name = ?, mention = ?, provider = ?, description = ?, instructions = ?, enabled = ?, sort_order = ?, updated_at = ?
		WHERE id = ?
	`, updated.Name, updated.Mention, updated.Provider, updated.Description, updated.Instructions, boolToInt(updated.Enabled), updated.SortOrder, updated.UpdatedAt, updated.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("update agent profile: %w", err))
		return
	}
	writeJSON(w, updated)
}

func (a *app) handleListClusters(w http.ResponseWriter, _ *http.Request) {
	clusters, err := a.listClusters()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, clusters)
}

func (a *app) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	var input clusterInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	cluster, err := normalizeClusterInput(cluster{}, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	now := nowString()
	cluster.ID = uuid.NewString()
	cluster.CreatedAt = now
	cluster.UpdatedAt = now

	if err := a.insertCluster(cluster); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, cluster)
}

func (a *app) handleImportClusters(w http.ResponseWriter, r *http.Request) {
	var input kubeconfigImportRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	paths := normalizeKubeconfigImportPaths(input)
	if len(paths) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one kubeconfig path is required"))
		return
	}
	result, err := a.importKubeconfigs(paths)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, result)
}

func (a *app) handleDiscoverDefaultClusters(w http.ResponseWriter, _ *http.Request) {
	paths, err := discoverDefaultKubeconfigPaths(filepath.Join(userHomeDir(), ".kube"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result := discoverKubeconfigs(paths)
	writeJSON(w, result)
}

func (a *app) handleImportDefaultClusters(w http.ResponseWriter, _ *http.Request) {
	paths, err := discoverDefaultKubeconfigPaths(filepath.Join(userHomeDir(), ".kube"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := a.importKubeconfigs(paths)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, result)
}

func (a *app) handleUpdateCluster(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")
	existing, err := a.loadCluster(clusterID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errClusterNotFound
		}
		writeError(w, status, err)
		return
	}

	var input clusterInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := normalizeClusterInput(existing, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updated.ID = existing.ID
	updated.CreatedAt = existing.CreatedAt
	updated.UpdatedAt = nowString()

	if err := a.updateCluster(updated); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	reloaded, err := a.loadCluster(clusterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, reloaded)
}

func (a *app) handleDeleteCluster(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")
	existing, err := a.loadCluster(clusterID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errClusterNotFound
		}
		writeError(w, status, err)
		return
	}
	if existing.ProjectCount > 0 || existing.EnvironmentCount > 0 {
		writeError(w, http.StatusConflict, errors.New("cluster cannot be deleted while projects or test environments reference it"))
		return
	}
	if _, err := a.db.Exec(`DELETE FROM clusters WHERE id = ?`, clusterID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleListProjects(w http.ResponseWriter, _ *http.Request) {
	projects := make([]project, 0)
	rows, err := a.db.Query(`
		SELECT
			p.id,
			p.name,
			p.repo_path,
			p.source_type,
			p.remote_url,
			p.git_provider,
			p.git_owner,
			p.git_repo,
			p.default_branch,
			p.deploy_command,
			p.validation_command,
			p.kube_context,
			p.kubeconfig_path,
			p.namespace,
			p.image_registry_prefix,
			p.preview_domain,
			p.ingress_class,
			p.node_host,
			p.default_cluster_id,
			COALESCE(r.status, 'empty') AS runbook_status,
			COALESCE(r.source, '') AS runbook_source,
			COALESCE(r.source_session_id, '') AS runbook_source_session_id,
			COALESCE(r.updated_at, '') AS runbook_updated_at,
			COUNT(DISTINCT i.id) AS issue_count,
			COUNT(DISTINCT s.id) AS session_count,
			MAX(i.updated_at) AS latest_issue_updated_at,
			p.created_at,
			p.updated_at
		FROM projects p
		LEFT JOIN project_runbooks r ON r.project_id = p.id
		LEFT JOIN issues i ON i.project_id = p.id AND COALESCE(i.parent_issue_id, '') = ''
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		GROUP BY p.id
		ORDER BY p.updated_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p project
		var latestIssueUpdatedAt sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPath, &p.SourceType, &p.RemoteURL, &p.GitProvider, &p.GitOwner, &p.GitRepo, &p.DefaultBranch, &p.DeployCommand, &p.ValidationCommand, &p.KubeContext, &p.KubeconfigPath, &p.Namespace, &p.ImageRegistryPrefix, &p.PreviewDomain, &p.IngressClass, &p.NodeHost, &p.DefaultClusterID, &p.RunbookStatus, &p.RunbookSource, &p.RunbookSourceSessionID, &p.RunbookUpdatedAt, &p.IssueCount, &p.SessionCount, &latestIssueUpdatedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if latestIssueUpdatedAt.Valid {
			p.LatestIssueUpdatedAt = latestIssueUpdatedAt.String
		}
		projects = append(projects, p)
	}
	writeJSON(w, projects)
}

func (a *app) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var input projectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	normalizedProject, err := a.normalizeProjectInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	now := nowString()
	normalizedProject.ID = uuid.NewString()
	normalizedProject.CreatedAt = now
	normalizedProject.UpdatedAt = now

	_, err = a.db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, kubeconfig_path, namespace, image_registry_prefix, preview_domain, ingress_class, node_host, default_cluster_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, normalizedProject.ID, normalizedProject.Name, normalizedProject.RepoPath, normalizedProject.SourceType, normalizedProject.RemoteURL, normalizedProject.GitProvider, normalizedProject.GitOwner, normalizedProject.GitRepo, normalizedProject.DefaultBranch, normalizedProject.DeployCommand, normalizedProject.ValidationCommand, normalizedProject.KubeContext, normalizedProject.KubeconfigPath, normalizedProject.Namespace, normalizedProject.ImageRegistryPrefix, normalizedProject.PreviewDomain, normalizedProject.IngressClass, normalizedProject.NodeHost, normalizedProject.DefaultClusterID, normalizedProject.CreatedAt, normalizedProject.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, normalizedProject)
}

func (a *app) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	var input projectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	existingProject, err := a.loadProject(projectID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("project not found")
		}
		writeError(w, status, err)
		return
	}

	normalizedProject, err := a.normalizeProjectInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	normalizedProject.ID = existingProject.ID
	normalizedProject.DeployCommand = existingProject.DeployCommand
	normalizedProject.ValidationCommand = existingProject.ValidationCommand
	normalizedProject.CreatedAt = existingProject.CreatedAt
	normalizedProject.UpdatedAt = nowString()

	_, err = a.db.Exec(`
		UPDATE projects
		SET name = ?, repo_path = ?, source_type = ?, remote_url = ?, git_provider = ?, git_owner = ?, git_repo = ?, default_branch = ?, deploy_command = ?, validation_command = ?, kube_context = ?, kubeconfig_path = ?, namespace = ?, image_registry_prefix = ?, preview_domain = ?, ingress_class = ?, node_host = ?, default_cluster_id = ?, updated_at = ?
		WHERE id = ?
	`, normalizedProject.Name, normalizedProject.RepoPath, normalizedProject.SourceType, normalizedProject.RemoteURL, normalizedProject.GitProvider, normalizedProject.GitOwner, normalizedProject.GitRepo, normalizedProject.DefaultBranch, normalizedProject.DeployCommand, normalizedProject.ValidationCommand, normalizedProject.KubeContext, normalizedProject.KubeconfigPath, normalizedProject.Namespace, normalizedProject.ImageRegistryPrefix, normalizedProject.PreviewDomain, normalizedProject.IngressClass, normalizedProject.NodeHost, normalizedProject.DefaultClusterID, normalizedProject.UpdatedAt, normalizedProject.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	updatedProject, err := a.loadProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, updatedProject)
}

func (a *app) handleGetProjectRunbook(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectID"))
	if projectID == "" {
		writeError(w, http.StatusBadRequest, errors.New("projectID is required"))
		return
	}
	if _, err := a.loadProject(projectID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errProjectNotFound
		}
		writeError(w, status, err)
		return
	}
	runbook, err := a.loadProjectRunbook(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, runbook)
}

func (a *app) handleUpdateProjectRunbook(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectID"))
	if projectID == "" {
		writeError(w, http.StatusBadRequest, errors.New("projectID is required"))
		return
	}
	if _, err := a.loadProject(projectID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errProjectNotFound
		}
		writeError(w, status, err)
		return
	}

	var input projectRunbookInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor := issueActor{Kind: "human", Name: defaultHumanActorName}
	if token := requestBearerToken(r); token != "" {
		if authenticatedActor, err := a.authenticateActor(r); err == nil && authenticatedActor.Kind == "human" {
			actor = authenticatedActor
		}
	}
	runbook, err := a.saveProjectRunbook(projectID, input.Content, normalizeRunbookStatus(input.Status, input.Content), "human", actor.Name, "", true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, runbook)
}

func (a *app) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	existingProject, err := a.loadProject(projectID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("project not found")
		}
		writeError(w, status, err)
		return
	}

	if existingProject.IssueCount > 0 || existingProject.SessionCount > 0 {
		writeError(w, http.StatusConflict, errors.New("project cannot be deleted after issues or sessions have been created"))
		return
	}

	_, err = a.db.Exec(`DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIssueAttachmentBytes+(1024*1024))
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("image file is required"))
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxIssueAttachmentBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(content) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("image file cannot be empty"))
		return
	}
	if len(content) > maxIssueAttachmentBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("image file must be %dMB or smaller", maxIssueAttachmentBytes/(1024*1024)))
		return
	}

	contentType := normalizeIssueAttachmentContentType(header.Header.Get("Content-Type"), content)
	if !isAllowedIssueAttachmentContentType(contentType) {
		writeError(w, http.StatusUnsupportedMediaType, errors.New("only png, jpeg, gif, and webp images are supported"))
		return
	}

	now := nowString()
	attachment := issueAttachment{
		ID:             uuid.NewString(),
		Filename:       normalizeIssueAttachmentFilename(header.Filename, contentType),
		ContentType:    contentType,
		SizeBytes:      int64(len(content)),
		StorageBackend: "sqlite_blob",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	attachment.URL = issueAttachmentURL(attachment.ID)

	if _, err := a.db.Exec(`
		INSERT INTO issue_attachments (id, filename, content_type, size_bytes, storage_backend, storage_key, content, created_at, updated_at, bound_at)
		VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, '')
	`, attachment.ID, attachment.Filename, attachment.ContentType, attachment.SizeBytes, attachment.StorageBackend, content, now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, attachment)
}

func (a *app) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	attachmentID := strings.TrimSpace(chi.URLParam(r, "attachmentID"))
	if attachmentID == "" {
		writeError(w, http.StatusNotFound, errors.New("attachment not found"))
		return
	}
	var filename string
	var contentType string
	var sizeBytes int64
	var storageBackend string
	var content []byte
	err := a.db.QueryRow(`
		SELECT filename, content_type, size_bytes, storage_backend, content
		FROM issue_attachments
		WHERE id = ?
	`, attachmentID).Scan(&filename, &contentType, &sizeBytes, &storageBackend, &content)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, errors.New("attachment not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if storageBackend != "sqlite_blob" {
		writeError(w, http.StatusNotImplemented, errors.New("attachment storage backend is not available locally"))
		return
	}
	if int64(len(content)) != sizeBytes {
		writeError(w, http.StatusInternalServerError, errors.New("attachment content is incomplete"))
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
	w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(filename))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
}

func (a *app) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	var input struct {
		ProjectID     string           `json:"projectId"`
		Title         string           `json:"title"`
		Body          string           `json:"body"`
		Prompt        string           `json:"prompt"`
		Tasks         []string         `json:"tasks"`
		ChildIssues   []issueTaskInput `json:"childIssues"`
		Labels        []string         `json:"labels"`
		LabelKeys     []string         `json:"labelKeys"`
		Assignee      string           `json:"assignee"`
		AssigneeType  string           `json:"assigneeType"`
		CreatorName   string           `json:"creatorName"`
		CreatorAvatar string           `json:"creatorAvatarUrl"`
		AttachmentIDs []string         `json:"attachmentIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Assignee = strings.TrimSpace(input.Assignee)
	input.AssigneeType = normalizeAssigneeType(input.AssigneeType)
	input.CreatorName = normalizeHumanActorName(firstNonEmpty(input.CreatorName, actor.Name))
	input.CreatorAvatar = normalizeActorAvatarURL(input.CreatorAvatar)
	if input.Body == "" {
		input.Body = input.Prompt
	}
	parentBody, taskDrafts := extractIssueTaskDrafts(input.Body)
	taskDrafts = append(taskDrafts, normalizeIssueTaskStrings(input.Tasks)...)
	taskDrafts = append(taskDrafts, normalizeIssueTaskInputs(input.ChildIssues)...)
	if input.Title == "" {
		if parentBody != "" {
			input.Title = deriveIssueTitle(parentBody)
		} else if len(taskDrafts) > 0 {
			input.Title = deriveIssueTitle(taskDrafts[0].Title)
		} else {
			input.Title = deriveIssueTitle(input.Body)
		}
	}
	if input.Title == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue cannot be empty"))
		return
	}
	input.Body = parentBody
	labelValues := append([]string{}, input.LabelKeys...)
	labelValues = append(labelValues, input.Labels...)
	initialLabels, err := a.normalizeIssueLabelKeys(labelValues)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resolvedProject, err := a.resolveIssueProject(input.ProjectID, input.Title+"\n"+input.Body+"\n"+formatIssueTaskDraftTitles(taskDrafts))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusBadRequest
			err = errors.New("create a project before creating issues")
		} else if errors.Is(err, errProjectNotFound) {
			status = http.StatusNotFound
			err = errors.New("project not found")
		}
		writeError(w, status, err)
		return
	}
	input.ProjectID = resolvedProject.ID

	now := nowString()
	issueID := uuid.NewString()
	inboxID := uuid.NewString()
	status := "open"
	triageStatus := "pending"
	if hasIssueLabelDimension(initialLabels, issueLabelDimensionType) {
		triageStatus = "classified"
	}
	if input.AssigneeType == "" {
		input.AssigneeType = "human"
	}
	if input.AssigneeType != "human" && input.AssigneeType != "agent" {
		writeError(w, http.StatusBadRequest, errors.New("assignee type must be human or agent"))
		return
	}
	assignee := input.Assignee
	if input.AssigneeType == "human" {
		if assignee == "" {
			assignee = input.CreatorName
		}
		assignee = normalizeHumanActorName(assignee)
	}
	for _, task := range taskDrafts {
		if err := validateIssueStatus(task.Status); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, creator_name, creator_avatar_url, environment_url, created_at, updated_at)
		VALUES (?, ?, NULL, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issueID, input.ProjectID, input.Title, input.Body, status, triageStatus, assignee, input.AssigneeType, input.CreatorName, input.CreatorAvatar, "", now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	for index, task := range taskDrafts {
		taskID := uuid.NewString()
		taskStatus := normalizeIssueStatus(task.Status)
		if taskStatus == "" {
			taskStatus = "open"
		}
		if _, err := tx.Exec(`
			INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, creator_name, creator_avatar_url, environment_url, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'none', ?, 'human', ?, ?, '', ?, ?)
		`, taskID, input.ProjectID, issueID, index+1, task.Title, task.Body, taskStatus, input.CreatorName, input.CreatorName, input.CreatorAvatar, now, now); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`, inboxID, issueID, input.ProjectID, input.Title, status, now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if _, err := tx.Exec(`
		INSERT INTO comments (id, issue_id, author_type, author_name, author_avatar_url, body, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), issueID, "system", systemActorName, "", "Issue created and ready for review.", now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	for _, label := range initialLabels {
		if _, err := tx.Exec(`
			INSERT INTO issue_labels (id, issue_id, label_id, name, color, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, uuid.NewString(), issueID, label.ID, label.Name, label.Color, now); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if err := bindIssueAttachmentsTx(tx, input.AttachmentIDs, issueID, "", now); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if triageStatus == "pending" {
		go a.triageIssueType(issueID)
	}

	writeJSON(w, map[string]string{"issueId": issueID})
}

func deriveIssueTitle(body string) string {
	title := strings.TrimSpace(body)
	if title == "" {
		return ""
	}
	if idx := strings.IndexByte(title, '\n'); idx >= 0 {
		title = strings.TrimSpace(title[:idx])
	}
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) > 64 {
		return string(runes[:64]) + "..."
	}
	return title
}

func extractIssueTaskDrafts(body string) (string, []issueTaskDraft) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	parentLines := make([]string, 0, len(lines))
	tasks := make([]issueTaskDraft, 0)
	for _, line := range lines {
		matches := checklistItemPattern.FindStringSubmatch(line)
		if len(matches) != 3 {
			parentLines = append(parentLines, line)
			continue
		}
		title := strings.TrimSpace(matches[2])
		if title == "" {
			parentLines = append(parentLines, line)
			continue
		}
		status := "open"
		if strings.EqualFold(matches[1], "x") {
			status = "closed"
		}
		tasks = append(tasks, issueTaskDraft{Title: title, Status: status})
	}
	return strings.TrimSpace(strings.Join(parentLines, "\n")), tasks
}

func normalizeIssueTaskStrings(values []string) []issueTaskDraft {
	tasks := make([]issueTaskDraft, 0, len(values))
	for _, value := range values {
		title := strings.TrimSpace(value)
		if title == "" {
			continue
		}
		tasks = append(tasks, issueTaskDraft{Title: title, Status: "open"})
	}
	return tasks
}

func normalizeIssueTaskInputs(values []issueTaskInput) []issueTaskDraft {
	tasks := make([]issueTaskDraft, 0, len(values))
	for _, value := range values {
		title := strings.TrimSpace(value.Title)
		body := strings.TrimSpace(value.Body)
		if title == "" {
			title = deriveIssueTitle(body)
		}
		if title == "" {
			continue
		}
		status := normalizeIssueStatus(value.Status)
		if status == "" && value.Completed {
			status = "closed"
		}
		if status == "" {
			status = "open"
		}
		tasks = append(tasks, issueTaskDraft{Title: title, Body: body, Status: status})
	}
	return tasks
}

func formatIssueTaskDraftTitles(tasks []issueTaskDraft) string {
	if len(tasks) == 0 {
		return ""
	}
	titles := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.Title != "" {
			titles = append(titles, task.Title)
		}
	}
	return strings.Join(titles, "\n")
}

func normalizeIssueStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "open", "needs_review", "changes_requested", "ready_for_test", "blocked", "closed", "cancelled":
		return value
	case "review", "in_review":
		return "needs_review"
	case "ready_for_testing", "ready_to_test", "waiting_for_test", "awaiting_test":
		return "ready_for_test"
	case "testing", "test_in_progress":
		return "needs_review"
	case "todo":
		return "open"
	case "done", "completed":
		return "closed"
	case "queued", "running", "in_progress":
		return "open"
	default:
		return value
	}
}

func validateIssueStatus(value string) error {
	switch normalizeIssueStatus(value) {
	case "open", "needs_review", "changes_requested", "ready_for_test", "blocked", "closed", "cancelled":
		return nil
	default:
		return fmt.Errorf("unsupported issue status %q", value)
	}
}

func isClosedIssueStatusValue(value string) bool {
	normalized := normalizeIssueStatus(value)
	return normalized == "closed" || normalized == "cancelled"
}

func (a *app) resolveIssueProject(projectID, text string) (project, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		p, err := a.loadProject(projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return project{}, errProjectNotFound
		}
		return p, err
	}

	projects, err := a.listProjectsForIssueInference()
	if err != nil {
		return project{}, err
	}
	if len(projects) == 0 {
		return project{}, sql.ErrNoRows
	}

	bestProject := projects[0]
	bestScore := issueProjectScore(bestProject, text)
	for _, candidate := range projects[1:] {
		score := issueProjectScore(candidate, text)
		if score > bestScore {
			bestProject = candidate
			bestScore = score
		}
	}
	return bestProject, nil
}

func (a *app) listProjectsForIssueInference() ([]project, error) {
	rows, err := a.db.Query(`
		SELECT id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, kubeconfig_path, namespace, image_registry_prefix, preview_domain, ingress_class, node_host, default_cluster_id, created_at, updated_at
		FROM projects
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]project, 0)
	for rows.Next() {
		var p project
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPath, &p.SourceType, &p.RemoteURL, &p.GitProvider, &p.GitOwner, &p.GitRepo, &p.DefaultBranch, &p.DeployCommand, &p.ValidationCommand, &p.KubeContext, &p.KubeconfigPath, &p.Namespace, &p.ImageRegistryPrefix, &p.PreviewDomain, &p.IngressClass, &p.NodeHost, &p.DefaultClusterID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func issueProjectScore(project project, text string) int {
	normalizedText := strings.ToLower(text)
	score := 0
	score += projectTokenScore(normalizedText, project.Name, 6)
	score += projectTokenScore(normalizedText, project.GitRepo, 5)
	score += projectTokenScore(normalizedText, project.GitOwner+"/"+project.GitRepo, 7)
	score += projectTokenScore(normalizedText, filepath.Base(project.RepoPath), 4)
	score += projectTokenScore(normalizedText, project.RemoteURL, 2)
	return score
}

func projectTokenScore(text, token string, weight int) int {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" || !strings.Contains(text, token) {
		return 0
	}
	return weight
}

func (a *app) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	viewerUserID := ""
	if requestBearerToken(r) != "" {
		if actor, err := a.authenticateActor(r); err == nil && actor.Kind == "human" {
			viewerUserID = actor.UserID
		}
	}
	detail, err := a.loadIssueDetailForViewer(issueID, viewerUserID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, detail)
}

func (a *app) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input struct {
		Title  *string `json:"title"`
		Body   *string `json:"body"`
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, err := a.authenticateActor(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}

	existing, err := a.loadIssue(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}

	updated := existing
	nextStatus := ""
	if input.Title != nil {
		if actor.Kind != "human" {
			writeAuthError(w, errForbidden)
			return
		}
		updated.Title = strings.TrimSpace(*input.Title)
		if updated.Title == "" {
			writeError(w, http.StatusBadRequest, errors.New("issue title cannot be empty"))
			return
		}
	}
	if input.Body != nil {
		if actor.Kind != "human" {
			writeAuthError(w, errForbidden)
			return
		}
		updated.Body = strings.TrimSpace(*input.Body)
	}
	if input.Status != nil {
		nextStatus = normalizeIssueStatus(*input.Status)
		if err := validateIssueStatus(nextStatus); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	statusChanged := nextStatus != "" && existing.Status != nextStatus
	if statusChanged {
		if err := authorizeIssueStatusTransition(existing, nextStatus, actor); err != nil {
			writeAuthError(w, err)
			return
		}
	}

	contentChanged := input.Title != nil || input.Body != nil
	topIssueID := issueID
	if existing.ParentIssueID != "" {
		topIssueID = existing.ParentIssueID
	}
	if contentChanged {
		updated.UpdatedAt = nowString()
		tx, err := a.db.Begin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`
				UPDATE issues
				SET title = ?, body = ?, updated_at = ?
				WHERE id = ?
			`, updated.Title, updated.Body, updated.UpdatedAt, issueID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		if existing.ParentIssueID == "" {
			if _, err := tx.Exec(`
					UPDATE inbox_items SET title = ?, updated_at = ?, unread = 1 WHERE issue_id = ?
				`, updated.Title, updated.UpdatedAt, issueID); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		} else {
			if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, updated.UpdatedAt, existing.ParentIssueID); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			if _, err := tx.Exec(`
					UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?
				`, updated.UpdatedAt, existing.ParentIssueID); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if statusChanged {
		if err := a.transitionIssueStatus(issueID, nextStatus, actor, "Status was changed from the issue UI or API."); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errUnauthorized) {
				status = http.StatusUnauthorized
			} else if errors.Is(err, errForbidden) {
				status = http.StatusForbidden
			}
			writeError(w, status, err)
			return
		}
	} else if contentChanged {
		a.publishInboxEvent(topIssueID, "updated")
	}
	reloaded, err := a.loadIssue(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, reloaded)
}

func (a *app) handleCreateIssueTask(w http.ResponseWriter, r *http.Request) {
	parentIssueID := chi.URLParam(r, "issueID")
	actor, err := a.authenticateActor(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	var input issueTaskInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	parent, err := a.loadIssue(parentIssueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if parent.ParentIssueID != "" {
		writeError(w, http.StatusBadRequest, errors.New("nested issue tasks are not supported yet"))
		return
	}
	if actor.Kind == "agent" && actor.IssueID != parentIssueID {
		writeAuthError(w, errForbidden)
		return
	}

	taskDrafts := normalizeIssueTaskInputs([]issueTaskInput{input})
	if len(taskDrafts) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("task title cannot be empty"))
		return
	}
	task := taskDrafts[0]
	if err := validateIssueStatus(task.Status); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	now := nowString()
	taskID := uuid.NewString()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	var sortOrder int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM issues WHERE parent_issue_id = ?`, parentIssueID).Scan(&sortOrder); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, creator_name, creator_avatar_url, environment_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'none', ?, 'human', ?, ?, '', ?, ?)
	`, taskID, parent.ProjectID, parentIssueID, sortOrder, task.Title, task.Body, task.Status, normalizeHumanActorName(parent.CreatorName), normalizeHumanActorName(parent.CreatorName), normalizeActorAvatarURL(parent.CreatorAvatar), now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, now, parentIssueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, parentIssueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.publishInboxEvent(parentIssueID, "updated")
	item, err := a.loadIssueListItem(taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, item)
}

func (a *app) handleDeleteIssueTask(w http.ResponseWriter, r *http.Request) {
	parentIssueID := chi.URLParam(r, "issueID")
	taskID := chi.URLParam(r, "taskID")
	actor, err := a.authenticateActor(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}

	parent, err := a.loadIssue(parentIssueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if parent.ParentIssueID != "" {
		writeError(w, http.StatusBadRequest, errors.New("nested issue tasks are not supported yet"))
		return
	}
	if actor.Kind == "agent" && actor.IssueID != parentIssueID {
		writeAuthError(w, errForbidden)
		return
	}

	task, err := a.loadIssue(taskID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("task not found")
		}
		writeError(w, status, err)
		return
	}
	if task.ParentIssueID != parentIssueID {
		writeError(w, http.StatusNotFound, errors.New("task not found"))
		return
	}

	now := nowString()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(`DELETE FROM issues WHERE id = ? AND parent_issue_id = ?`, taskID, parentIssueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, errors.New("task not found"))
		return
	}
	if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, now, parentIssueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, parentIssueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.publishInboxEvent(parentIssueID, "updated")
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleUpdateIssueLabels(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	if _, ok := a.requireHumanActor(w, r); !ok {
		return
	}
	var input struct {
		Labels    []string `json:"labels"`
		LabelKeys []string `json:"labelKeys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	labelValues := append([]string{}, input.LabelKeys...)
	labelValues = append(labelValues, input.Labels...)
	labels, err := a.normalizeIssueLabelKeys(labelValues)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	var projectID string
	var currentTriageStatus string
	if err := tx.QueryRow(`SELECT project_id, triage_status FROM issues WHERE id = ?`, issueID).Scan(&projectID, &currentTriageStatus); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}

	if _, err := tx.Exec(`DELETE FROM issue_labels WHERE issue_id = ?`, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	now := nowString()
	for _, label := range labels {
		if _, err := tx.Exec(`
			INSERT INTO issue_labels (id, issue_id, label_id, name, color, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, uuid.NewString(), issueID, label.ID, label.Name, label.Color, now); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	nextTriageStatus := currentTriageStatus
	if hasIssueLabelDimension(labels, issueLabelDimensionType) {
		nextTriageStatus = "classified"
	} else if currentTriageStatus == "classified" {
		nextTriageStatus = "none"
	}
	if _, err := tx.Exec(`UPDATE issues SET triage_status = ?, updated_at = ? WHERE id = ?`, nextTriageStatus, now, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.publishInboxEvent(issueID, "labels")
	updatedLabels, err := a.listIssueLabels(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, updatedLabels)
}

func (a *app) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	var input struct {
		Body          string   `json:"body"`
		AuthorName    string   `json:"authorName"`
		AuthorAvatar  string   `json:"authorAvatarUrl"`
		AttachmentIDs []string `json:"attachmentIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	input.AuthorName = normalizeHumanActorName(firstNonEmpty(input.AuthorName, actor.Name))
	input.AuthorAvatar = normalizeActorAvatarURL(input.AuthorAvatar)
	if input.Body == "" {
		writeError(w, http.StatusBadRequest, errors.New("comment body cannot be empty"))
		return
	}
	now := nowString()
	commentID := uuid.NewString()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO comments (id, issue_id, author_type, author_user_id, author_name, author_avatar_url, body, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, commentID, issueID, "human", actor.UserID, input.AuthorName, input.AuthorAvatar, input.Body, now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := bindIssueAttachmentsTx(tx, input.AttachmentIDs, issueID, commentID, now); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, now, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.publishInboxEvent(issueID, "updated")
	writeJSON(w, map[string]any{"ok": true, "commentId": commentID})
}

func (a *app) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	commentID := chi.URLParam(r, "commentID")
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	var input struct {
		Body          string   `json:"body"`
		AttachmentIDs []string `json:"attachmentIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" {
		writeError(w, http.StatusBadRequest, errors.New("comment body cannot be empty"))
		return
	}

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	existing, err := loadCommentTx(tx, issueID, commentID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("comment not found")
		}
		writeError(w, status, err)
		return
	}
	if existing.AuthorType != "human" || !commentBelongsToActor(existing, actor) {
		writeAuthError(w, errForbidden)
		return
	}
	if a.issueHasActiveSession(issueID) {
		writeError(w, http.StatusConflict, errors.New("stop the active session before editing comments"))
		return
	}
	if err := ensureLastIssueCommentTx(tx, issueID, commentID); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err := ensureCommentNotSessionTriggerTx(tx, commentID); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}

	now := nowString()
	if _, err := tx.Exec(`
		UPDATE comments
		SET body = ?, author_name = ?, author_avatar_url = ?, updated_at = ?, edited_at = ?
		WHERE id = ? AND issue_id = ?
	`, input.Body, commentActorName(actor), actor.AvatarURL, now, now, commentID, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := bindIssueAttachmentsTx(tx, input.AttachmentIDs, issueID, commentID, now); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, now, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.publishInboxEvent(issueID, "updated")
	updated, err := a.loadComment(issueID, commentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "comment": updated})
}

func (a *app) handleSetCommentReaction(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	commentID := chi.URLParam(r, "commentID")
	reaction, err := normalizeCommentReaction(chi.URLParam(r, "reaction"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(actor.UserID) == "" {
		writeError(w, http.StatusForbidden, errors.New("authenticated user id is required"))
		return
	}

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()
	if err := ensureCommentOnIssueTx(tx, issueID, commentID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("comment not found")
		}
		writeError(w, status, err)
		return
	}
	if _, err := tx.Exec(`
		INSERT INTO comment_reactions (id, issue_id, comment_id, reaction, user_id, actor_name, actor_avatar_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(comment_id, user_id, reaction) DO UPDATE SET
			actor_name = excluded.actor_name,
			actor_avatar_url = excluded.actor_avatar_url
	`, uuid.NewString(), issueID, commentID, reaction, actor.UserID, commentActorName(actor), actor.AvatarURL, nowString()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) handleDeleteCommentReaction(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	commentID := chi.URLParam(r, "commentID")
	reaction, err := normalizeCommentReaction(chi.URLParam(r, "reaction"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(actor.UserID) == "" {
		writeError(w, http.StatusForbidden, errors.New("authenticated user id is required"))
		return
	}

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()
	if err := ensureCommentOnIssueTx(tx, issueID, commentID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("comment not found")
		}
		writeError(w, status, err)
		return
	}
	if _, err := tx.Exec(`
		DELETE FROM comment_reactions
		WHERE issue_id = ? AND comment_id = ? AND reaction = ? AND user_id = ?
	`, issueID, commentID, reaction, actor.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) handleAssignIssueToAgent(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	var input sessionRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	session, err := a.queueAgentSession(issueID, input, actor)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		} else if errors.Is(err, errUnknownAgentProfile) || errors.Is(err, errInvalidRuntimeMode) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}

	writeJSON(w, map[string]string{"sessionId": session.ID})
}

func (a *app) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	var input sessionRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	session, err := a.queueAgentSession(issueID, input, actor)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		} else if errors.Is(err, errUnknownAgentProfile) || errors.Is(err, errInvalidRuntimeMode) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}

	writeJSON(w, map[string]string{"sessionId": session.ID})
}

func (a *app) handleCreateServerIssueTeamSession(w http.ResponseWriter, r *http.Request) {
	issueID := strings.TrimSpace(chi.URLParam(r, "issueID"))
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	var input serverIssueSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if input.IssueID == "" {
		input.IssueID = issueID
	}
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.WorkspaceID == "" {
		_, _, configuredWorkspaceID := a.controlPlaneSession()
		input.WorkspaceID = configuredWorkspaceID
	}
	input.IssueID = strings.TrimSpace(input.IssueID)
	input.CommentID = strings.TrimSpace(input.CommentID)
	input.Provider = strings.TrimSpace(input.Provider)
	input.AgentProfile = strings.TrimSpace(input.AgentProfile)
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	input.Command = strings.TrimSpace(input.Command)
	input.Branch = strings.TrimSpace(input.Branch)
	input.SourceSessionID = strings.TrimSpace(input.SourceSessionID)
	input.SourceCommitSHA = strings.TrimSpace(input.SourceCommitSHA)
	if input.IssueID == "" || input.IssueID != issueID {
		writeError(w, http.StatusBadRequest, errors.New("issue id mismatch"))
		return
	}
	if input.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspace id is required"))
		return
	}
	_, _, configuredWorkspaceID := a.controlPlaneSession()
	if configuredWorkspaceID != "" && input.WorkspaceID != configuredWorkspaceID {
		writeError(w, http.StatusForbidden, errors.New("selected workspace does not match runner control-plane session"))
		return
	}
	if input.Command == "" {
		writeError(w, http.StatusBadRequest, errors.New("session command is required"))
		return
	}
	if input.RuntimeMode == "" {
		input.RuntimeMode = "team"
	}
	if input.RuntimeMode != "team" {
		writeError(w, http.StatusBadRequest, errors.New("server issue bridge only supports team runtime"))
		return
	}

	session, err := a.queueServerIssueTeamSession(input, actor)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errUnknownAgentProfile) || errors.Is(err, errInvalidRuntimeMode) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}

	writeJSON(w, map[string]string{"sessionId": session.ID})
}

func (a *app) handleStartIssueTestDeploy(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	var input testDeployRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if a.issueHasActiveSession(issueID) {
		writeError(w, http.StatusConflict, errActiveIssueSession)
		return
	}

	environment, err := a.buildIssueTestEnvironment(detail, input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errClusterNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	sourceNode, err := a.loadIssueChangeNodeForDeploy(issueID, input.SourceCommitSHA, input.SourceSessionID, detail.Project)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	environment.SourceSessionID = sourceNode.SessionID
	environment.SourceCommitSHA = sourceNode.CommitSHA
	command := buildIssueTestDeployPrompt(detail, environment, sourceNode)
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	session, err := a.queueAgentSession(issueID, sessionRequest{
		Provider:        "codex",
		AgentProfile:    strings.TrimSpace(input.AgentProfile),
		Command:         command,
		SourceSessionID: sourceNode.SessionID,
		SourceCommitSHA: sourceNode.CommitSHA,
	}, actor)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errUnknownAgentProfile) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}

	environment.LastDeploySessionID = session.ID
	environment.NamespaceStatus = "deploying"
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.addSystemComment(issueID, fmt.Sprintf(
		"Queued test deployment session `%s` for namespace `%s`.\n\nSource commit: `%s`\nCluster: `%s`\nRegistry: `%s`\nExposure: %s",
		shortID(session.ID),
		environment.Namespace,
		shortID(sourceNode.CommitSHA),
		environment.ClusterID,
		environment.ImageRegistryPrefix,
		previewStrategyLabel(environment),
	))
	a.publishInboxEvent(issueID, "test-deploy")

	writeJSON(w, map[string]any{
		"sessionId":       session.ID,
		"testEnvironment": environment,
	})
}

func (a *app) handleRequestIssueTestEnvironmentCleanup(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	var input testDeployRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if detail.TestEnvironment == nil || strings.TrimSpace(detail.TestEnvironment.Namespace) == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue has no test namespace to clean up"))
		return
	}
	if a.issueHasActiveSession(issueID) {
		writeError(w, http.StatusConflict, errActiveIssueSession)
		return
	}

	environment := *detail.TestEnvironment
	environment.NamespaceStatus = "cleanup_requested"
	environment.CleanupStatus = "cleanup_requested"
	command := buildIssueTestCleanupPrompt(detail, environment)
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	session, err := a.queueAgentSession(issueID, sessionRequest{
		Provider:     "codex",
		AgentProfile: strings.TrimSpace(input.AgentProfile),
		Command:      command,
	}, actor)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errUnknownAgentProfile) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	environment.LastCleanupSessionID = session.ID
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.addSystemComment(issueID, fmt.Sprintf("Queued namespace cleanup session `%s` for `%s`.", shortID(session.ID), environment.Namespace))
	a.publishInboxEvent(issueID, "test-cleanup")
	writeJSON(w, map[string]any{
		"sessionId":       session.ID,
		"testEnvironment": environment,
	})
}

func (a *app) handleRetainIssueTestEnvironment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	if _, ok := a.requireHumanActor(w, r); !ok {
		return
	}
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if detail.TestEnvironment == nil || strings.TrimSpace(detail.TestEnvironment.Namespace) == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue has no test namespace to retain"))
		return
	}
	if a.issueHasActiveSession(issueID) {
		writeError(w, http.StatusConflict, errActiveIssueSession)
		return
	}
	environment := *detail.TestEnvironment
	environment.CleanupStatus = "retained"
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.addSystemComment(issueID, fmt.Sprintf("Retained test namespace `%s` for later inspection.", environment.Namespace))
	a.publishInboxEvent(issueID, "test-retain")
	writeJSON(w, environment)
}

func (a *app) handleListIssueTestEnvironmentResources(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	if _, ok := a.requireHumanActor(w, r); !ok {
		return
	}
	if _, ok := r.URL.Query()["namespace"]; ok {
		writeError(w, http.StatusBadRequest, errors.New("namespace is fixed by the issue test environment"))
		return
	}
	if _, err := a.loadIssue(issueID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	environment, err := a.loadIssueTestEnvironment(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if environment == nil || strings.TrimSpace(environment.Namespace) == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue has no test namespace to inspect"))
		return
	}

	resolvedEnvironment := *environment
	clusterName := ""
	if strings.TrimSpace(resolvedEnvironment.ClusterID) != "" {
		cluster, err := a.loadCluster(resolvedEnvironment.ClusterID)
		if err == nil {
			clusterName = cluster.Name
			if strings.TrimSpace(resolvedEnvironment.KubeconfigPath) == "" {
				resolvedEnvironment.KubeconfigPath = cluster.KubeconfigPath
			}
			if strings.TrimSpace(resolvedEnvironment.KubeContext) == "" {
				resolvedEnvironment.KubeContext = cluster.KubeContext
			}
			if strings.TrimSpace(resolvedEnvironment.NodeHost) == "" {
				resolvedEnvironment.NodeHost = cluster.NodeHost
			}
			if strings.TrimSpace(resolvedEnvironment.ExposureMode) == "" {
				resolvedEnvironment.ExposureMode = cluster.ExposureMode
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	resources, err := listIssueTestEnvironmentResources(r.Context(), resolvedEnvironment, clusterName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, resources)
}

func listIssueTestEnvironmentResources(ctx context.Context, environment issueTestEnvironment, clusterName string) (issueTestEnvironmentResources, error) {
	namespace := strings.TrimSpace(environment.Namespace)
	if namespace == "" {
		return issueTestEnvironmentResources{}, errors.New("issue test namespace is empty")
	}
	clientset, err := issueTestEnvironmentKubernetesClient(environment)
	if err != nil {
		return issueTestEnvironmentResources{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	resources := issueTestEnvironmentResources{
		IssueID:         environment.IssueID,
		ClusterID:       environment.ClusterID,
		ClusterName:     clusterName,
		Context:         environment.KubeContext,
		Namespace:       namespace,
		NamespaceStatus: environment.NamespaceStatus,
		CleanupStatus:   environment.CleanupStatus,
		ExposureMode:    environment.ExposureMode,
		PreviewURL:      environment.PreviewURL,
		NodeHost:        environment.NodeHost,
		RefreshedAt:     nowString(),
		Pods:            []kubernetesPodResource{},
		Services:        []kubernetesServiceResource{},
		Deployments:     []kubernetesDeploymentResource{},
		Ingresses:       []kubernetesIngressResource{},
		Events:          []kubernetesEventResource{},
		Errors:          []kubernetesResourceFetchError{},
	}

	if pods, err := listKubernetesPods(ctx, clientset, namespace); err != nil {
		resources.Errors = append(resources.Errors, kubernetesResourceFetchError{Section: "pods", Message: err.Error()})
	} else {
		resources.Pods = pods
	}
	if services, err := listKubernetesServices(ctx, clientset, namespace, environment.NodeHost); err != nil {
		resources.Errors = append(resources.Errors, kubernetesResourceFetchError{Section: "services", Message: err.Error()})
	} else {
		resources.Services = services
	}
	if deployments, err := listKubernetesDeployments(ctx, clientset, namespace); err != nil {
		resources.Errors = append(resources.Errors, kubernetesResourceFetchError{Section: "deployments", Message: err.Error()})
	} else {
		resources.Deployments = deployments
	}
	if ingresses, err := listKubernetesIngresses(ctx, clientset, namespace); err != nil {
		resources.Errors = append(resources.Errors, kubernetesResourceFetchError{Section: "ingresses", Message: err.Error()})
	} else {
		resources.Ingresses = ingresses
	}
	if events, err := listKubernetesEvents(ctx, clientset, namespace); err != nil {
		resources.Errors = append(resources.Errors, kubernetesResourceFetchError{Section: "events", Message: err.Error()})
	} else {
		resources.Events = events
	}
	return resources, nil
}

func issueTestEnvironmentKubernetesClient(environment issueTestEnvironment) (*kubernetes.Clientset, error) {
	kubeconfigPath, err := normalizeKubeconfigPath(environment.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext := strings.TrimSpace(environment.KubeContext); kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		overrides,
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	config.UserAgent = "mspace-runner/test-environment-resources"
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return clientset, nil
}

func listKubernetesPods(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]kubernetesPodResource, error) {
	list, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	pods := make([]kubernetesPodResource, 0, len(list.Items))
	for _, pod := range list.Items {
		pods = append(pods, mapKubernetesPod(pod))
	}
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})
	return pods, nil
}

func mapKubernetesPod(pod corev1.Pod) kubernetesPodResource {
	resource := kubernetesPodResource{
		Name:      pod.Name,
		Phase:     string(pod.Status.Phase),
		NodeName:  pod.Spec.NodeName,
		PodIP:     pod.Status.PodIP,
		HostIP:    pod.Status.HostIP,
		CreatedAt: kubernetesTimeString(pod.CreationTimestamp),
	}
	statusesByName := map[string]corev1.ContainerStatus{}
	for _, status := range pod.Status.ContainerStatuses {
		statusesByName[status.Name] = status
	}
	for _, container := range pod.Spec.Containers {
		status, ok := statusesByName[container.Name]
		item := kubernetesPodContainer{Name: container.Name, State: "waiting"}
		if ok {
			state, reason := kubernetesContainerState(status.State)
			item.Ready = status.Ready
			item.RestartCount = status.RestartCount
			item.State = state
			item.Reason = reason
			resource.Restarts += status.RestartCount
			if status.Ready {
				resource.ReadyContainers++
			}
		}
		resource.Containers = append(resource.Containers, item)
	}
	resource.TotalContainers = len(pod.Spec.Containers)
	if resource.Containers == nil {
		resource.Containers = []kubernetesPodContainer{}
	}
	return resource
}

func kubernetesContainerState(state corev1.ContainerState) (string, string) {
	if state.Waiting != nil {
		return "waiting", state.Waiting.Reason
	}
	if state.Running != nil {
		return "running", ""
	}
	if state.Terminated != nil {
		return "terminated", firstNonEmpty(state.Terminated.Reason, fmt.Sprintf("exit %d", state.Terminated.ExitCode))
	}
	return "unknown", ""
}

func listKubernetesServices(ctx context.Context, clientset *kubernetes.Clientset, namespace, nodeHost string) ([]kubernetesServiceResource, error) {
	list, err := clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	services := make([]kubernetesServiceResource, 0, len(list.Items))
	for _, service := range list.Items {
		services = append(services, mapKubernetesService(service, nodeHost))
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})
	return services, nil
}

func mapKubernetesService(service corev1.Service, nodeHost string) kubernetesServiceResource {
	externalIPs := append([]string{}, service.Spec.ExternalIPs...)
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if strings.TrimSpace(ingress.IP) != "" {
			externalIPs = append(externalIPs, ingress.IP)
		}
		if strings.TrimSpace(ingress.Hostname) != "" {
			externalIPs = append(externalIPs, ingress.Hostname)
		}
	}
	resource := kubernetesServiceResource{
		Name:       service.Name,
		Type:       string(service.Spec.Type),
		ClusterIP:  service.Spec.ClusterIP,
		ExternalIP: strings.Join(uniqueStrings(externalIPs), ", "),
		CreatedAt:  kubernetesTimeString(service.CreationTimestamp),
		Ports:      []kubernetesServicePort{},
	}
	for _, port := range service.Spec.Ports {
		item := kubernetesServicePort{
			Name:       port.Name,
			Protocol:   string(port.Protocol),
			Port:       port.Port,
			TargetPort: port.TargetPort.String(),
			NodePort:   port.NodePort,
		}
		if service.Spec.Type == corev1.ServiceTypeNodePort || service.Spec.Type == corev1.ServiceTypeLoadBalancer {
			item.URL = nodePortPreviewURL(nodeHost, port.NodePort)
		}
		resource.Ports = append(resource.Ports, item)
	}
	return resource
}

func nodePortPreviewURL(nodeHost string, nodePort int32) string {
	host := strings.TrimSpace(nodeHost)
	if host == "" || nodePort <= 0 {
		return ""
	}
	scheme := "http"
	if parsed, err := url.Parse(host); err == nil && parsed.Scheme != "" {
		scheme = parsed.Scheme
		host = parsed.Hostname()
	}
	if strings.Count(host, ":") > 1 && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, nodePort)
}

func listKubernetesDeployments(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]kubernetesDeploymentResource, error) {
	list, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	deployments := make([]kubernetesDeploymentResource, 0, len(list.Items))
	for _, deployment := range list.Items {
		deployments = append(deployments, mapKubernetesDeployment(deployment))
	}
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].Name < deployments[j].Name
	})
	return deployments, nil
}

func mapKubernetesDeployment(deployment appsv1.Deployment) kubernetesDeploymentResource {
	resource := kubernetesDeploymentResource{
		Name:              deployment.Name,
		Replicas:          deployment.Status.Replicas,
		ReadyReplicas:     deployment.Status.ReadyReplicas,
		UpdatedReplicas:   deployment.Status.UpdatedReplicas,
		AvailableReplicas: deployment.Status.AvailableReplicas,
		CreatedAt:         kubernetesTimeString(deployment.CreationTimestamp),
		Conditions:        []kubernetesCondition{},
	}
	for _, condition := range deployment.Status.Conditions {
		resource.Conditions = append(resource.Conditions, kubernetesCondition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}
	return resource
}

func listKubernetesIngresses(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]kubernetesIngressResource, error) {
	list, err := clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	ingresses := make([]kubernetesIngressResource, 0, len(list.Items))
	for _, ingress := range list.Items {
		ingresses = append(ingresses, mapKubernetesIngress(ingress))
	}
	sort.Slice(ingresses, func(i, j int) bool {
		return ingresses[i].Name < ingresses[j].Name
	})
	return ingresses, nil
}

func mapKubernetesIngress(ingress networkingv1.Ingress) kubernetesIngressResource {
	className := ""
	if ingress.Spec.IngressClassName != nil {
		className = *ingress.Spec.IngressClassName
	}
	hosts := []string{}
	for _, rule := range ingress.Spec.Rules {
		if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	for _, tls := range ingress.Spec.TLS {
		hosts = append(hosts, tls.Hosts...)
	}
	addresses := []string{}
	for _, item := range ingress.Status.LoadBalancer.Ingress {
		if strings.TrimSpace(item.IP) != "" {
			addresses = append(addresses, item.IP)
		}
		if strings.TrimSpace(item.Hostname) != "" {
			addresses = append(addresses, item.Hostname)
		}
	}
	return kubernetesIngressResource{
		Name:      ingress.Name,
		ClassName: className,
		Hosts:     uniqueStrings(hosts),
		Addresses: uniqueStrings(addresses),
		CreatedAt: kubernetesTimeString(ingress.CreationTimestamp),
	}
}

func listKubernetesEvents(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]kubernetesEventResource, error) {
	list, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return kubernetesEventSortTime(list.Items[i]).After(kubernetesEventSortTime(list.Items[j]))
	})
	limit := len(list.Items)
	if limit > 50 {
		limit = 50
	}
	events := make([]kubernetesEventResource, 0, limit)
	for _, event := range list.Items[:limit] {
		events = append(events, mapKubernetesEvent(event))
	}
	return events, nil
}

func mapKubernetesEvent(event corev1.Event) kubernetesEventResource {
	return kubernetesEventResource{
		Type:         event.Type,
		Reason:       event.Reason,
		Message:      event.Message,
		InvolvedKind: event.InvolvedObject.Kind,
		InvolvedName: event.InvolvedObject.Name,
		Count:        event.Count,
		FirstSeen:    kubernetesEventTimeString(event.FirstTimestamp.Time),
		LastSeen:     kubernetesEventTimeString(kubernetesEventSortTime(event)),
		CreatedAt:    kubernetesTimeString(event.CreationTimestamp),
	}
}

func kubernetesEventSortTime(event corev1.Event) time.Time {
	if !event.EventTime.Time.IsZero() {
		return event.EventTime.Time
	}
	if !event.LastTimestamp.Time.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.FirstTimestamp.Time.IsZero() {
		return event.FirstTimestamp.Time
	}
	return event.CreationTimestamp.Time
}

func kubernetesTimeString(value metav1.Time) string {
	if value.Time.IsZero() {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339)
}

func kubernetesEventTimeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (a *app) handleProbeIssueTestEnvironment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	if _, ok := a.requireHumanActor(w, r); !ok {
		return
	}
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if detail.TestEnvironment == nil || strings.TrimSpace(detail.TestEnvironment.Namespace) == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue has no test namespace to check"))
		return
	}
	deploySession := probeSessionForIssue(detail)
	if deploySession == nil {
		writeError(w, http.StatusNotFound, errors.New("deploy session or preview artifact not found"))
		return
	}
	a.appendSessionLog(deploySession.ID, "system", "Preview status check requested from Issue Detail.")
	a.reconcileIssueTestEnvironmentForSession(*deploySession, detail.Project, "probe")
	environment, err := a.loadIssueTestEnvironment(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if environment == nil {
		writeError(w, http.StatusNotFound, errors.New("issue test environment not found"))
		return
	}
	writeJSON(w, environment)
}

func (a *app) handleCreateIssuePullRequest(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	if _, ok := a.requireHumanActor(w, r); !ok {
		return
	}
	var input issuePullRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if current := currentIssuePullRequestHandoff(detail.Handoffs); current != nil && strings.TrimSpace(current.PRURL) != "" {
		writeError(w, http.StatusConflict, fmt.Errorf("issue already has a pull request handoff: %s", current.PRURL))
		return
	}
	handoff, err := a.buildIssueHandoff(detail, issueHandoffRequest{
		SourceSessionID: input.SourceSessionID,
		SourceCommitSHA: input.SourceCommitSHA,
		Kind:            "pr",
	}, "gh")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.createPullRequestForHandoff(detail, &handoff, strings.TrimSpace(input.Title), input.Draft); err != nil {
		handoff.Error = err.Error()
		_, _ = a.storeIssueHandoff(handoff)
		a.publishInboxEvent(issueID, "handoff")
		writeError(w, http.StatusBadRequest, err)
		return
	}
	handoff, err = a.storeIssueHandoff(handoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.addSystemComment(issueID, issueHandoffComment(handoff))
	a.publishInboxEvent(issueID, "handoff")
	writeJSON(w, handoff)
}

func (a *app) handleRefreshIssueHandoff(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	handoffID := chi.URLParam(r, "handoffID")
	if _, ok := a.requireHumanActor(w, r); !ok {
		return
	}
	handoff, err := a.loadIssueHandoff(issueID, handoffID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("handoff not found")
		}
		writeError(w, status, err)
		return
	}
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := refreshIssueHandoffPRStatus(detail.Project, &handoff); err != nil {
		handoff.Error = err.Error()
		handoff.LastCheckedAt = nowString()
	}
	handoff, err = a.storeIssueHandoff(handoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, handoff)
}

func (a *app) maybeAutoCreateIssuePullRequest(session agentSession, project project, changeNode *issueChangeNode) {
	if changeNode == nil {
		return
	}
	settings, err := a.loadWorkspaceSettings()
	if err != nil {
		a.appendSessionLog(session.ID, "system", "Auto PR skipped: workspace settings could not be loaded: "+err.Error())
		return
	}
	if !settings.AutoCreateDraftPR {
		return
	}

	detail, err := a.loadIssueDetail(session.IssueID)
	if err != nil {
		a.appendSessionLog(session.ID, "system", "Auto PR skipped: issue detail could not be loaded: "+err.Error())
		return
	}
	if current := currentIssuePullRequestHandoff(detail.Handoffs); current != nil && strings.TrimSpace(current.PRURL) != "" {
		handoff := *current
		if err := refreshIssueHandoffPRStatus(project, &handoff); err != nil {
			handoff.Error = err.Error()
			handoff.LastCheckedAt = nowString()
		}
		if _, err := a.storeIssueHandoff(handoff); err != nil {
			a.appendSessionLog(session.ID, "system", "Auto PR refresh failed: "+err.Error())
			return
		}
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Auto PR refreshed existing issue pull request %s.", valueOrUnset(handoff.PRURL)))
		a.publishInboxEvent(session.IssueID, "handoff")
		return
	}

	handoff, err := a.buildIssueHandoff(detail, issueHandoffRequest{
		SourceSessionID: changeNode.SessionID,
		SourceCommitSHA: changeNode.CommitSHA,
		Kind:            "pr",
	}, "auto")
	if err != nil {
		a.appendSessionLog(session.ID, "system", "Auto PR skipped: "+err.Error())
		return
	}
	if err := a.createPullRequestForHandoff(detail, &handoff, "", true); err != nil {
		handoff.Error = err.Error()
		if _, storeErr := a.storeIssueHandoff(handoff); storeErr != nil {
			a.appendSessionLog(session.ID, "system", "Auto PR failed and handoff state could not be stored: "+storeErr.Error())
			return
		}
		a.appendSessionLog(session.ID, "system", "Auto PR failed: "+err.Error())
		a.publishInboxEvent(session.IssueID, "handoff")
		return
	}
	handoff, err = a.storeIssueHandoff(handoff)
	if err != nil {
		a.appendSessionLog(session.ID, "system", "Auto PR created but handoff state could not be stored: "+err.Error())
		return
	}
	a.addSystemComment(session.IssueID, issueHandoffComment(handoff))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Auto PR handoff ready: %s.", valueOrUnset(handoff.PRURL)))
	a.publishInboxEvent(session.IssueID, "handoff")
}

func (a *app) buildIssueHandoff(detail issueDetail, input issueHandoffRequest, createdVia string) (issueHandoff, error) {
	issueID := detail.Issue.ID
	input.SourceSessionID = strings.TrimSpace(input.SourceSessionID)
	input.SourceCommitSHA = strings.TrimSpace(input.SourceCommitSHA)
	input.Branch = strings.TrimSpace(input.Branch)
	input.PRURL = strings.TrimSpace(input.PRURL)
	input.PRTitle = strings.TrimSpace(input.PRTitle)
	input.EvidenceSummary = strings.TrimSpace(input.EvidenceSummary)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	if input.Kind == "" {
		input.Kind = "branch"
		if input.PRURL != "" {
			input.Kind = "pr"
		}
	}
	if input.Kind != "branch" && input.Kind != "pr" {
		return issueHandoff{}, fmt.Errorf("unsupported handoff kind %q", input.Kind)
	}
	if input.PRURL != "" {
		normalizedURL, err := normalizePullRequestURL(input.PRURL)
		if err != nil {
			return issueHandoff{}, err
		}
		input.PRURL = normalizedURL
		input.Kind = "pr"
	}

	var sourceNode *issueChangeNode
	if input.SourceCommitSHA != "" || input.SourceSessionID != "" || (input.Branch == "" && input.PRURL == "") {
		node, err := a.loadIssueChangeNodeForDeploy(issueID, input.SourceCommitSHA, input.SourceSessionID, detail.Project)
		if err != nil {
			if input.Branch == "" {
				return issueHandoff{}, err
			}
		} else {
			sourceNode = &node
		}
	}

	sourceSessionID := input.SourceSessionID
	sourceCommitSHA := input.SourceCommitSHA
	branch := input.Branch
	if sourceNode != nil {
		sourceSessionID = sourceNode.SessionID
		sourceCommitSHA = sourceNode.CommitSHA
		if branch == "" {
			branch = sourceNode.Branch
		}
	}
	if branch == "" && input.PRURL == "" && sourceCommitSHA == "" {
		return issueHandoff{}, errors.New("handoff requires a source commit, branch, or pull request URL")
	}

	handoff := issueHandoff{
		ID:              uuid.NewString(),
		IssueID:         issueID,
		SourceSessionID: sourceSessionID,
		SourceCommitSHA: sourceCommitSHA,
		Branch:          branch,
		Kind:            input.Kind,
		PRURL:           input.PRURL,
		PRNumber:        parsePullRequestNumber(input.PRURL),
		PRTitle:         input.PRTitle,
		PreviewURL:      issueHandoffPreviewURL(detail, sourceSessionID, sourceCommitSHA),
		CreatedVia:      firstNonEmpty(strings.TrimSpace(createdVia), "manual"),
		CreatedAt:       nowString(),
	}
	if handoff.Kind == "pr" {
		if current := currentIssuePullRequestHandoff(detail.Handoffs); current != nil {
			handoff.ID = current.ID
			handoff.CreatedAt = current.CreatedAt
		}
	}
	handoff.EvidenceSummary = input.EvidenceSummary
	if handoff.EvidenceSummary == "" {
		handoff.EvidenceSummary = issueHandoffEvidenceSummary(detail, sourceSessionID, sourceCommitSHA)
	}

	commits, head, err := a.issueHandoffCommits(detail.Project, branch, sourceCommitSHA)
	if err != nil {
		handoff.Error = err.Error()
	} else {
		handoff.Commits = commits
		handoff.HeadCommitSHA = head
	}
	if handoff.HeadCommitSHA == "" {
		handoff.HeadCommitSHA = sourceCommitSHA
	}
	if len(handoff.Commits) == 0 && sourceCommitSHA != "" {
		handoff.Commits = append(handoff.Commits, issueHandoffCommit{
			SHA:      sourceCommitSHA,
			ShortSHA: shortCommitSHA(sourceCommitSHA),
			Subject:  sourceNodeSubject(sourceNode),
		})
	}
	return handoff, nil
}

func (a *app) issueHandoffCommits(project project, branch, sourceCommitSHA string) ([]issueHandoffCommit, string, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, "", errors.New("git is not available on PATH")
	}
	if err := ensureGitRepository(gitPath, project.RepoPath); err != nil {
		return nil, "", err
	}
	branch = strings.TrimSpace(branch)
	if branch != "" {
		if err := validateGitBranchName(gitPath, project.RepoPath, branch); err != nil {
			return nil, "", err
		}
		exists, err := gitRefExists(gitPath, project.RepoPath, "refs/heads/"+branch)
		if err != nil {
			return nil, "", err
		}
		if exists {
			return gitCommitRangeForBranch(gitPath, project.RepoPath, project.DefaultBranch, branch)
		}
	}
	sourceCommitSHA = strings.TrimSpace(sourceCommitSHA)
	if sourceCommitSHA == "" {
		return []issueHandoffCommit{}, "", nil
	}
	if err := gitCommitExists(gitPath, project.RepoPath, sourceCommitSHA); err != nil {
		return nil, "", err
	}
	subject, _ := runGitReadOnly(gitPath, project.RepoPath, "log", "-1", "--pretty=%s", sourceCommitSHA)
	return []issueHandoffCommit{{SHA: sourceCommitSHA, ShortSHA: shortCommitSHA(sourceCommitSHA), Subject: subject}}, sourceCommitSHA, nil
}

func (a *app) createPullRequestForHandoff(detail issueDetail, handoff *issueHandoff, title string, draft bool) error {
	if handoff == nil {
		return errors.New("handoff is required")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return errors.New("git is not available on PATH")
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return errors.New("GitHub CLI is not available on PATH")
	}
	gitleaksPath, err := exec.LookPath("gitleaks")
	if err != nil {
		return errors.New("gitleaks is not available on PATH")
	}
	branch := strings.TrimSpace(handoff.Branch)
	if branch == "" {
		return errors.New("source branch is required to create a pull request")
	}
	if err := validateGitBranchName(gitPath, detail.Project.RepoPath, branch); err != nil {
		return err
	}
	exists, err := gitRefExists(gitPath, detail.Project.RepoPath, "refs/heads/"+branch)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("source branch %q does not exist in the project repository", branch)
	}
	commits, head, err := gitCommitRangeForBranch(gitPath, detail.Project.RepoPath, detail.Project.DefaultBranch, branch)
	if err != nil {
		return err
	}
	handoff.Commits = commits
	handoff.HeadCommitSHA = head
	if len(commits) == 0 {
		return fmt.Errorf("source branch %q has no commits ahead of %s", branch, valueOrUnset(detail.Project.DefaultBranch))
	}
	if title == "" {
		title = firstNonEmpty(handoff.PRTitle, commits[0].Subject, detail.Issue.Title)
	}
	handoff.PRTitle = title
	handoff.EvidenceSummary = firstNonEmpty(handoff.EvidenceSummary, issueHandoffEvidenceSummary(detail, handoff.SourceSessionID, handoff.SourceCommitSHA))
	handoff.PreviewURL = firstNonEmpty(handoff.PreviewURL, issueHandoffPreviewURL(detail, handoff.SourceSessionID, handoff.SourceCommitSHA))

	if err := runGitleaksHandoffScan(gitleaksPath, gitPath, detail.Project.RepoPath, detail.Project.DefaultBranch, branch); err != nil {
		return err
	}

	if err := refreshIssueHandoffPRStatus(detail.Project, handoff); err == nil && strings.TrimSpace(handoff.PRURL) != "" {
		handoff.Kind = "pr"
		handoff.CreatedVia = "gh"
		return nil
	}

	output, err := runCommandWithTimeout(2*time.Minute, gitPath, []string{"-C", detail.Project.RepoPath, "push", "-u", "origin", "refs/heads/" + branch + ":refs/heads/" + branch}, []string{"GIT_TERMINAL_PROMPT=0"})
	if err != nil {
		return fmt.Errorf("push branch %q: %s", branch, truncate(formatCommandFailure(err, output), 1200))
	}

	repoSlug, err := githubRepoSlug(detail.Project)
	if err != nil {
		return err
	}
	baseBranch := strings.TrimSpace(detail.Project.DefaultBranch)
	if baseBranch == "" {
		baseBranch = "main"
	}
	body := buildIssuePullRequestBody(detail, *handoff)
	args := []string{"pr", "create", "--repo", repoSlug, "--base", baseBranch, "--head", branch, "--title", title, "--body", body}
	if draft {
		args = append(args, "--draft")
	}
	output, err = runCommandWithTimeout(2*time.Minute, ghPath, args, []string{"GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0"})
	if err != nil {
		return fmt.Errorf("create pull request: %s", truncate(formatCommandFailure(err, output), 1200))
	}
	prURL := firstHTTPURL(string(output))
	if prURL == "" {
		return fmt.Errorf("create pull request returned no URL: %s", truncate(strings.TrimSpace(string(output)), 600))
	}
	handoff.PRURL = prURL
	handoff.PRNumber = parsePullRequestNumber(prURL)
	handoff.Kind = "pr"
	handoff.CreatedVia = "gh"
	handoff.Error = ""
	if err := refreshIssueHandoffPRStatus(detail.Project, handoff); err != nil {
		handoff.Error = err.Error()
	}
	return nil
}

func normalizePullRequestURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("pull request URL must be an absolute GitHub URL")
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Host), "www.")
	if host != "github.com" {
		return "", errors.New("pull request URL must point to github.com")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" || parseIntOrZero(parts[3]) == 0 {
		return "", errors.New("pull request URL must look like https://github.com/<owner>/<repo>/pull/<number>")
	}
	parsed.Fragment = ""
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func parsePullRequestNumber(value string) int {
	matches := pullRequestURLPattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return 0
	}
	return parseIntOrZero(matches[1])
}

func refreshIssueHandoffPRStatus(project project, handoff *issueHandoff) error {
	if handoff == nil {
		return nil
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return errors.New("GitHub CLI is not available on PATH")
	}
	selector := strings.TrimSpace(handoff.PRURL)
	if selector == "" {
		selector = strings.TrimSpace(handoff.Branch)
	}
	if selector == "" {
		return errors.New("handoff has no pull request URL or source branch")
	}
	args := []string{"pr", "view", selector, "--json", "url,number,state,title,isDraft"}
	if repoSlug, err := githubRepoSlug(project); err == nil && repoSlug != "" {
		args = append(args, "--repo", repoSlug)
	}
	output, err := runCommandWithTimeout(45*time.Second, ghPath, args, []string{"GH_PROMPT_DISABLED=1"})
	handoff.LastCheckedAt = nowString()
	if err != nil {
		if handoff.PRNumber == 0 {
			handoff.PRNumber = parsePullRequestNumber(handoff.PRURL)
		}
		return fmt.Errorf("refresh pull request status: %s", truncate(formatCommandFailure(err, output), 600))
	}
	var payload struct {
		URL     string `json:"url"`
		Number  int    `json:"number"`
		State   string `json:"state"`
		Title   string `json:"title"`
		IsDraft bool   `json:"isDraft"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return fmt.Errorf("parse pull request status: %w", err)
	}
	if strings.TrimSpace(payload.URL) != "" {
		handoff.PRURL = strings.TrimSpace(payload.URL)
	}
	if payload.Number > 0 {
		handoff.PRNumber = payload.Number
	}
	if strings.TrimSpace(payload.Title) != "" {
		handoff.PRTitle = strings.TrimSpace(payload.Title)
	}
	state := strings.ToLower(strings.TrimSpace(payload.State))
	if payload.IsDraft && state == "open" {
		state = "draft"
	}
	handoff.PRState = state
	handoff.Error = ""
	return nil
}

func gitCommitRangeForBranch(gitPath, repoPath, defaultBranch, branch string) ([]issueHandoffCommit, string, error) {
	head, err := runGitReadOnly(gitPath, repoPath, "rev-parse", branch)
	if err != nil {
		return nil, "", err
	}
	baseRef, err := resolveBaseRef(gitPath, repoPath, defaultBranch)
	if err != nil {
		return nil, "", err
	}
	mergeBase, err := runGitReadOnly(gitPath, repoPath, "merge-base", branch, baseRef)
	if err != nil {
		return nil, "", err
	}
	output, err := runGitReadOnly(gitPath, repoPath, "log", "--reverse", "--pretty=%H%x00%s", mergeBase+".."+branch)
	if err != nil {
		return nil, "", err
	}
	commits := []issueHandoffCommit{}
	for _, line := range splitNonEmptyLines(output) {
		parts := strings.SplitN(line, "\x00", 2)
		sha := strings.TrimSpace(parts[0])
		if sha == "" {
			continue
		}
		subject := ""
		if len(parts) == 2 {
			subject = strings.TrimSpace(parts[1])
		}
		commits = append(commits, issueHandoffCommit{SHA: sha, ShortSHA: shortCommitSHA(sha), Subject: subject})
	}
	return commits, strings.TrimSpace(head), nil
}

func validateGitBranchName(gitPath, repoPath, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("branch is required")
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	output, err := exec.Command(gitPath, "-C", repoPath, "check-ref-format", "--branch", branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("invalid branch name %q: %s", branch, formatCommandFailure(err, output))
	}
	return nil
}

func runGitleaksHandoffScan(gitleaksPath, gitPath, repoPath, defaultBranch, branch string) error {
	baseRef, err := resolveBaseRef(gitPath, repoPath, defaultBranch)
	if err != nil {
		return err
	}
	mergeBase, err := runGitReadOnly(gitPath, repoPath, "merge-base", branch, baseRef)
	if err != nil {
		return err
	}
	output, err := runCommandWithTimeout(2*time.Minute, gitleaksPath, []string{"git", "--no-banner", "--redact", "--log-opts", mergeBase + ".." + branch, repoPath}, nil)
	if err != nil {
		return fmt.Errorf("gitleaks preflight failed: %s", truncate(formatCommandFailure(err, output), 1200))
	}
	return nil
}

func runCommandWithTimeout(timeout time.Duration, name string, args []string, extraEnv []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	if len(extraEnv) > 0 {
		command.Env = append(os.Environ(), extraEnv...)
	}
	output, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("%s timed out", filepath.Base(name))
	}
	return output, err
}

func githubRepoSlug(project project) (string, error) {
	if strings.EqualFold(project.GitProvider, "github") && strings.TrimSpace(project.GitOwner) != "" && strings.TrimSpace(project.GitRepo) != "" {
		return project.GitOwner + "/" + project.GitRepo, nil
	}
	if info, ok := parseGitRemoteInfo(project.RemoteURL); ok && info.Provider == "github" {
		return info.Owner + "/" + info.Repo, nil
	}
	return "", errors.New("project does not have GitHub repository metadata")
}

func firstHTTPURL(value string) string {
	for _, field := range strings.Fields(value) {
		field = strings.Trim(field, " \t\r\n<>\"'")
		if strings.HasPrefix(field, "https://") || strings.HasPrefix(field, "http://") {
			return field
		}
	}
	return ""
}

func sourceNodeSubject(node *issueChangeNode) string {
	if node == nil {
		return ""
	}
	return node.Subject
}

func issueHandoffPreviewURL(detail issueDetail, sourceSessionID, sourceCommitSHA string) string {
	if review := matchingReviewEvidence(detail, sourceSessionID, sourceCommitSHA); review != nil && strings.TrimSpace(review.PreviewURL) != "" {
		return strings.TrimSpace(review.PreviewURL)
	}
	if detail.TestEnvironment != nil {
		environment := detail.TestEnvironment
		sourceMatches := sourceCommitSHA == "" || environment.SourceCommitSHA == sourceCommitSHA || strings.HasPrefix(environment.SourceCommitSHA, sourceCommitSHA)
		sessionMatches := sourceSessionID == "" || environment.SourceSessionID == sourceSessionID
		if sourceMatches && sessionMatches && strings.TrimSpace(environment.PreviewURL) != "" {
			return strings.TrimSpace(environment.PreviewURL)
		}
	}
	for _, review := range detail.ReviewEvidence {
		if strings.TrimSpace(review.PreviewURL) != "" {
			return strings.TrimSpace(review.PreviewURL)
		}
	}
	return ""
}

func issueHandoffEvidenceSummary(detail issueDetail, sourceSessionID, sourceCommitSHA string) string {
	review := matchingReviewEvidence(detail, sourceSessionID, sourceCommitSHA)
	if review == nil {
		if detail.TestEnvironment != nil && strings.TrimSpace(detail.TestEnvironment.PreviewURL) != "" {
			return "Preview URL recorded: " + strings.TrimSpace(detail.TestEnvironment.PreviewURL)
		}
		return "No review evidence has been captured for this handoff yet."
	}
	lines := []string{}
	if review.AgentSummary != "" {
		lines = append(lines, "Agent summary: "+review.AgentSummary)
	}
	if tests := reviewChecksSummary(review.Tests); tests != "" {
		lines = append(lines, "Tests: "+tests)
	}
	if build := reviewResultSummary(review.BuildResult); build != "" {
		lines = append(lines, "Build: "+build)
	}
	if deploy := reviewResultSummary(review.DeploymentResult); deploy != "" {
		lines = append(lines, "Deployment: "+deploy)
	}
	if review.PreviewURL != "" {
		lines = append(lines, "Preview URL: "+review.PreviewURL)
	}
	if len(review.Risks) > 0 {
		lines = append(lines, "Risks: "+strings.Join(review.Risks, "; "))
	}
	if len(review.FollowUps) > 0 {
		lines = append(lines, "Follow-ups: "+strings.Join(review.FollowUps, "; "))
	}
	if len(lines) == 0 {
		return "Review evidence exists, but no summary fields were reported."
	}
	return truncate(strings.Join(lines, "\n"), 3000)
}

func matchingReviewEvidence(detail issueDetail, sourceSessionID, sourceCommitSHA string) *sessionReviewEvidence {
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	sourceCommitSHA = strings.TrimSpace(sourceCommitSHA)
	for index := range detail.ReviewEvidence {
		review := &detail.ReviewEvidence[index]
		reviewCommit := strings.TrimSpace(review.SourceCommitSHA)
		commitMatches := sourceCommitSHA != "" && reviewCommit != "" && (reviewCommit == sourceCommitSHA || strings.HasPrefix(reviewCommit, sourceCommitSHA) || strings.HasPrefix(sourceCommitSHA, reviewCommit))
		sessionMatches := sourceSessionID != "" && (review.SourceSessionID == sourceSessionID || review.SessionID == sourceSessionID)
		if commitMatches || sessionMatches {
			return review
		}
	}
	return nil
}

func reviewChecksSummary(checks []reviewEvidenceCheck) string {
	if len(checks) == 0 {
		return ""
	}
	values := make([]string, 0, len(checks))
	for _, check := range checks {
		name := firstNonEmpty(check.Name, "check")
		status := firstNonEmpty(check.Status, "reported")
		summary := strings.TrimSpace(check.Summary)
		if summary != "" {
			values = append(values, fmt.Sprintf("%s %s - %s", name, status, summary))
		} else {
			values = append(values, fmt.Sprintf("%s %s", name, status))
		}
	}
	return strings.Join(values, "; ")
}

func reviewResultSummary(result reviewEvidenceResult) string {
	status := strings.TrimSpace(result.Status)
	summary := strings.TrimSpace(result.Summary)
	if status == "" && summary == "" {
		return ""
	}
	if status == "" {
		return summary
	}
	if summary == "" {
		return status
	}
	return status + " - " + summary
}

func buildIssuePullRequestBody(detail issueDetail, handoff issueHandoff) string {
	var builder strings.Builder
	builder.WriteString("## mspace Issue\n\n")
	builder.WriteString(fmt.Sprintf("- Issue: `%s`\n", detail.Issue.ID))
	builder.WriteString(fmt.Sprintf("- Title: %s\n", detail.Issue.Title))
	builder.WriteString(fmt.Sprintf("- Project: %s\n", detail.Project.Name))
	builder.WriteString("\n## Source\n\n")
	builder.WriteString(fmt.Sprintf("- Branch: `%s`\n", valueOrUnset(handoff.Branch)))
	builder.WriteString(fmt.Sprintf("- Source session: `%s`\n", valueOrUnset(handoff.SourceSessionID)))
	builder.WriteString(fmt.Sprintf("- Source commit: `%s`\n", valueOrUnset(handoff.SourceCommitSHA)))
	if len(handoff.Commits) > 0 {
		builder.WriteString("- Commits:\n")
		for _, commit := range handoff.Commits {
			builder.WriteString(fmt.Sprintf("  - `%s` %s\n", firstNonEmpty(commit.ShortSHA, shortCommitSHA(commit.SHA)), commit.Subject))
		}
	}
	builder.WriteString("\n## Preview\n\n")
	if handoff.PreviewURL != "" {
		builder.WriteString(fmt.Sprintf("- %s\n", handoff.PreviewURL))
	} else {
		builder.WriteString("- Not recorded.\n")
	}
	builder.WriteString("\n## Evidence\n\n")
	builder.WriteString(strings.TrimSpace(firstNonEmpty(handoff.EvidenceSummary, "No review evidence has been captured for this handoff yet.")))
	builder.WriteString("\n")
	return builder.String()
}

func issueHandoffComment(handoff issueHandoff) string {
	if strings.TrimSpace(handoff.PRURL) != "" {
		state := strings.TrimSpace(handoff.PRState)
		if state == "" {
			state = "recorded"
		}
		label := "PR"
		if handoff.PRNumber > 0 {
			label = fmt.Sprintf("PR #%d", handoff.PRNumber)
		}
		return fmt.Sprintf("PR handoff [%s](%s) is %s.\n\nBranch: `%s`\nSource commit: `%s`", label, handoff.PRURL, state, valueOrUnset(handoff.Branch), valueOrUnset(shortCommitSHA(handoff.SourceCommitSHA)))
	}
	return fmt.Sprintf("Branch handoff recorded for `%s`.\n\nSource commit: `%s`", valueOrUnset(handoff.Branch), valueOrUnset(shortCommitSHA(handoff.SourceCommitSHA)))
}

func probeSessionForIssue(detail issueDetail) *agentSession {
	lastDeploySessionID := ""
	if detail.TestEnvironment != nil {
		lastDeploySessionID = strings.TrimSpace(detail.TestEnvironment.LastDeploySessionID)
	}
	for i := range detail.Sessions {
		if readTestEnvironmentPreviewURL(detail.Sessions[i]) != "" {
			return &detail.Sessions[i]
		}
		if lastDeploySessionID != "" && detail.Sessions[i].ID == lastDeploySessionID {
			return &detail.Sessions[i]
		}
	}
	return nil
}

func (a *app) queueAgentSession(issueID string, input sessionRequest, actor issueActor) (agentSession, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.AgentProfile = strings.TrimSpace(input.AgentProfile)
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	input.Command = strings.TrimSpace(input.Command)
	input.Branch = strings.TrimSpace(input.Branch)
	input.SourceSessionID = strings.TrimSpace(input.SourceSessionID)
	input.SourceCommitSHA = strings.TrimSpace(input.SourceCommitSHA)
	input.TriggerCommentID = strings.TrimSpace(input.TriggerCommentID)
	if input.RuntimeMode == "" {
		input.RuntimeMode = "local"
	}
	if input.RuntimeMode != "local" && input.RuntimeMode != "team" {
		return agentSession{}, errInvalidRuntimeMode
	}
	if input.Provider != "" && !strings.EqualFold(input.Provider, "codex") && input.AgentProfile == "" {
		if profile, err := a.resolveEnabledAgentProfile(input.Provider); err == nil {
			input.AgentProfile = profile.ID
			input.Provider = "codex"
		}
	}
	if input.Provider == "" {
		input.Provider = "codex"
	}
	profile := agentProfile{}
	if isCodexProvider(input.Provider) {
		var err error
		profile, err = a.resolveEnabledAgentProfile(input.AgentProfile)
		if err != nil {
			return agentSession{}, err
		}
		input.Provider = "codex"
		input.AgentProfile = profile.ID
	}

	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		return agentSession{}, err
	}

	command := input.Command
	if !isCodexProvider(input.Provider) {
		command = buildSessionCommand(issueID, detail.Project, input.Command)
	}
	sessionID := uuid.NewString()
	agentToken := agentTokenPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	branch := input.Branch
	if branch == "" {
		branch = fmt.Sprintf("mspace/%s/%s", shortID(issueID), shortID(sessionID))
	}
	workdir := plannedSessionWorkdir(a.workdir, detail.Project.ID, sessionID)

	session := agentSession{
		ID:               sessionID,
		IssueID:          issueID,
		Provider:         input.Provider,
		AgentProfile:     input.AgentProfile,
		RuntimeMode:      input.RuntimeMode,
		Command:          command,
		Status:           "queued",
		Branch:           branch,
		Workdir:          workdir,
		AgentStatus:      "queued",
		SourceSessionID:  input.SourceSessionID,
		SourceCommitSHA:  input.SourceCommitSHA,
		TriggerCommentID: input.TriggerCommentID,
		AgentToken:       agentToken,
		CleanupStatus:    "retained",
		CreatedAt:        nowString(),
		UpdatedAt:        nowString(),
	}

	if _, err := a.db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, runtime_task_id, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, source_session_id, source_commit_sha, trigger_comment_id, agent_token, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, session.ID, session.IssueID, session.Provider, session.AgentProfile, session.RuntimeMode, session.RuntimeTaskID, session.Command, session.Status, session.Branch, session.Workdir, session.CodexThreadID, session.CodexTurnID, session.AgentStatus, session.ArtifactDir, session.SourceSessionID, session.SourceCommitSHA, session.TriggerCommentID, session.AgentToken, session.CleanupStatus, session.CleanedAt, session.CreatedAt, session.UpdatedAt); err != nil {
		return agentSession{}, err
	}

	assignee := input.Provider
	if profile.ID != "" {
		assignee = profile.ID
	}
	displayAgent := input.Provider
	if profile.Name != "" {
		displayAgent = profile.Name
	}
	modeLabel := "local"
	if session.RuntimeMode == "team" {
		modeLabel = "team runtime"
	}
	if err := a.updateIssueAssignment(issueID, assignee, "agent", normalizeIssueStatus(detail.Issue.Status), actor, fmt.Sprintf("Assigned to agent `%s` and queued %s session `%s`.", displayAgent, modeLabel, shortID(session.ID))); err != nil {
		return agentSession{}, err
	}
	a.addSystemComment(issueID, fmt.Sprintf(
		"Assigned to agent `%s` and queued %s session `%s` on branch `%s`.\n\nPlanned workspace: `%s`",
		displayAgent,
		modeLabel,
		shortID(session.ID),
		session.Branch,
		session.Workdir,
	))

	go a.runSession(session, detail.Project)
	return session, nil
}

func (a *app) queueServerIssueTeamSession(input serverIssueSessionRequest, actor issueActor) (agentSession, error) {
	if err := a.requireTeamControlPlaneWorkspace(context.Background()); err != nil {
		return agentSession{}, err
	}
	if input.Provider == "" {
		input.Provider = "codex"
	}
	profile := agentProfile{}
	if isCodexProvider(input.Provider) {
		var err error
		profile, err = a.resolveEnabledAgentProfile(input.AgentProfile)
		if err != nil {
			return agentSession{}, err
		}
		input.Provider = "codex"
		input.AgentProfile = profile.ID
	}
	if input.RuntimeMode != "team" {
		return agentSession{}, errInvalidRuntimeMode
	}
	input.Issue.ID = strings.TrimSpace(firstNonEmpty(input.Issue.ID, input.IssueID))
	input.Issue.ProjectID = strings.TrimSpace(firstNonEmpty(input.Issue.ProjectID, input.Project.ID))
	input.Issue.Title = strings.TrimSpace(input.Issue.Title)
	input.Issue.Body = strings.TrimSpace(input.Issue.Body)
	input.Issue.Status = normalizeIssueStatus(input.Issue.Status)
	if input.Issue.Status == "" {
		input.Issue.Status = "open"
	}
	if input.Issue.Assignee == "" {
		input.Issue.Assignee = input.AgentProfile
		input.Issue.AssigneeType = "agent"
	}
	if input.Issue.AssigneeType == "" {
		input.Issue.AssigneeType = "agent"
	}
	if input.Project.ID == "" || input.Project.ID != input.Issue.ProjectID {
		input.Project.ID = input.Issue.ProjectID
	}
	input.Project.Name = strings.TrimSpace(input.Project.Name)
	if input.Project.Name == "" {
		input.Project.Name = "Server project"
	}
	input.Project.RepoPath = strings.TrimSpace(input.Project.RepoPath)
	input.Project.RemoteURL = strings.TrimSpace(input.Project.RemoteURL)
	if input.Project.RepoPath == "" {
		input.Project.RepoPath = input.Project.RemoteURL
	}
	if input.Project.DefaultBranch == "" {
		input.Project.DefaultBranch = "main"
	}
	if input.Project.SourceType == "" {
		if input.Project.RemoteURL != "" {
			input.Project.SourceType = "github"
		} else {
			input.Project.SourceType = "local"
		}
	}
	if input.Project.ID == "" || input.Issue.ID == "" || input.Issue.ProjectID == "" {
		return agentSession{}, errors.New("server issue bridge requires issue and project ids")
	}
	if remoteURLForTeamRuntime(input.Project) == "" {
		return agentSession{}, errors.New("team runtime requires a project remote URL or repository path")
	}
	if input.Issue.Title == "" {
		input.Issue.Title = "Server issue " + shortID(input.Issue.ID)
	}

	if err := a.upsertServerIssueSnapshot(input, actor); err != nil {
		return agentSession{}, err
	}

	sessionID := uuid.NewString()
	agentToken := agentTokenPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	branch := input.Branch
	if branch == "" {
		branch = fmt.Sprintf("mspace/%s/%s", shortID(input.Issue.ID), shortID(sessionID))
	}
	session := agentSession{
		ID:               sessionID,
		IssueID:          input.Issue.ID,
		Provider:         input.Provider,
		AgentProfile:     input.AgentProfile,
		RuntimeMode:      "team",
		Command:          input.Command,
		Status:           "queued",
		Branch:           branch,
		Workdir:          plannedSessionWorkdir(a.workdir, input.Project.ID, sessionID),
		AgentStatus:      "queued",
		SourceSessionID:  input.SourceSessionID,
		SourceCommitSHA:  input.SourceCommitSHA,
		TriggerCommentID: input.CommentID,
		AgentToken:       agentToken,
		CleanupStatus:    "retained",
		CreatedAt:        nowString(),
		UpdatedAt:        nowString(),
	}
	if _, err := a.db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, runtime_task_id, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, source_session_id, source_commit_sha, trigger_comment_id, agent_token, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, session.ID, session.IssueID, session.Provider, session.AgentProfile, session.RuntimeMode, session.RuntimeTaskID, session.Command, session.Status, session.Branch, session.Workdir, session.CodexThreadID, session.CodexTurnID, session.AgentStatus, session.ArtifactDir, session.SourceSessionID, session.SourceCommitSHA, session.TriggerCommentID, session.AgentToken, session.CleanupStatus, session.CleanedAt, session.CreatedAt, session.UpdatedAt); err != nil {
		return agentSession{}, err
	}

	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Bridge accepted server-owned issue %s from workspace %s.", shortID(input.Issue.ID), input.WorkspaceID))
	go a.runSession(session, input.Project)
	return session, nil
}

func (a *app) upsertServerIssueSnapshot(input serverIssueSessionRequest, actor issueActor) error {
	now := nowString()
	if input.Project.CreatedAt == "" {
		input.Project.CreatedAt = now
	}
	input.Project.UpdatedAt = now
	if input.Issue.CreatedAt == "" {
		input.Issue.CreatedAt = now
	}
	input.Issue.UpdatedAt = now
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, kubeconfig_path, namespace, image_registry_prefix, preview_domain, ingress_class, node_host, default_cluster_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			repo_path = excluded.repo_path,
			source_type = excluded.source_type,
			remote_url = excluded.remote_url,
			git_provider = excluded.git_provider,
			git_owner = excluded.git_owner,
			git_repo = excluded.git_repo,
			default_branch = excluded.default_branch,
			deploy_command = excluded.deploy_command,
			validation_command = excluded.validation_command,
			kube_context = excluded.kube_context,
			kubeconfig_path = excluded.kubeconfig_path,
			namespace = excluded.namespace,
			image_registry_prefix = excluded.image_registry_prefix,
			preview_domain = excluded.preview_domain,
			ingress_class = excluded.ingress_class,
			node_host = excluded.node_host,
			default_cluster_id = excluded.default_cluster_id,
			updated_at = excluded.updated_at
	`, input.Project.ID, input.Project.Name, input.Project.RepoPath, input.Project.SourceType, input.Project.RemoteURL, input.Project.GitProvider, input.Project.GitOwner, input.Project.GitRepo, input.Project.DefaultBranch, input.Project.DeployCommand, input.Project.ValidationCommand, input.Project.KubeContext, input.Project.KubeconfigPath, input.Project.Namespace, input.Project.ImageRegistryPrefix, input.Project.PreviewDomain, input.Project.IngressClass, input.Project.NodeHost, input.Project.DefaultClusterID, input.Project.CreatedAt, input.Project.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO project_runbooks (project_id, content, status, source, source_session_id, content_hash, created_at, updated_at)
		VALUES (?, '', 'empty', '', '', '', ?, ?)
		ON CONFLICT(project_id) DO NOTHING
	`, input.Project.ID, now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, close_reason, triage_status, assignee, assignee_type, creator_name, creator_avatar_url, environment_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			project_id = excluded.project_id,
			parent_issue_id = excluded.parent_issue_id,
			sort_order = excluded.sort_order,
			title = excluded.title,
			body = excluded.body,
			status = excluded.status,
			close_reason = excluded.close_reason,
			triage_status = excluded.triage_status,
			assignee = excluded.assignee,
			assignee_type = excluded.assignee_type,
			creator_name = excluded.creator_name,
			creator_avatar_url = excluded.creator_avatar_url,
			environment_url = excluded.environment_url,
			updated_at = excluded.updated_at
	`, input.Issue.ID, input.Issue.ProjectID, nullableString(input.Issue.ParentIssueID), input.Issue.SortOrder, input.Issue.Title, input.Issue.Body, input.Issue.Status, input.Issue.CloseReason, input.Issue.TriageStatus, input.Issue.Assignee, input.Issue.AssigneeType, input.Issue.CreatorName, input.Issue.CreatorAvatar, input.Issue.EnvironmentURL, input.Issue.CreatedAt, input.Issue.UpdatedAt); err != nil {
		return err
	}
	for _, child := range input.ChildIssues {
		child.ID = strings.TrimSpace(child.ID)
		if child.ID == "" {
			continue
		}
		child.ProjectID = firstNonEmpty(child.ProjectID, input.Project.ID)
		child.ParentIssueID = firstNonEmpty(child.ParentIssueID, input.Issue.ID)
		child.Status = normalizeIssueStatus(child.Status)
		if child.Status == "" {
			child.Status = "open"
		}
		if child.CreatedAt == "" {
			child.CreatedAt = now
		}
		child.UpdatedAt = now
		if _, err := tx.Exec(`
			INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, close_reason, triage_status, assignee, assignee_type, creator_name, creator_avatar_url, environment_url, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				project_id = excluded.project_id,
				parent_issue_id = excluded.parent_issue_id,
				sort_order = excluded.sort_order,
				title = excluded.title,
				body = excluded.body,
				status = excluded.status,
				triage_status = excluded.triage_status,
				assignee = excluded.assignee,
				assignee_type = excluded.assignee_type,
				updated_at = excluded.updated_at
		`, child.ID, child.ProjectID, nullableString(child.ParentIssueID), child.SortOrder, child.Title, child.Body, child.Status, child.CloseReason, child.TriageStatus, child.Assignee, child.AssigneeType, input.Issue.CreatorName, input.Issue.CreatorAvatar, child.CreatedAt, child.UpdatedAt); err != nil {
			return err
		}
	}
	if input.CommentID != "" {
		commentBody := input.Command
		for _, c := range input.Comments {
			if c.ID == input.CommentID {
				commentBody = c.Body
				break
			}
		}
		if _, err := tx.Exec(`
			INSERT INTO comments (id, issue_id, author_type, author_user_id, author_name, author_avatar_url, body, created_at, updated_at)
			VALUES (?, ?, 'human', ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				body = excluded.body,
				updated_at = excluded.updated_at
		`, input.CommentID, input.Issue.ID, actor.UserID, commentActorName(actor), actor.AvatarURL, commentBody, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func (a *app) issueHasActiveSession(issueID string) bool {
	var count int
	if err := a.db.QueryRow(`
		SELECT COUNT(*)
		FROM agent_sessions
		WHERE issue_id = ? AND status IN ('queued', 'running')
	`, issueID).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (a *app) buildIssueTestEnvironment(detail issueDetail, input testDeployRequest) (issueTestEnvironment, error) {
	environment := issueTestEnvironment{}
	if detail.TestEnvironment != nil {
		environment = *detail.TestEnvironment
	}
	environment.IssueID = detail.Issue.ID
	if strings.TrimSpace(environment.Namespace) == "" {
		environment.Namespace = defaultIssueNamespace(detail)
	}
	clusterID := firstNonEmpty(input.ClusterID, environment.ClusterID, detail.Project.DefaultClusterID)
	if clusterID == "" {
		return environment, errors.New("cluster is required before starting a test deployment")
	}
	selectedCluster, err := a.loadCluster(clusterID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return environment, errClusterNotFound
		}
		return environment, err
	}
	exposureMode, err := normalizeExposureMode(input.ExposureMode)
	if err != nil {
		return environment, err
	}
	if exposureMode == "" {
		exposureMode = selectedCluster.ExposureMode
	}
	if exposureMode == "" {
		exposureMode = "nodeport"
	}

	environment.NamespaceStatus = "planned"
	environment.CleanupStatus = "retained"
	environment.ClusterID = selectedCluster.ID
	environment.KubeconfigPath = selectedCluster.KubeconfigPath
	environment.KubeContext = selectedCluster.KubeContext
	environment.ImageRegistryPrefix = selectedCluster.ImageRegistryPrefix
	environment.ExposureMode = exposureMode
	environment.NodeHost = firstNonEmpty(input.NodeHost, selectedCluster.NodeHost)
	if exposureMode == "ingress" {
		environment.PreviewDomain = firstNonEmpty(input.PreviewDomain, selectedCluster.PreviewDomain)
		environment.IngressClass = firstNonEmpty(input.IngressClass, selectedCluster.IngressClass)
	} else {
		environment.PreviewDomain = ""
		environment.IngressClass = ""
	}

	if environment.KubeconfigPath == "" {
		return environment, errors.New("selected cluster needs a kubeconfig path before starting a test deployment")
	}
	if environment.ImageRegistryPrefix == "" {
		return environment, errors.New("selected cluster needs an image registry prefix before starting a test deployment")
	}
	if environment.ExposureMode == "ingress" && environment.PreviewDomain == "" {
		return environment, errors.New("preview domain is required when ingress exposure is selected")
	}
	return environment, nil
}

func defaultIssueNamespace(detail issueDetail) string {
	return dnsLabel("mspace-" + detail.Project.Name + "-" + shortID(detail.Issue.ID))
}

func dnsLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen && builder.Len() > 0 {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		result = "mspace-issue"
	}
	if len(result) > 63 {
		result = strings.TrimRight(result[:63], "-")
	}
	if result == "" {
		return "mspace-issue"
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func issueAttachmentURL(attachmentID string) string {
	return "/api/attachments/" + url.PathEscape(attachmentID)
}

func normalizeIssueAttachmentContentType(value string, content []byte) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	if isAllowedIssueAttachmentContentType(value) {
		return value
	}
	detected := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(content), ";")[0]))
	if isAllowedIssueAttachmentContentType(detected) {
		return detected
	}
	return value
}

func isAllowedIssueAttachmentContentType(value string) bool {
	switch value {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func normalizeIssueAttachmentFilename(value, contentType string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	filename := filepath.Base(value)
	if filename == "." || filename == "/" {
		filename = ""
	}
	filename = strings.TrimSpace(filename)
	if filename != "" {
		return filename
	}
	switch contentType {
	case "image/png":
		return "image.png"
	case "image/jpeg":
		return "image.jpg"
	case "image/gif":
		return "image.gif"
	case "image/webp":
		return "image.webp"
	default:
		return "image"
	}
}

func normalizeIssueAttachmentIDs(values []string) ([]string, error) {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	if len(result) > 50 {
		return nil, errors.New("too many attachments")
	}
	return result, nil
}

func bindIssueAttachmentsTx(tx *sql.Tx, attachmentIDs []string, issueID, commentID, now string) error {
	attachmentIDs, err := normalizeIssueAttachmentIDs(attachmentIDs)
	if err != nil {
		return err
	}
	if len(attachmentIDs) == 0 {
		return nil
	}
	issueID = strings.TrimSpace(issueID)
	commentID = strings.TrimSpace(commentID)
	if issueID == "" {
		return errors.New("attachment issue id is required")
	}
	var commentValue any
	if commentID != "" {
		commentValue = commentID
	}
	for _, attachmentID := range attachmentIDs {
		result, err := tx.Exec(`
			UPDATE issue_attachments
			SET issue_id = ?, comment_id = ?, updated_at = ?, bound_at = CASE WHEN bound_at = '' THEN ? ELSE bound_at END
			WHERE id = ? AND (issue_id IS NULL OR issue_id = '' OR issue_id = ?)
		`, issueID, commentValue, now, now, attachmentID, issueID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("attachment %s was not found or belongs to another issue", attachmentID)
		}
	}
	return nil
}

func buildIssueTestDeployPrompt(detail issueDetail, environment issueTestEnvironment, source issueChangeNode) string {
	var builder strings.Builder
	builder.WriteString("Deploy a test environment for this issue.\n\n")
	builder.WriteString("The user manually triggered this deployment after agent work, so do not create a PR unless explicitly asked in a separate turn.\n\n")
	builder.WriteString("Source code to deploy:\n")
	builder.WriteString(fmt.Sprintf("- Source commit: %s\n", source.CommitSHA))
	builder.WriteString(fmt.Sprintf("- Source session: %s\n", shortID(source.SessionID)))
	builder.WriteString(fmt.Sprintf("- Source branch: %s\n", valueOrUnset(source.Branch)))
	builder.WriteString(fmt.Sprintf("- Source subject: %s\n", valueOrUnset(source.Subject)))
	builder.WriteString("- The prepared session worktree is checked out at the source commit. Before building, verify `git rev-parse HEAD` matches the source commit.\n")
	builder.WriteString("- Build and deploy exactly this commit, not the latest branch tip or another session's worktree.\n\n")
	builder.WriteString("Deployment contract:\n")
	builder.WriteString(fmt.Sprintf("- Cluster ID: %s\n", environment.ClusterID))
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", environment.KubeconfigPath))
	if environment.KubeContext != "" {
		builder.WriteString(fmt.Sprintf("- Kube context: %s\n", environment.KubeContext))
	}
	builder.WriteString(fmt.Sprintf("- Issue namespace to create and manage: %s\n", environment.Namespace))
	builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", environment.ImageRegistryPrefix))
	builder.WriteString(fmt.Sprintf("- Project deploy command hint: %s\n", valueOrUnset(detail.Project.DeployCommand)))
	builder.WriteString(fmt.Sprintf("- Project validation command hint: %s\n", valueOrUnset(detail.Project.ValidationCommand)))
	builder.WriteString("- Build and push linux/amd64 images unless the issue explicitly requires another platform.\n")
	builder.WriteString("- Create the namespace yourself if it does not exist.\n")
	builder.WriteString("- Deploy or update only namespaced resources inside the issue namespace.\n")
	builder.WriteString("- Do not read Kubernetes Secrets.\n")
	builder.WriteString("- Return a team-accessible preview URL after deployment.\n")
	if environment.ExposureMode == "ingress" {
		builder.WriteString(fmt.Sprintf("- Preferred preview exposure: Ingress or HTTPRoute under domain %s.\n", environment.PreviewDomain))
		if environment.IngressClass != "" {
			builder.WriteString(fmt.Sprintf("- Preferred ingress class: %s.\n", environment.IngressClass))
		}
	} else {
		builder.WriteString("- Preferred preview exposure: Service type=NodePort.\n")
		if environment.NodeHost != "" {
			builder.WriteString(fmt.Sprintf("- Use this node host when forming the preview URL: %s.\n", environment.NodeHost))
		} else {
			builder.WriteString("- Discover a usable node ExternalIP, node hostname, or fallback node address for the NodePort URL.\n")
		}
	}
	builder.WriteString("\nRequired completion evidence:\n")
	builder.WriteString("- Image reference(s) pushed.\n")
	builder.WriteString("- Kubernetes resources applied.\n")
	builder.WriteString("- Pods, Services, Ingress/HTTPRoute if any, and recent Events inspected.\n")
	builder.WriteString("- Preview URL probed with curl or an equivalent HTTP check.\n")
	builder.WriteString("- Final answer includes the preview URL, namespace, image reference, validation result, and any blocker.\n")
	builder.WriteString("- Also write a JSON file to `${MSPACE_SESSION_ARTIFACT_DIR}/test-environment.json` with at least `previewUrl` when a URL is available.\n")
	builder.WriteString("- Also write `${MSPACE_SESSION_ARTIFACT_DIR}/review-evidence.json` with `commandsRun`, `tests`, `buildResult`, `deploymentResult`, `agentSummary`, `risks`, and `followUps`.\n")
	return builder.String()
}

func latestSourceSession(detail issueDetail) *agentSession {
	for _, session := range detail.Sessions {
		if session.Status == "queued" || session.Status == "running" {
			continue
		}
		if detail.TestEnvironment != nil {
			if session.ID == detail.TestEnvironment.LastDeploySessionID || session.ID == detail.TestEnvironment.LastCleanupSessionID {
				continue
			}
		}
		if isSystemTestEnvironmentCommand(session.Command) {
			continue
		}
		sessionCopy := session
		return &sessionCopy
	}
	return nil
}

func isSystemTestEnvironmentCommand(command string) bool {
	command = strings.TrimSpace(command)
	return strings.HasPrefix(command, "Deploy a test environment for this issue.") ||
		strings.HasPrefix(command, "Clean up the test environment for this issue.")
}

func buildIssueTestCleanupPrompt(detail issueDetail, environment issueTestEnvironment) string {
	var builder strings.Builder
	builder.WriteString("Clean up the test environment for this issue.\n\n")
	builder.WriteString("The user manually chose to remove this issue's test namespace. Use the provided kubeconfig and delete only this namespace and resources inside it.\n\n")
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", environment.KubeconfigPath))
	if environment.KubeContext != "" {
		builder.WriteString(fmt.Sprintf("- Kube context: %s\n", environment.KubeContext))
	}
	builder.WriteString(fmt.Sprintf("- Namespace to delete: %s\n", environment.Namespace))
	builder.WriteString(fmt.Sprintf("- Issue: %s\n", detail.Issue.Title))
	builder.WriteString("\nAfter cleanup, verify whether the namespace still exists and report the result. Do not delete any other namespace or cluster-scoped resource.\n")
	return builder.String()
}

func previewStrategyLabel(environment issueTestEnvironment) string {
	if environment.ExposureMode == "ingress" || environment.PreviewDomain != "" {
		if environment.IngressClass != "" {
			return fmt.Sprintf("Ingress `%s` on `%s`", environment.IngressClass, environment.PreviewDomain)
		}
		return fmt.Sprintf("Ingress on `%s`", environment.PreviewDomain)
	}
	if environment.NodeHost != "" {
		return fmt.Sprintf("NodePort via `%s`", environment.NodeHost)
	}
	return "NodePort with discovered node address"
}

func (a *app) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	detail, err := a.loadSessionDetail(sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, detail)
}

func (a *app) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	actor, ok := a.requireHumanActor(w, r)
	if !ok {
		return
	}
	detail, detailErr := a.loadSessionDetail(sessionID)
	if detailErr != nil {
		status := http.StatusInternalServerError
		if errors.Is(detailErr, sql.ErrNoRows) {
			status = http.StatusNotFound
			detailErr = errors.New("session not found")
		}
		writeError(w, status, detailErr)
		return
	}
	if detail.Session.RuntimeMode == "team" && strings.TrimSpace(detail.Session.RuntimeTaskID) != "" && (detail.Session.Status == "queued" || detail.Session.Status == "running") {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		err := a.cancelTeamRuntimeTask(ctx, detail.Session.RuntimeTaskID, "Stopped session "+shortID(sessionID)+" by "+commentActorName(actor)+".")
		cancel()
		if err != nil {
			a.appendSessionLog(sessionID, "system", "Unable to request team runtime cancellation: "+err.Error())
			writeError(w, http.StatusBadGateway, err)
			return
		}
		a.appendSessionLog(sessionID, "system", "Requested cancellation for team runtime task "+detail.Session.RuntimeTaskID+".")
	}
	a.mu.Lock()
	canceller := a.cancellers[sessionID]
	if canceller.cancel != nil {
		canceller.actor = actor
		a.cancellers[sessionID] = canceller
	}
	a.mu.Unlock()
	if canceller.cancel == nil {
		if detail.Session.Status == "queued" || detail.Session.Status == "running" {
			a.updateSessionStatus(sessionID, "cancelled")
			a.updateSessionAgentStatus(sessionID, "cancelled")
			if detail.Session.Status == "running" {
				if detail.Session.RuntimeMode == "team" && strings.TrimSpace(detail.Session.RuntimeTaskID) != "" {
					a.appendSessionLog(sessionID, "system", "Stop requested after the runner lost its in-memory wait handle. The matching team runtime task has been asked to cancel.")
				} else {
					a.appendSessionLog(sessionID, "system", "Stop requested after the runner lost its in-memory handle for this session. No live process was attached in the current runner.")
				}
			}
			a.markIssueTestEnvironmentSessionInterrupted(detail.Session)
			detail.Session.Status = "cancelled"
			detail.Session.AgentStatus = "cancelled"
			a.recordSessionFailure(detail.Session, detail.Project, errors.New("Stopped after the runner lost its in-memory handle for this session."), nil, "agent_interrupted", "stopped")
			a.addSystemComment(detail.Session.IssueID, fmt.Sprintf("Stopped session `%s` by %s.", shortID(sessionID), commentActorName(actor)))
			writeJSON(w, map[string]bool{"ok": true})
			return
		}
		writeError(w, http.StatusConflict, errors.New("session is not running"))
		return
	}
	canceller.cancel()
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleCleanupSessionWorktree(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	err := a.cleanupSessionWorktree(sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("session not found")
		} else if errors.Is(err, errSessionActive) {
			status = http.StatusConflict
		} else if errors.Is(err, errUnsafeSessionWorkdir) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleSessionStream(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	a.streamEvents(w, r, sessionID)
}

func (a *app) streamEvents(w http.ResponseWriter, r *http.Request, channelID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := a.broker.subscribe(channelID)
	defer unsubscribe()

	for {
		select {
		case payload, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (a *app) runSession(session agentSession, project project) {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancellers[session.ID] = sessionCanceller{cancel: cancel}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.cancellers, session.ID)
		a.mu.Unlock()
	}()
	if a.sessionWasCancelled(session.ID) {
		return
	}

	if session.RuntimeMode == "team" {
		a.runTeamRuntimeSession(ctx, session, project)
		return
	}

	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Preparing workspace for branch %s", session.Branch))
	preparedSession, err := a.prepareSessionWorkspace(session, project)
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("prepare workspace: %w", err))
		return
	}
	session = preparedSession
	contextPath, err := a.writeSessionContext(session, project)
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("write session context: %w", err))
		return
	}

	a.updateSessionStatus(session.ID, "running")
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Runner starting %s session in %s", session.Provider, session.Workdir))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Source repository: %s", project.RepoPath))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Session branch: %s", session.Branch))
	if strings.TrimSpace(session.AgentProfile) != "" {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Agent profile: %s", session.AgentProfile))
	}
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Session context: %s", contextPath))
	a.addSystemComment(session.IssueID, fmt.Sprintf(
		"Started local session `%s` on branch `%s`.\n\nWorkspace: `%s`\nContext: `%s`",
		shortID(session.ID),
		session.Branch,
		session.Workdir,
		contextPath,
	))

	if isCodexProvider(session.Provider) {
		if strings.TrimSpace(session.Command) != "" {
			a.appendSessionLog(session.ID, "system", fmt.Sprintf("Agent instructions: %s", session.Command))
		}
		err = a.runCodexAppServerSession(ctx, session, project, contextPath)
	} else {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Command: %s", session.Command))
		err = a.runShellSession(ctx, session, project, contextPath)
	}

	if ctx.Err() == context.Canceled || errors.Is(err, context.Canceled) {
		cancelActor := a.sessionCancelActor(session.ID)
		a.updateSessionStatus(session.ID, "cancelled")
		a.updateSessionAgentStatus(session.ID, "cancelled")
		session.Status = "cancelled"
		session.AgentStatus = "cancelled"
		a.appendSessionLog(session.ID, "system", "Session cancelled.")
		if a.isIssueTestDeploySession(session) {
			a.reconcileIssueTestEnvironmentForSession(session, project, "interrupted")
			a.recordSessionReviewEvidence(session, project, nil, false)
		} else {
			a.reconcileIssueTestEnvironmentForSession(session, project, "interrupted")
		}
		a.recordSessionFailure(session, project, errors.New("Session stopped by "+commentActorName(cancelActor)+"."), nil, "agent_interrupted", "stopped")
		a.addSystemComment(session.IssueID, fmt.Sprintf("Stopped session `%s` by %s.", shortID(session.ID), commentActorName(cancelActor)))
		return
	}
	if err != nil {
		a.failSession(session, &project, err)
		return
	}

	a.importProjectRunbookArtifact(session, project)

	changeNode, err := a.recordSourceChangeNode(session, project)
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("record source commit: %w", err))
		return
	}
	a.completeSuccessfulSession(session, project, changeNode)
}

func (a *app) runTeamRuntimeSession(ctx context.Context, session agentSession, project project) {
	if err := a.requireTeamControlPlaneWorkspace(ctx); err != nil {
		a.failSession(session, &project, err)
		return
	}
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Preparing team runtime context for branch %s", session.Branch))
	contextPath, err := a.writeSessionContext(session, project)
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("write session context: %w", err))
		return
	}
	artifactDir := filepath.Join(a.workdir, "_team_artifacts", session.ID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		a.failSession(session, &project, fmt.Errorf("create session artifact dir: %w", err))
		return
	}
	session.ArtifactDir = artifactDir
	a.updateSessionArtifactDir(session.ID, artifactDir)

	prompt := session.Command
	if isCodexProvider(session.Provider) {
		prompt, err = a.buildCodexSessionPrompt(session, project, contextPath, artifactDir)
		if err != nil {
			a.failSession(session, &project, fmt.Errorf("build codex prompt: %w", err))
			return
		}
	}

	a.updateSessionStatus(session.ID, "running")
	a.updateSessionAgentStatus(session.ID, "team-runtime-queued")
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Queueing team runtime task for %s session. The server worker will prepare its own workspace.", session.Provider))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Source repository: %s", remoteURLForTeamRuntime(project)))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Session branch: %s", session.Branch))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Session context: %s", contextPath))
	a.addSystemComment(session.IssueID, fmt.Sprintf(
		"Started team runtime session `%s` on branch `%s`.\n\nServer Worker will prepare an isolated workspace from `%s`.\nContext: `%s`",
		shortID(session.ID),
		session.Branch,
		remoteURLForTeamRuntime(project),
		contextPath,
	))

	task, err := a.createTeamRuntimeAgentTask(ctx, session, project, prompt)
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("queue team runtime task: %w", err))
		return
	}
	session.RuntimeTaskID = task.ID
	a.updateSessionRuntimeTaskID(session.ID, task.ID)
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Team runtime task queued: %s", task.ID))
	err = a.waitTeamRuntimeTask(ctx, session, task.ID)
	if ctx.Err() == context.Canceled || errors.Is(err, context.Canceled) {
		cancelActor := a.sessionCancelActor(session.ID)
		a.updateSessionStatus(session.ID, "cancelled")
		a.updateSessionAgentStatus(session.ID, "cancelled")
		session.Status = "cancelled"
		session.AgentStatus = "cancelled"
		a.appendSessionLog(session.ID, "system", "Team runtime session cancelled.")
		a.recordSessionFailure(session, project, errors.New("Session stopped by "+commentActorName(cancelActor)+"."), nil, "agent_interrupted", "stopped")
		a.addSystemComment(session.IssueID, fmt.Sprintf("Stopped session `%s` by %s.", shortID(session.ID), commentActorName(cancelActor)))
		return
	}
	if err != nil {
		a.failSession(session, &project, err)
		return
	}

	result, _ := a.loadTeamRuntimeTaskResult(ctx, task.ID)
	a.applyTeamRuntimeResult(session.ID, result)
	session = a.sessionWithTeamRuntimeResult(session, result)
	a.importProjectRunbookArtifact(session, project)
	changeNode, err := a.recordTeamRuntimeSourceChangeNode(session, project, result)
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("record source commit: %w", err))
		return
	}
	a.completeSuccessfulSession(session, project, changeNode)
}

func (a *app) completeSuccessfulSession(session agentSession, project project, changeNode *issueChangeNode) {
	a.updateSessionStatus(session.ID, "completed")
	a.updateSessionAgentStatus(session.ID, "completed")
	if changeNode != nil {
		a.updateIssueStatus(session.IssueID, "needs_review")
		a.addSystemComment(session.IssueID, fmt.Sprintf(
			"Session `%s` completed successfully and captured source commit `%s`.\n\nFiles changed: %d\nSubject: %s",
			shortID(session.ID),
			shortCommitSHA(changeNode.CommitSHA),
			changeNode.FilesChanged,
			changeNode.Subject,
		))
	} else {
		a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` completed successfully.", shortID(session.ID)))
	}
	if a.isIssueTestCleanupSession(session) {
		a.appendSessionLog(session.ID, "system", "Session completed. Updating test namespace cleanup state.")
		a.updateIssueTestEnvironmentForSession(session, true)
		a.updateReviewEvidenceCleanupStatus(session.ID, "cleaned")
		return
	}
	if a.isIssueTestDeploySession(session) {
		a.appendSessionLog(session.ID, "system", "Session completed. Reconciling test deployment state.")
		a.reconcileIssueTestEnvironmentForSession(session, project, "success")
		a.recordSessionReviewEvidence(session, project, changeNode, true)
		return
	}
	a.appendSessionLog(session.ID, "system", "Session completed. Collecting Kubernetes evidence.")
	a.collectEvidence(session, project)
	a.updateIssueTestEnvironmentForSession(session, true)
	a.recordSessionReviewEvidence(session, project, changeNode, true)
	a.maybeAutoCreateIssuePullRequest(session, project, changeNode)
}

func (a *app) createTeamRuntimeAgentTask(ctx context.Context, session agentSession, project project, prompt string) (runtimeTask, error) {
	contextPath := filepath.Join(a.workdir, "_contexts", session.ID+".md")
	contextMarkdown := ""
	if data, err := os.ReadFile(contextPath); err == nil {
		contextMarkdown = string(data)
	}
	repoURL := remoteURLForTeamRuntime(project)
	if strings.TrimSpace(repoURL) == "" {
		return runtimeTask{}, errors.New("team runtime requires a project remote URL or a repository path reachable from the worker")
	}
	payload := map[string]any{
		"prompt":                prompt,
		"developerInstructions": buildMspaceCodexDeveloperInstructions(),
		"approvalPolicy":        "never",
		"sandbox":               "danger-full-access",
		"env":                   envSliceToMap(a.buildSessionEnv(session, project, contextPath)),
		"issueId":               session.IssueID,
		"sessionId":             session.ID,
		"projectId":             project.ID,
		"branch":                session.Branch,
		"sourceCommitSha":       session.SourceCommitSHA,
		"contextMarkdown":       contextMarkdown,
		"artifactDir":           session.ArtifactDir,
		"agentProfile":          session.AgentProfile,
		"repository": map[string]any{
			"url":           repoURL,
			"defaultBranch": project.DefaultBranch,
			"sourceType":    project.SourceType,
			"provider":      project.GitProvider,
			"owner":         project.GitOwner,
			"repo":          project.GitRepo,
		},
	}
	body := map[string]any{
		"issueId":              session.IssueID,
		"sessionId":            session.ID,
		"projectId":            project.ID,
		"kind":                 "agent_session",
		"runtimeMode":          "team",
		"requiredCapabilities": map[string]bool{"codex": true},
		"payload":              payload,
	}
	var task runtimeTask
	if err := a.controlPlaneJSON(ctx, http.MethodPost, "/api/workspaces/{workspaceID}/runtime-tasks", body, http.StatusCreated, &task); err != nil {
		return runtimeTask{}, err
	}
	return task, nil
}

func remoteURLForTeamRuntime(project project) string {
	if strings.TrimSpace(project.RemoteURL) != "" {
		return strings.TrimSpace(project.RemoteURL)
	}
	return strings.TrimSpace(project.RepoPath)
}

func (a *app) waitTeamRuntimeTask(ctx context.Context, session agentSession, taskID string) error {
	seenLogs := map[string]bool{}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		task, err := a.loadTeamRuntimeTask(ctx, taskID)
		if err != nil {
			return err
		}
		a.importTeamRuntimeLogs(ctx, session.ID, taskID, seenLogs)
		a.updateSessionAgentStatus(session.ID, teamRuntimeAgentStatus(task.Status))
		switch task.Status {
		case "completed":
			a.importTeamRuntimeTaskResult(session.ID, task)
			a.importTeamRuntimeLogs(ctx, session.ID, taskID, seenLogs)
			return nil
		case "failed":
			a.importTeamRuntimeTaskResult(session.ID, task)
			if strings.TrimSpace(task.Error) != "" {
				return errors.New(task.Error)
			}
			return errors.New("team runtime task failed")
		case "cancelled":
			return context.Canceled
		}

		select {
		case <-ctx.Done():
			return context.Canceled
		case <-ticker.C:
		}
	}
}

func (a *app) loadTeamRuntimeTask(ctx context.Context, taskID string) (runtimeTask, error) {
	var tasks []runtimeTask
	if err := a.controlPlaneJSON(ctx, http.MethodGet, "/api/workspaces/{workspaceID}/runtime-tasks", nil, http.StatusOK, &tasks); err != nil {
		return runtimeTask{}, err
	}
	for _, task := range tasks {
		if task.ID == taskID {
			return task, nil
		}
	}
	return runtimeTask{}, fmt.Errorf("team runtime task %s was not found", taskID)
}

func (a *app) cancelTeamRuntimeTask(ctx context.Context, taskID, reason string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	var task runtimeTask
	if err := a.controlPlaneJSON(ctx, http.MethodPost, "/api/workspaces/{workspaceID}/runtime-tasks/"+url.PathEscape(taskID)+"/cancel", cancelRuntimeTaskInput{Reason: reason}, http.StatusOK, &task); err != nil {
		return err
	}
	return nil
}

func (a *app) importTeamRuntimeLogs(ctx context.Context, sessionID, taskID string, seen map[string]bool) {
	var logs []runtimeTaskLog
	if err := a.controlPlaneJSON(ctx, http.MethodGet, "/api/workspaces/{workspaceID}/runtime-tasks/"+url.PathEscape(taskID)+"/logs", nil, http.StatusOK, &logs); err != nil {
		if len(seen) == 0 {
			a.appendSessionLog(sessionID, "system", "Unable to read team runtime logs yet: "+err.Error())
		}
		return
	}
	for _, log := range logs {
		if seen[log.ID] {
			continue
		}
		seen[log.ID] = true
		stream := strings.TrimSpace(log.Stream)
		if stream == "" {
			stream = "runtime"
		}
		a.appendSessionLog(sessionID, stream, log.Message)
	}
}

func (a *app) importTeamRuntimeTaskResult(sessionID string, task runtimeTask) {
	if len(task.Result) == 0 {
		return
	}
	var result runtimeTaskResult
	if err := json.Unmarshal(task.Result, &result); err != nil {
		return
	}
	a.updateSessionCodexThread(sessionID, result.ThreadID)
	a.updateSessionCodexTurn(sessionID, result.TurnID)
}

func (a *app) loadTeamRuntimeTaskResult(ctx context.Context, taskID string) (runtimeTaskResult, error) {
	task, err := a.loadTeamRuntimeTask(ctx, taskID)
	if err != nil {
		return runtimeTaskResult{}, err
	}
	return parseRuntimeTaskResult(task.Result)
}

func parseRuntimeTaskResult(raw json.RawMessage) (runtimeTaskResult, error) {
	if len(raw) == 0 {
		return runtimeTaskResult{}, nil
	}
	var result runtimeTaskResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return runtimeTaskResult{}, err
	}
	return result, nil
}

func (a *app) applyTeamRuntimeResult(sessionID string, result runtimeTaskResult) {
	a.updateSessionCodexThread(sessionID, result.ThreadID)
	a.updateSessionCodexTurn(sessionID, result.TurnID)
	if strings.TrimSpace(result.Workdir) != "" {
		a.updateSessionWorkdir(sessionID, result.Workdir)
	}
	if strings.TrimSpace(result.ArtifactDir) != "" {
		a.updateSessionArtifactDir(sessionID, result.ArtifactDir)
	}
}

func (a *app) sessionWithTeamRuntimeResult(session agentSession, result runtimeTaskResult) agentSession {
	if strings.TrimSpace(result.Workdir) != "" {
		session.Workdir = strings.TrimSpace(result.Workdir)
	}
	if strings.TrimSpace(result.ArtifactDir) != "" {
		session.ArtifactDir = strings.TrimSpace(result.ArtifactDir)
	}
	return session
}

func (a *app) recordTeamRuntimeSourceChangeNode(session agentSession, project project, result runtimeTaskResult) (*issueChangeNode, error) {
	if !shouldRecordSourceChangeNode(session) {
		return nil, nil
	}
	if strings.TrimSpace(result.Source.CommitSHA) == "" {
		a.appendSessionLog(session.ID, "system", "Team runtime completed with no source changes to commit.")
		return nil, nil
	}
	node := issueChangeNode{
		ID:             uuid.NewString(),
		IssueID:        session.IssueID,
		SessionID:      session.ID,
		CommitSHA:      strings.TrimSpace(result.Source.CommitSHA),
		ShortCommitSHA: firstNonEmpty(result.Source.ShortCommitSHA, shortCommitSHA(result.Source.CommitSHA)),
		Branch:         firstNonEmpty(result.Source.Branch, session.Branch),
		Subject:        strings.TrimSpace(result.Source.Subject),
		FilesChanged:   result.Source.FilesChanged,
		Changes:        result.Source.Changes,
		DiffPreview:    strings.TrimSpace(result.Source.DiffPreview),
		DiffTruncated:  result.Source.DiffTruncated,
		Source:         "team-runtime",
		RemoteWorkdir:  strings.TrimSpace(result.Workdir),
		ArtifactDir:    strings.TrimSpace(result.ArtifactDir),
		CreatedAt:      nowString(),
	}
	if node.FilesChanged == 0 {
		node.FilesChanged = len(node.Changes)
	}
	if err := a.saveIssueChangeNode(node); err != nil {
		return nil, err
	}
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Captured team runtime source commit %s with %d changed files.", shortCommitSHA(node.CommitSHA), node.FilesChanged))
	a.broker.publish(session.ID, sessionEvent{Type: "status", Payload: "source-commit"})
	return &node, nil
}

func teamRuntimeAgentStatus(status string) string {
	switch status {
	case "queued":
		return "team-runtime-queued"
	case "claimed":
		return "team-runtime-claimed"
	case "running":
		return "team-runtime-running"
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	case "cancelled":
		return "cancelled"
	default:
		if strings.TrimSpace(status) == "" {
			return "team-runtime"
		}
		return "team-runtime-" + status
	}
}

func (a *app) controlPlaneJSON(ctx context.Context, method, path string, input any, expectedStatus int, output any) error {
	baseURL, token, workspaceID := a.controlPlaneSession()
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(token) == "" || strings.TrimSpace(workspaceID) == "" {
		return errors.New("sign in to mspace and select a workspace before using team runtime")
	}
	path = strings.ReplaceAll(path, "{workspaceID}", url.PathEscape(workspaceID))
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != expectedStatus {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = fmt.Sprintf("control plane returned HTTP %d", res.StatusCode)
		}
		if res.StatusCode == http.StatusNotFound && method == http.MethodPost && strings.HasSuffix(path, "/cancel") {
			message = "team runtime task is already final or unavailable for cancellation: " + message
		}
		return errors.New(message)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(output)
}

func (a *app) requireTeamControlPlaneWorkspace(ctx context.Context) error {
	baseURL, token, workspaceID := a.controlPlaneSession()
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(token) == "" || strings.TrimSpace(workspaceID) == "" {
		return errors.New("sign in to mspace and select a workspace before using team runtime")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/auth/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = fmt.Sprintf("control plane returned HTTP %d", res.StatusCode)
		}
		return errors.New(message)
	}
	var payload struct {
		Workspaces []controlPlaneWorkspaceRef `json:"workspaces"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return err
	}
	for _, workspace := range payload.Workspaces {
		if workspace.ID != workspaceID {
			continue
		}
		if strings.TrimSpace(workspace.Kind) == "team" {
			return nil
		}
		return errors.New("team runtime requires a team workspace; create or switch to a team workspace in Workspace Settings")
	}
	return errors.New("selected workspace is not available for team runtime")
}

func envSliceToMap(values []string) map[string]string {
	env := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		env[key] = val
	}
	return env
}

func (a *app) recordSourceChangeNode(session agentSession, project project) (*issueChangeNode, error) {
	if !shouldRecordSourceChangeNode(session) {
		return nil, nil
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git is not available on PATH")
	}
	if err := ensureGitRepository(gitPath, session.Workdir); err != nil {
		return nil, err
	}

	output, err := runGitWriteWithIndexLockRetry(gitPath, session.Workdir, nil, "add", "-A", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("stage source changes: %s", formatCommandFailure(err, output))
	}
	output, err = runGitWriteWithIndexLockRetry(gitPath, session.Workdir, nil, "reset", "-q", "--", ".mspace")
	if err != nil {
		return nil, fmt.Errorf("exclude session artifacts from source commit: %s", formatCommandFailure(err, output))
	}

	if err := exec.Command(gitPath, "-C", session.Workdir, "diff", "--cached", "--quiet").Run(); err == nil {
		changeNode, err := a.recordExistingSourceHeadChangeNode(session, project, gitPath)
		if err != nil {
			return nil, err
		}
		if changeNode != nil {
			return changeNode, nil
		}
		a.appendSessionLog(session.ID, "system", "Session completed with no source changes to commit.")
		return nil, nil
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("inspect staged source changes: %w", err)
		}
	}

	issueTitle := a.issueTitleForCommit(session.IssueID)
	subject := sourceCommitSubject(issueTitle, session.IssueID)
	output, err = runGitWriteWithIndexLockRetry(gitPath, session.Workdir, []string{
		"GIT_AUTHOR_NAME=mspace",
		"GIT_AUTHOR_EMAIL=mspace@example.local",
		"GIT_COMMITTER_NAME=mspace",
		"GIT_COMMITTER_EMAIL=mspace@example.local",
	}, "commit", "-m", subject)
	if err != nil {
		return nil, fmt.Errorf("commit source changes: %s", formatCommandFailure(err, output))
	}

	commitSHA, err := runGitReadOnly(gitPath, session.Workdir, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	node, err := a.recordIssueChangeNodeForCommit(session, project, gitPath, commitSHA)
	if err != nil {
		return nil, err
	}
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Captured source commit %s with %d changed files.", shortCommitSHA(node.CommitSHA), node.FilesChanged))
	a.broker.publish(session.ID, sessionEvent{Type: "status", Payload: "source-commit"})
	return node, nil
}

func (a *app) recordExistingSourceHeadChangeNode(session agentSession, project project, gitPath string) (*issueChangeNode, error) {
	baseRef, err := resolveBaseRef(gitPath, session.Workdir, project.DefaultBranch)
	if err != nil {
		return nil, err
	}
	mergeBase, err := runGitReadOnly(gitPath, session.Workdir, "merge-base", "HEAD", baseRef)
	if err != nil {
		return nil, err
	}
	mergeBase = strings.TrimSpace(mergeBase)

	head, err := runGitReadOnly(gitPath, session.Workdir, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	head = strings.TrimSpace(head)
	if head == "" || mergeBase == "" || head == mergeBase {
		return nil, nil
	}

	aheadOutput, err := runGitReadOnly(gitPath, session.Workdir, "rev-list", "--count", mergeBase+"..HEAD")
	if err != nil {
		return nil, err
	}
	if parseIntOrZero(aheadOutput) == 0 {
		return nil, nil
	}

	existingNode, err := a.loadIssueChangeNodeByCommit(session.IssueID, head, project)
	if err != nil {
		return nil, err
	}
	if existingNode != nil {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Source commit %s is already recorded for this issue; skipping duplicate source capture.", shortCommitSHA(head)))
		return nil, nil
	}

	node, err := a.recordIssueChangeNodeForCommit(session, project, gitPath, head)
	if err != nil {
		return nil, err
	}
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Captured existing source commit %s with %d changed files.", shortCommitSHA(node.CommitSHA), node.FilesChanged))
	a.broker.publish(session.ID, sessionEvent{Type: "status", Payload: "source-commit"})
	return node, nil
}

func (a *app) recordIssueChangeNodeForCommit(session agentSession, project project, gitPath, commitSHA string) (*issueChangeNode, error) {
	commitSubject, err := runGitReadOnly(gitPath, session.Workdir, "log", "-1", "--pretty=%s", commitSHA)
	if err != nil {
		return nil, err
	}
	nameStatusOutput, err := runGitReadOnly(gitPath, session.Workdir, "diff-tree", "--root", "--no-commit-id", "--name-status", "-r", "--find-renames", commitSHA)
	if err != nil {
		return nil, err
	}
	changes := []workspaceChange{}
	for _, line := range splitNonEmptyLines(nameStatusOutput) {
		changes = append(changes, parseNameStatusChange(line))
	}

	node := issueChangeNode{
		ID:             uuid.NewString(),
		IssueID:        session.IssueID,
		SessionID:      session.ID,
		CommitSHA:      commitSHA,
		ShortCommitSHA: shortCommitSHA(commitSHA),
		Branch:         session.Branch,
		Subject:        commitSubject,
		FilesChanged:   len(changes),
		Changes:        changes,
		Source:         "local",
		RemoteWorkdir:  "",
		ArtifactDir:    session.ArtifactDir,
		CreatedAt:      nowString(),
	}
	if err := a.saveIssueChangeNode(node); err != nil {
		return nil, err
	}

	hydrateIssueChangeNode(&node, project)
	return &node, nil
}

func (a *app) saveIssueChangeNode(node issueChangeNode) error {
	if node.ID == "" {
		node.ID = uuid.NewString()
	}
	if node.ShortCommitSHA == "" {
		node.ShortCommitSHA = shortCommitSHA(node.CommitSHA)
	}
	if node.Source == "" {
		node.Source = "local"
	}
	if node.CreatedAt == "" {
		node.CreatedAt = nowString()
	}
	if node.FilesChanged == 0 {
		node.FilesChanged = len(node.Changes)
	}
	changesJSON, err := json.Marshal(node.Changes)
	if err != nil {
		return err
	}
	diffTruncated := 0
	if node.DiffTruncated {
		diffTruncated = 1
	}
	_, err = a.db.Exec(`
		INSERT INTO issue_change_nodes (id, issue_id, session_id, commit_sha, branch, subject, files_changed, changes_json, diff_preview, diff_truncated, source, remote_workdir, artifact_dir, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			commit_sha = excluded.commit_sha,
			branch = excluded.branch,
			subject = excluded.subject,
			files_changed = excluded.files_changed,
			changes_json = excluded.changes_json,
			diff_preview = excluded.diff_preview,
			diff_truncated = excluded.diff_truncated,
			source = excluded.source,
			remote_workdir = excluded.remote_workdir,
			artifact_dir = excluded.artifact_dir,
			created_at = excluded.created_at
	`, node.ID, node.IssueID, node.SessionID, node.CommitSHA, node.Branch, node.Subject, node.FilesChanged, string(changesJSON), node.DiffPreview, diffTruncated, node.Source, node.RemoteWorkdir, node.ArtifactDir, node.CreatedAt)
	return err
}

func shouldRecordSourceChangeNode(session agentSession) bool {
	if strings.TrimSpace(session.SourceCommitSHA) != "" || strings.TrimSpace(session.SourceSessionID) != "" {
		return false
	}
	return !isSystemTestEnvironmentCommand(session.Command)
}

func (a *app) issueTitleForCommit(issueID string) string {
	var title string
	_ = a.db.QueryRow(`SELECT title FROM issues WHERE id = ?`, issueID).Scan(&title)
	return title
}

func sourceCommitSubject(issueTitle, issueID string) string {
	title := strings.Join(strings.Fields(strings.TrimSpace(issueTitle)), " ")
	if title == "" {
		title = "issue " + shortID(issueID)
	}
	subject := "mspace: " + title
	const maxSubjectRunes = 72
	runes := []rune(subject)
	if len(runes) <= maxSubjectRunes {
		return subject
	}
	return string(runes[:maxSubjectRunes-3]) + "..."
}

func (a *app) runShellSession(ctx context.Context, session agentSession, project project, contextPath string) error {
	command := exec.CommandContext(ctx, "/bin/zsh", "-lc", session.Command)
	command.Dir = session.Workdir
	command.Env = append(os.Environ(), a.buildSessionEnv(session, project, contextPath)...)

	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go a.captureStream(&wg, session.ID, "stdout", stdout)
	go a.captureStream(&wg, session.ID, "stderr", stderr)

	err = command.Wait()
	wg.Wait()
	if ctx.Err() == context.Canceled {
		return context.Canceled
	}
	if err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}

func (a *app) captureStream(wg *sync.WaitGroup, sessionID, stream string, reader interface{ Read([]byte) (int, error) }) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		a.appendSessionLog(sessionID, stream, scanner.Text())
	}
}

func (a *app) failSession(session agentSession, project *project, err error) {
	a.appendSessionLog(session.ID, "system", err.Error())
	a.updateSessionStatus(session.ID, "failed")
	a.updateSessionAgentStatus(session.ID, "failed")
	session.Status = "failed"
	session.AgentStatus = "failed"
	a.updateIssueStatus(session.IssueID, "blocked")
	a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` failed.\n\n%s", shortID(session.ID), err.Error()))
	var evidence *deploymentEvidence
	if project != nil && a.isIssueTestDeploySession(session) {
		a.appendSessionLog(session.ID, "system", "Reconciling test deployment state after failure.")
		evidence = a.reconcileIssueTestEnvironmentForSession(session, *project, "failed")
	} else if project != nil && a.evidenceTargetProject(session, *project).Namespace != "" {
		a.appendSessionLog(session.ID, "system", "Collecting Kubernetes evidence after failure.")
		evidence = a.collectEvidence(session, *project)
		a.updateIssueTestEnvironmentForSession(session, false)
	} else {
		a.updateIssueTestEnvironmentForSession(session, false)
	}
	if project != nil {
		a.recordSessionReviewEvidence(session, *project, nil, false)
		a.recordSessionFailure(session, *project, err, evidence, "", "open")
	}
}

func (a *app) writeSessionContext(session agentSession, project project) (string, error) {
	detail, err := a.loadIssueDetail(session.IssueID)
	if err != nil {
		return "", err
	}
	contextDir := filepath.Join(a.workdir, "_contexts")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return "", err
	}
	contextPath := filepath.Join(contextDir, session.ID+".md")

	var builder strings.Builder
	builder.WriteString("# mspace Session Context\n\n")
	builder.WriteString("## Issue\n\n")
	builder.WriteString(fmt.Sprintf("- ID: %s\n", detail.Issue.ID))
	builder.WriteString(fmt.Sprintf("- Title: %s\n", detail.Issue.Title))
	builder.WriteString(fmt.Sprintf("- Status: %s\n", detail.Issue.Status))
	builder.WriteString(fmt.Sprintf("- Creator: %s\n", normalizeHumanActorName(detail.Issue.CreatorName)))
	builder.WriteString(fmt.Sprintf("- Assignee: %s (%s)\n", detail.Issue.Assignee, detail.Issue.AssigneeType))
	builder.WriteString(fmt.Sprintf("- Labels: %s\n", formatIssueLabels(detail.Labels)))
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(detail.Issue.Body))
	if strings.TrimSpace(detail.Issue.Body) == "" {
		builder.WriteString("(no issue body)")
	}
	builder.WriteString("\n\n")

	writeIssueTaskList(&builder, detail.ChildIssues)

	builder.WriteString("## Project\n\n")
	builder.WriteString(fmt.Sprintf("- Name: %s\n", project.Name))
	builder.WriteString(fmt.Sprintf("- Repository: %s\n", project.RepoPath))
	builder.WriteString(fmt.Sprintf("- Remote: %s\n", project.RemoteURL))
	builder.WriteString(fmt.Sprintf("- GitHub: %s/%s\n", project.GitOwner, project.GitRepo))
	builder.WriteString(fmt.Sprintf("- Default branch: %s\n", project.DefaultBranch))
	builder.WriteString(fmt.Sprintf("- Kube context: %s\n", project.KubeContext))
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", valueOrUnset(project.KubeconfigPath)))
	builder.WriteString(fmt.Sprintf("- Namespace: %s\n", project.Namespace))
	builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", valueOrUnset(project.ImageRegistryPrefix)))
	builder.WriteString(fmt.Sprintf("- Preview domain: %s\n", valueOrUnset(project.PreviewDomain)))
	builder.WriteString(fmt.Sprintf("- Ingress class: %s\n", valueOrUnset(project.IngressClass)))
	builder.WriteString(fmt.Sprintf("- Node host: %s\n", valueOrUnset(project.NodeHost)))
	builder.WriteString(fmt.Sprintf("- Default cluster ID: %s\n", valueOrUnset(project.DefaultClusterID)))
	builder.WriteString("\n")

	if runbook, err := a.loadProjectRunbook(project.ID); err == nil {
		writeProjectRunbookPromptSection(&builder, runbook)
	}

	if detail.TestEnvironment != nil {
		builder.WriteString("## Issue Test Environment\n\n")
		builder.WriteString(fmt.Sprintf("- Cluster ID: %s\n", valueOrUnset(detail.TestEnvironment.ClusterID)))
		builder.WriteString(fmt.Sprintf("- Namespace: %s\n", detail.TestEnvironment.Namespace))
		builder.WriteString(fmt.Sprintf("- Namespace status: %s\n", detail.TestEnvironment.NamespaceStatus))
		builder.WriteString(fmt.Sprintf("- Cleanup status: %s\n", detail.TestEnvironment.CleanupStatus))
		builder.WriteString(fmt.Sprintf("- Preview URL: %s\n", valueOrUnset(detail.TestEnvironment.PreviewURL)))
		builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", valueOrUnset(detail.TestEnvironment.ImageRegistryPrefix)))
		builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", valueOrUnset(detail.TestEnvironment.KubeconfigPath)))
		builder.WriteString(fmt.Sprintf("- Kube context: %s\n", valueOrUnset(detail.TestEnvironment.KubeContext)))
		builder.WriteString(fmt.Sprintf("- Exposure mode: %s\n", valueOrUnset(detail.TestEnvironment.ExposureMode)))
		builder.WriteString(fmt.Sprintf("- Preview strategy: %s\n", previewStrategyLabel(*detail.TestEnvironment)))
		builder.WriteString(fmt.Sprintf("- Source session: %s\n", valueOrUnset(detail.TestEnvironment.SourceSessionID)))
		builder.WriteString(fmt.Sprintf("- Source commit: %s\n", valueOrUnset(detail.TestEnvironment.SourceCommitSHA)))
		builder.WriteString("\n")
	}

	builder.WriteString("## Session\n\n")
	builder.WriteString(fmt.Sprintf("- ID: %s\n", session.ID))
	builder.WriteString(fmt.Sprintf("- Provider: %s\n", session.Provider))
	builder.WriteString(fmt.Sprintf("- Agent profile: %s\n", valueOrUnset(session.AgentProfile)))
	builder.WriteString(fmt.Sprintf("- Branch: %s\n", session.Branch))
	builder.WriteString(fmt.Sprintf("- Workdir: %s\n", session.Workdir))
	builder.WriteString(fmt.Sprintf("- Source session: %s\n", valueOrUnset(session.SourceSessionID)))
	builder.WriteString(fmt.Sprintf("- Source commit: %s\n", valueOrUnset(session.SourceCommitSHA)))
	builder.WriteString("\n")

	builder.WriteString("## Comments\n\n")
	if len(detail.Comments) == 0 {
		builder.WriteString("(no comments)\n")
	} else {
		for i := len(detail.Comments) - 1; i >= 0; i-- {
			comment := detail.Comments[i]
			builder.WriteString(fmt.Sprintf("### %s at %s\n\n", comment.AuthorType, comment.CreatedAt))
			builder.WriteString(strings.TrimSpace(comment.Body))
			builder.WriteString("\n\n")
		}
	}

	if err := os.WriteFile(contextPath, []byte(builder.String()), 0o600); err != nil {
		return "", err
	}
	return contextPath, nil
}

func (a *app) collectEvidence(session agentSession, project project) *deploymentEvidence {
	if evidence := a.collectEvidenceSnapshot(session, project); evidence != nil {
		a.storeEvidence(*evidence, true)
		return evidence
	}
	return nil
}

func (a *app) collectEvidenceSnapshot(session agentSession, project project) *deploymentEvidence {
	project = a.evidenceTargetProject(session, project)
	if project.Namespace == "" {
		a.appendSessionLog(session.ID, "system", "Skipping Kubernetes evidence collection because no namespace is configured for this project.")
		return nil
	}

	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		a.appendSessionLog(session.ID, "system", "kubectl is not available on PATH, so Kubernetes evidence could not be collected.")
		return &deploymentEvidence{
			ID:        uuid.NewString(),
			IssueID:   session.IssueID,
			SessionID: session.ID,
			Cluster:   project.KubeContext,
			Namespace: project.Namespace,
			Summary:   "Kubernetes evidence collection failed after session completion.",
			Details:   "kubectl was not found on PATH.",
			CreatedAt: nowString(),
		}
	}

	summaryOutput, summaryErr := exec.Command(kubectlPath, buildKubectlArgs(project, "get", "pods,deploy,svc,ingress")...).CombinedOutput()
	eventsOutput, eventsErr := exec.Command(kubectlPath, buildKubectlArgs(project, "get", "events", "--sort-by=.lastTimestamp")...).CombinedOutput()

	summary := "Captured Kubernetes resources after session completion."
	if summaryErr != nil {
		summary = "Kubernetes evidence collection failed after session completion."
	}
	details := strings.TrimSpace(string(summaryOutput))
	if details == "" {
		if summaryErr != nil {
			details = summaryErr.Error()
		} else {
			details = "(no kubectl summary output)"
		}
	}
	if strings.TrimSpace(string(eventsOutput)) != "" {
		details += "\n\n--- events ---\n" + strings.TrimSpace(string(eventsOutput))
	} else if eventsErr != nil {
		details += "\n\n--- events ---\n" + eventsErr.Error()
	}

	return &deploymentEvidence{
		ID:        uuid.NewString(),
		IssueID:   session.IssueID,
		SessionID: session.ID,
		Cluster:   project.KubeContext,
		Namespace: project.Namespace,
		Summary:   summary,
		Details:   truncate(details, 12000),
		CreatedAt: nowString(),
	}
}

func (a *app) evidenceTargetProject(session agentSession, project project) project {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	if err != nil || environment == nil {
		return project
	}
	if strings.TrimSpace(environment.Namespace) != "" {
		project.Namespace = strings.TrimSpace(environment.Namespace)
	}
	if strings.TrimSpace(environment.KubeconfigPath) != "" {
		project.KubeconfigPath = strings.TrimSpace(environment.KubeconfigPath)
	}
	if strings.TrimSpace(environment.KubeContext) != "" {
		project.KubeContext = strings.TrimSpace(environment.KubeContext)
	} else if strings.TrimSpace(environment.ClusterID) != "" {
		project.KubeContext = strings.TrimSpace(environment.ClusterID)
	}
	return project
}

func (a *app) updateIssueTestEnvironmentForSession(session agentSession, success bool) {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	if err != nil || environment == nil {
		return
	}
	previewURL := readTestEnvironmentPreviewURL(session)
	changed := false
	switch {
	case environment.LastDeploySessionID == session.ID:
		changed = true
		if success {
			if previewURL != "" {
				environment.PreviewURL = previewURL
			}
			environment.NamespaceStatus = "active"
			environment.CleanupStatus = "retained"
			a.updateIssueStatus(session.IssueID, "ready_for_test")
		} else {
			environment.NamespaceStatus = "deploy_failed"
			a.updateIssueStatus(session.IssueID, "blocked")
		}
	case environment.LastCleanupSessionID == session.ID:
		changed = true
		if success {
			environment.NamespaceStatus = "cleaned"
			environment.CleanupStatus = "cleaned"
		} else {
			environment.NamespaceStatus = "cleanup_failed"
			environment.CleanupStatus = "cleanup_failed"
		}
	case previewURL != "" && environment.LastCleanupSessionID != session.ID:
		changed = true
		environment.PreviewURL = previewURL
		environment.NamespaceStatus = "active"
		environment.CleanupStatus = "retained"
		environment.LastDeploySessionID = session.ID
		if strings.TrimSpace(session.SourceSessionID) != "" {
			environment.SourceSessionID = session.SourceSessionID
		}
		if strings.TrimSpace(session.SourceCommitSHA) != "" {
			environment.SourceCommitSHA = session.SourceCommitSHA
		}
		a.updateIssueStatus(session.IssueID, "ready_for_test")
		a.appendSessionLog(session.ID, "system", "Updated issue test environment from session preview artifact.")
	}
	if !changed {
		return
	}
	if err := a.saveIssueTestEnvironment(*environment); err == nil {
		a.publishInboxEvent(session.IssueID, "test-environment")
	}
}

func (a *app) markIssueTestEnvironmentSessionInterrupted(session agentSession) {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	if err != nil || environment == nil {
		return
	}

	changed := false
	switch {
	case environment.LastDeploySessionID == session.ID:
		environment.NamespaceStatus = "deploy_interrupted"
		changed = true
	case environment.LastCleanupSessionID == session.ID:
		environment.NamespaceStatus = "cleanup_failed"
		environment.CleanupStatus = "cleanup_failed"
		changed = true
	}
	if !changed {
		return
	}
	if err := a.saveIssueTestEnvironment(*environment); err == nil {
		a.publishInboxEvent(session.IssueID, "test-environment")
	}
}

func (a *app) isIssueTestDeploySession(session agentSession) bool {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	if err != nil || environment == nil {
		return false
	}
	return environment.LastDeploySessionID == session.ID
}

func (a *app) isIssueTestCleanupSession(session agentSession) bool {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	if err != nil || environment == nil {
		return false
	}
	return environment.LastCleanupSessionID == session.ID
}

func readTestEnvironmentPreviewURL(session agentSession) string {
	paths := []string{}
	if strings.TrimSpace(session.ArtifactDir) != "" {
		paths = append(paths, filepath.Join(session.ArtifactDir, "test-environment.json"))
	}
	if strings.TrimSpace(session.Workdir) != "" {
		workdirPath := filepath.Join(session.Workdir, ".mspace", "session", "test-environment.json")
		if len(paths) == 0 || paths[0] != workdirPath {
			paths = append(paths, workdirPath)
		}
	}
	for _, resultPath := range paths {
		data, err := os.ReadFile(resultPath)
		if err != nil {
			continue
		}
		var result struct {
			PreviewURL string `json:"previewUrl"`
			PreviewUrl string `json:"preview_url"`
			URL        string `json:"url"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			continue
		}
		if previewURL := firstNonEmpty(result.PreviewURL, result.PreviewUrl, result.URL); previewURL != "" {
			return previewURL
		}
	}
	return ""
}

type deployStagePayload struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Time    string `json:"time"`
}

type previewCandidate struct {
	URL    string
	Source string
}

type previewProbeResult struct {
	URL        string
	Source     string
	OK         bool
	StatusCode int
	Error      string
}

func (a *app) appendDeployStage(sessionID, id, label, status, summary string) {
	payload, err := json.Marshal(deployStagePayload{
		ID:      id,
		Label:   label,
		Status:  status,
		Summary: summary,
		Time:    nowString(),
	})
	if err != nil {
		return
	}
	a.appendSessionLog(sessionID, "deploy-stage", string(payload))
}

func (a *app) reconcileIssueTestEnvironmentForSession(session agentSession, project project, outcome string) *deploymentEvidence {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	artifactPreviewURL := readTestEnvironmentPreviewURL(session)
	adoptPreviewSession := environment != nil && environment.LastDeploySessionID != session.ID && artifactPreviewURL != ""
	if err != nil || environment == nil || (environment.LastDeploySessionID != session.ID && !adoptPreviewSession) {
		return nil
	}

	targetProject := a.evidenceTargetProject(session, project)
	var evidence *deploymentEvidence
	if outcome != "probe" {
		a.appendDeployStage(session.ID, "capture-evidence", "Capture Kubernetes evidence", "running", "Reading namespace resources and recent events.")
		evidence = a.collectEvidenceSnapshot(session, targetProject)
		if evidence != nil {
			a.appendDeployStage(session.ID, "capture-evidence", "Capture Kubernetes evidence", "passed", evidence.Summary)
		} else {
			a.appendDeployStage(session.ID, "capture-evidence", "Capture Kubernetes evidence", "skipped", "No namespace evidence was available.")
		}
	}

	a.appendDeployStage(session.ID, "discover-preview", "Discover preview URL", "running", "Looking for artifact, ingress, HTTPRoute, and NodePort candidates.")
	kubectlPath, kubectlErr := exec.LookPath("kubectl")
	candidates := []previewCandidate{}
	if existingURL := strings.TrimSpace(environment.PreviewURL); existingURL != "" {
		candidates = append(candidates, previewCandidate{URL: existingURL, Source: "environment"})
	}
	if artifactPreviewURL != "" {
		candidates = append(candidates, previewCandidate{URL: artifactPreviewURL, Source: "artifact"})
	}
	resourcesReady := false
	discoverySummary := ""
	if kubectlErr == nil {
		if ready, summary := kubernetesResourcesReady(kubectlPath, targetProject); summary != "" {
			resourcesReady = ready
			discoverySummary = summary
		}
		candidates = append(candidates, discoverPreviewCandidates(kubectlPath, targetProject, *environment)...)
	} else {
		discoverySummary = "kubectl is not available on PATH."
	}
	candidates = uniquePreviewCandidates(candidates)
	a.appendDeployStage(session.ID, "discover-preview", "Discover preview URL", "passed", fmt.Sprintf("Found %d preview candidate(s).", len(candidates)))

	a.appendDeployStage(session.ID, "probe-preview", "Check preview", "running", "Checking whether a candidate URL is open.")
	probe := probePreviewCandidates(candidates)
	if probe.OK {
		a.appendDeployStage(session.ID, "probe-preview", "Check preview", "passed", fmt.Sprintf("Preview opened with HTTP %d: %s", probe.StatusCode, probe.URL))
	} else if probe.Error != "" {
		a.appendDeployStage(session.ID, "probe-preview", "Check preview", "failed", probe.Error)
	} else {
		a.appendDeployStage(session.ID, "probe-preview", "Check preview", "skipped", "No preview candidates were available.")
	}

	if probe.OK {
		environment.PreviewURL = probe.URL
		environment.NamespaceStatus = "active"
		environment.CleanupStatus = "retained"
		if adoptPreviewSession {
			environment.LastDeploySessionID = session.ID
			if strings.TrimSpace(session.SourceSessionID) != "" {
				environment.SourceSessionID = session.SourceSessionID
			}
			if strings.TrimSpace(session.SourceCommitSHA) != "" {
				environment.SourceCommitSHA = session.SourceCommitSHA
			}
		}
		if outcome != "probe" {
			a.updateIssueStatus(session.IssueID, "ready_for_test")
		}
	} else if resourcesReady {
		environment.NamespaceStatus = "preview_unverified"
		environment.CleanupStatus = "retained"
		if adoptPreviewSession {
			environment.LastDeploySessionID = session.ID
		}
		if outcome != "probe" {
			a.updateIssueStatus(session.IssueID, "blocked")
		}
	} else if outcome == "interrupted" {
		environment.NamespaceStatus = "deploy_interrupted"
		a.updateIssueStatus(session.IssueID, "blocked")
	} else {
		environment.NamespaceStatus = "deploy_failed"
		if outcome != "probe" {
			a.updateIssueStatus(session.IssueID, "blocked")
		}
	}

	if err := a.saveIssueTestEnvironment(*environment); err == nil {
		a.publishInboxEvent(session.IssueID, "test-environment")
	}

	summary := deploymentReconcileSummary(*environment, resourcesReady, probe, outcome)
	a.appendDeployStage(session.ID, "reconcile", "Finalize deployment state", deployStageStatusForNamespace(environment.NamespaceStatus), summary)
	if outcome == "probe" {
		return nil
	}

	details := buildDeploymentReconcileDetails(discoverySummary, candidates, probe, evidence)
	record := deploymentEvidence{
		ID:        uuid.NewString(),
		IssueID:   session.IssueID,
		SessionID: session.ID,
		Cluster:   firstNonEmpty(environment.KubeContext, environment.ClusterID),
		Namespace: environment.Namespace,
		Summary:   summary,
		Details:   truncate(details, 12000),
		CreatedAt: nowString(),
	}
	a.storeEvidence(record, true)
	if shouldRecordFailureForNamespaceStatus(environment.NamespaceStatus) {
		a.recordSessionFailure(session, targetProject, errors.New(summary), &record, failurePhaseForNamespaceStatus(environment.NamespaceStatus), failureStatusForSession(session))
	}
	return &record
}

func deploymentReconcileSummary(environment issueTestEnvironment, resourcesReady bool, probe previewProbeResult, outcome string) string {
	if probe.OK {
		return fmt.Sprintf("Deployment is active. Preview URL opened with HTTP %d.", probe.StatusCode)
	}
	if resourcesReady {
		return "Deployment resources look ready, but no preview URL could be verified."
	}
	if outcome == "interrupted" {
		return "Deployment session was interrupted before mspace could verify readiness."
	}
	if environment.NamespaceStatus == "deploy_failed" {
		return "Deployment could not be verified as ready."
	}
	return "Deployment state was reconciled."
}

func deployStageStatusForNamespace(namespaceStatus string) string {
	switch namespaceStatus {
	case "active":
		return "passed"
	case "preview_unverified":
		return "warning"
	case "deploy_failed", "deploy_interrupted":
		return "failed"
	default:
		return "completed"
	}
}

func buildDeploymentReconcileDetails(discoverySummary string, candidates []previewCandidate, probe previewProbeResult, evidence *deploymentEvidence) string {
	var builder strings.Builder
	if discoverySummary != "" {
		builder.WriteString(discoverySummary)
		builder.WriteString("\n\n")
	}
	builder.WriteString("Preview candidates:\n")
	if len(candidates) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, candidate := range candidates {
			builder.WriteString(fmt.Sprintf("- %s (%s)\n", candidate.URL, candidate.Source))
		}
	}
	builder.WriteString("\nPreview check result:\n")
	if probe.OK {
		builder.WriteString(fmt.Sprintf("- opened %s with HTTP %d\n", probe.URL, probe.StatusCode))
	} else if probe.Error != "" {
		builder.WriteString("- " + probe.Error + "\n")
	} else {
		builder.WriteString("- no preview check ran\n")
	}
	if evidence != nil && strings.TrimSpace(evidence.Details) != "" {
		builder.WriteString("\nKubernetes snapshot:\n")
		builder.WriteString(evidence.Details)
	}
	return builder.String()
}

func kubernetesResourcesReady(kubectlPath string, project project) (bool, string) {
	deployments, deployErr := kubectlJSONItems(kubectlPath, project, "deploy")
	pods, podErr := kubectlJSONItems(kubectlPath, project, "pods")
	if deployErr != nil && podErr != nil {
		return false, fmt.Sprintf("Could not inspect deployments or pods: %v; %v", deployErr, podErr)
	}
	if len(deployments) > 0 {
		for _, item := range deployments {
			spec := mapValue(item, "spec")
			status := mapValue(item, "status")
			desired := intValue(spec, "replicas")
			if desired == 0 {
				desired = 1
			}
			if intValue(status, "readyReplicas") < desired || intValue(status, "availableReplicas") < desired {
				return false, "At least one deployment is not fully available."
			}
		}
		return true, "All deployments report ready and available replicas."
	}
	if len(pods) > 0 {
		for _, item := range pods {
			status := mapValue(item, "status")
			phase := stringValue(status, "phase")
			if phase != "Running" && phase != "Succeeded" {
				return false, "At least one pod is not running."
			}
			if phase == "Running" && !podReady(status) {
				return false, "At least one running pod is not ready."
			}
		}
		return true, "All pods are running or succeeded."
	}
	return false, "No deployments or pods were found in the namespace."
}

func discoverPreviewCandidates(kubectlPath string, project project, environment issueTestEnvironment) []previewCandidate {
	candidates := []previewCandidate{}
	candidates = append(candidates, ingressPreviewCandidates(kubectlPath, project)...)
	candidates = append(candidates, httpRoutePreviewCandidates(kubectlPath, project)...)
	candidates = append(candidates, nodePortPreviewCandidates(kubectlPath, project, environment)...)
	return candidates
}

func ingressPreviewCandidates(kubectlPath string, project project) []previewCandidate {
	items, err := kubectlJSONItems(kubectlPath, project, "ingress")
	if err != nil {
		return nil
	}
	candidates := []previewCandidate{}
	for _, item := range items {
		spec := mapValue(item, "spec")
		tlsHosts := map[string]bool{}
		for _, tls := range sliceValue(spec, "tls") {
			for _, host := range stringSliceValue(mapAny(tls), "hosts") {
				tlsHosts[host] = true
			}
		}
		for _, rule := range sliceValue(spec, "rules") {
			ruleMap := mapAny(rule)
			host := stringValue(ruleMap, "host")
			if host == "" {
				continue
			}
			scheme := "http"
			if tlsHosts[host] {
				scheme = "https"
			}
			paths := sliceValue(mapValue(ruleMap, "http"), "paths")
			if len(paths) == 0 {
				candidates = append(candidates, previewCandidate{URL: scheme + "://" + host, Source: "ingress"})
				continue
			}
			for _, pathItem := range paths {
				path := stringValue(mapAny(pathItem), "path")
				candidates = append(candidates, previewCandidate{URL: scheme + "://" + host + normalizeURLPath(path), Source: "ingress"})
			}
		}
	}
	return candidates
}

func httpRoutePreviewCandidates(kubectlPath string, project project) []previewCandidate {
	items, err := kubectlJSONItems(kubectlPath, project, "httproute")
	if err != nil {
		return nil
	}
	candidates := []previewCandidate{}
	for _, item := range items {
		spec := mapValue(item, "spec")
		hosts := stringSliceValue(spec, "hostnames")
		for _, host := range hosts {
			if host == "" {
				continue
			}
			paths := []string{""}
			for _, rule := range sliceValue(spec, "rules") {
				for _, match := range sliceValue(mapAny(rule), "matches") {
					path := stringValue(mapValue(mapAny(match), "path"), "value")
					if path != "" {
						paths = append(paths, path)
					}
				}
			}
			for _, path := range paths {
				candidates = append(candidates, previewCandidate{URL: "http://" + host + normalizeURLPath(path), Source: "httproute"})
			}
		}
	}
	return candidates
}

func nodePortPreviewCandidates(kubectlPath string, project project, environment issueTestEnvironment) []previewCandidate {
	items, err := kubectlJSONItems(kubectlPath, project, "svc")
	if err != nil {
		return nil
	}
	host := strings.TrimSpace(environment.NodeHost)
	if host == "" {
		host = discoverNodeHost(kubectlPath, project)
	}
	if host == "" {
		return nil
	}
	candidates := []previewCandidate{}
	for _, item := range items {
		spec := mapValue(item, "spec")
		if stringValue(spec, "type") != "NodePort" {
			continue
		}
		for _, portItem := range sliceValue(spec, "ports") {
			port := intValue(mapAny(portItem), "nodePort")
			if port > 0 {
				candidates = append(candidates, previewCandidate{URL: fmt.Sprintf("http://%s:%d", host, port), Source: "nodeport"})
			}
		}
	}
	return candidates
}

func discoverNodeHost(kubectlPath string, project project) string {
	nodeProject := project
	nodeProject.Namespace = ""
	items, err := kubectlJSONItems(kubectlPath, nodeProject, "nodes")
	if err != nil {
		return ""
	}
	for _, preferredType := range []string{"ExternalIP", "InternalIP", "Hostname"} {
		for _, item := range items {
			for _, address := range sliceValue(mapValue(item, "status"), "addresses") {
				addressMap := mapAny(address)
				if stringValue(addressMap, "type") == preferredType {
					if value := stringValue(addressMap, "address"); value != "" {
						return value
					}
				}
			}
		}
	}
	return ""
}

func probePreviewCandidates(candidates []previewCandidate) previewProbeResult {
	if len(candidates) == 0 {
		return previewProbeResult{}
	}
	client := http.Client{Timeout: 5 * time.Second}
	lastErr := ""
	for _, candidate := range candidates {
		request, err := http.NewRequest(http.MethodGet, candidate.URL, nil)
		if err != nil {
			lastErr = fmt.Sprintf("%s: %v", candidate.URL, err)
			continue
		}
		response, err := client.Do(request)
		if err != nil {
			lastErr = fmt.Sprintf("%s: %v", candidate.URL, err)
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4*1024))
		_ = response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 500 {
			return previewProbeResult{URL: candidate.URL, Source: candidate.Source, OK: true, StatusCode: response.StatusCode}
		}
		lastErr = fmt.Sprintf("%s returned HTTP %d", candidate.URL, response.StatusCode)
	}
	return previewProbeResult{Error: firstNonEmpty(lastErr, "No preview candidate opened successfully.")}
}

func uniquePreviewCandidates(candidates []previewCandidate) []previewCandidate {
	seen := map[string]bool{}
	result := []previewCandidate{}
	for _, candidate := range candidates {
		candidate.URL = normalizePreviewURL(candidate.URL)
		if candidate.URL == "" || seen[candidate.URL] {
			continue
		}
		seen[candidate.URL] = true
		result = append(result, candidate)
	}
	return result
}

func normalizePreviewURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "http://" + value
}

func normalizeURLPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func kubectlJSONItems(kubectlPath string, project project, resource string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	args := buildKubectlArgs(project, "get", resource, "-o", "json")
	output, err := exec.CommandContext(ctx, kubectlPath, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl get %s: %s", resource, formatCommandFailure(err, output))
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(output, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func podReady(status map[string]any) bool {
	for _, condition := range sliceValue(status, "conditions") {
		conditionMap := mapAny(condition)
		if stringValue(conditionMap, "type") == "Ready" && stringValue(conditionMap, "status") == "True" {
			return true
		}
	}
	return false
}

func mapAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func mapValue(values map[string]any, key string) map[string]any {
	return mapAny(values[key])
}

func sliceValue(values map[string]any, key string) []any {
	if typed, ok := values[key].([]any); ok {
		return typed
	}
	return nil
}

func stringValue(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func stringSliceValue(values map[string]any, key string) []string {
	result := []string{}
	for _, item := range sliceValue(values, key) {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func intValue(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	default:
		return 0
	}
}

type reviewEvidenceArtifact struct {
	AgentSummary     string                  `json:"agentSummary"`
	CommandsRun      []reviewEvidenceCommand `json:"commandsRun"`
	Tests            []reviewEvidenceCheck   `json:"tests"`
	BuildResult      reviewEvidenceResult    `json:"buildResult"`
	DeploymentResult reviewEvidenceResult    `json:"deploymentResult"`
	Risks            []string                `json:"risks"`
	FollowUps        []string                `json:"followUps"`
}

func (a *app) recordSessionReviewEvidence(session agentSession, project project, changeNode *issueChangeNode, success bool) *sessionReviewEvidence {
	evidence, err := a.buildSessionReviewEvidence(session, project, changeNode, success)
	if err != nil {
		a.appendSessionLog(session.ID, "system", "Review evidence snapshot could not be created: "+err.Error())
		return nil
	}
	if err := a.storeSessionReviewEvidence(evidence); err != nil {
		a.appendSessionLog(session.ID, "system", "Review evidence snapshot could not be stored: "+err.Error())
		return nil
	}
	return &evidence
}

func (a *app) buildSessionReviewEvidence(session agentSession, project project, changeNode *issueChangeNode, success bool) (sessionReviewEvidence, error) {
	logs, err := a.listSessionLogs(session.ID)
	if err != nil {
		return sessionReviewEvidence{}, err
	}
	artifact := readReviewEvidenceArtifact(session)
	commands := normalizeReviewCommands(artifact.CommandsRun, session.Status)
	if len(commands) == 0 {
		commands = deriveReviewCommands(logs, session)
	}

	tests := artifact.Tests
	if len(tests) == 0 {
		tests = reviewChecksFromCommands(commands, "test")
	}

	buildResult := artifact.BuildResult
	if buildResult.Status == "" {
		buildResult = reviewResultFromCommands(commands, "build", "Build result was not reported.")
	}

	deploymentResult := artifact.DeploymentResult
	if deploymentResult.Status == "" {
		deploymentResult = a.deploymentReviewResult(session, success)
	}

	agentSummary := strings.TrimSpace(artifact.AgentSummary)
	if agentSummary == "" {
		agentSummary = latestAgentSummary(logs)
	}

	sourceSessionID := firstNonEmpty(session.SourceSessionID, session.ID)
	sourceCommitSHA := strings.TrimSpace(session.SourceCommitSHA)
	branch := strings.TrimSpace(session.Branch)
	if changeNode != nil {
		sourceSessionID = firstNonEmpty(sourceSessionID, changeNode.SessionID)
		sourceCommitSHA = firstNonEmpty(sourceCommitSHA, changeNode.CommitSHA)
		branch = firstNonEmpty(branch, changeNode.Branch)
	}

	review := sessionReviewEvidence{
		ID:               uuid.NewString(),
		IssueID:          session.IssueID,
		SessionID:        session.ID,
		SourceSessionID:  sourceSessionID,
		SourceCommitSHA:  sourceCommitSHA,
		Branch:           branch,
		AgentSummary:     agentSummary,
		CommandsRun:      compactReviewEvidenceCommands(commands),
		Tests:            tests,
		BuildResult:      buildResult,
		DeploymentResult: deploymentResult,
		Risks:            normalizeStringList(artifact.Risks),
		FollowUps:        normalizeStringList(artifact.FollowUps),
		CreatedAt:        nowString(),
		UpdatedAt:        nowString(),
	}

	if environment, err := a.loadIssueTestEnvironment(session.IssueID); err == nil && environment != nil && environment.LastDeploySessionID == session.ID {
		review.PreviewURL = environment.PreviewURL
		review.Cluster = firstNonEmpty(environment.KubeContext, environment.ClusterID)
		review.Namespace = environment.Namespace
		review.NamespaceStatus = environment.NamespaceStatus
		review.CleanupStatus = environment.CleanupStatus
	}
	if review.Cluster == "" || review.Namespace == "" {
		if evidence, err := a.latestDeploymentEvidenceForSession(session.ID); err == nil && evidence != nil {
			review.Cluster = firstNonEmpty(review.Cluster, evidence.Cluster)
			review.Namespace = firstNonEmpty(review.Namespace, evidence.Namespace)
		}
	}
	return review, nil
}

func (a *app) recordSessionFailure(session agentSession, project project, cause error, evidence *deploymentEvidence, phaseOverride, statusOverride string) *sessionFailure {
	failure := a.buildSessionFailure(session, project, cause, evidence, phaseOverride, statusOverride)
	stored, err := a.storeSessionFailure(failure)
	if err != nil {
		a.appendSessionLog(session.ID, "system", "Failure evidence could not be stored: "+err.Error())
		return nil
	}
	return &stored
}

func (a *app) buildSessionFailure(session agentSession, project project, cause error, evidence *deploymentEvidence, phaseOverride, statusOverride string) sessionFailure {
	logs, _ := a.listSessionLogs(session.ID)
	commands := deriveReviewCommands(logs, session)
	failedCommand := latestFailedReviewCommand(commands)
	causeText := ""
	if cause != nil {
		causeText = cause.Error()
	}
	evidenceSummary := ""
	evidenceDetails := ""
	if evidence != nil {
		evidenceSummary = evidence.Summary
		evidenceDetails = evidence.Details
	}
	errorText := strings.TrimSpace(strings.Join(nonEmptyStrings(
		causeText,
		failedCommand.Summary,
		evidenceSummary,
		latestFailureLogMessage(logs),
		evidenceDetails,
	), "\n"))
	cluster := strings.TrimSpace(project.KubeContext)
	namespace := strings.TrimSpace(project.Namespace)
	if evidence != nil {
		cluster = firstNonEmpty(evidence.Cluster, cluster)
		namespace = firstNonEmpty(evidence.Namespace, namespace)
	}
	if environment, err := a.loadIssueTestEnvironment(session.IssueID); err == nil && environment != nil {
		cluster = firstNonEmpty(cluster, environment.KubeContext, environment.ClusterID)
		namespace = firstNonEmpty(namespace, environment.Namespace)
	}
	resourceKind, resourceName := failureResourceFromEvidence(evidenceDetails)
	failedCommandText := strings.TrimSpace(failedCommand.Command)
	if failedCommandText == "" && session.RuntimeMode != "codex-app-server" {
		failedCommandText = strings.TrimSpace(session.Command)
	}
	phase := normalizeFailurePhase(firstNonEmpty(phaseOverride, inferFailurePhase(failedCommandText, errorText)))
	status := normalizeFailureStatus(firstNonEmpty(statusOverride, failureStatusForSession(session)))
	now := nowString()
	return sessionFailure{
		ID:            uuid.NewString(),
		IssueID:       session.IssueID,
		SessionID:     session.ID,
		Phase:         phase,
		Status:        status,
		FailedCommand: truncate(failedCommandText, 1000),
		ErrorSummary:  truncate(firstNonEmpty(failedCommand.Summary, causeText, evidenceSummary, latestFailureLogMessage(logs), "Session did not finish successfully."), 600),
		ErrorExcerpt:  truncate(errorExcerpt(errorText), 2000),
		Cluster:       cluster,
		Namespace:     namespace,
		ResourceKind:  resourceKind,
		ResourceName:  resourceName,
		EvidenceID:    evidenceID(evidence),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func readReviewEvidenceArtifact(session agentSession) reviewEvidenceArtifact {
	artifactPath := ""
	if strings.TrimSpace(session.ArtifactDir) != "" {
		artifactPath = filepath.Join(session.ArtifactDir, "review-evidence.json")
	} else if strings.TrimSpace(session.Workdir) != "" {
		artifactPath = filepath.Join(session.Workdir, ".mspace", "session", "review-evidence.json")
	}
	if artifactPath == "" {
		return reviewEvidenceArtifact{}
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return reviewEvidenceArtifact{}
	}
	var raw struct {
		AgentSummary     string          `json:"agentSummary"`
		CommandsRun      json.RawMessage `json:"commandsRun"`
		Tests            json.RawMessage `json:"tests"`
		BuildResult      json.RawMessage `json:"buildResult"`
		DeploymentResult json.RawMessage `json:"deploymentResult"`
		Risks            json.RawMessage `json:"risks"`
		FollowUps        json.RawMessage `json:"followUps"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return reviewEvidenceArtifact{}
	}
	return reviewEvidenceArtifact{
		AgentSummary:     strings.TrimSpace(raw.AgentSummary),
		CommandsRun:      parseReviewCommandsValue(raw.CommandsRun),
		Tests:            parseReviewChecksValue(raw.Tests),
		BuildResult:      parseReviewResultValue(raw.BuildResult),
		DeploymentResult: parseReviewResultValue(raw.DeploymentResult),
		Risks:            parseReviewStringListValue(raw.Risks),
		FollowUps:        parseReviewStringListValue(raw.FollowUps),
	}
}

func parseReviewCommandsValue(data json.RawMessage) []reviewEvidenceCommand {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var commands []reviewEvidenceCommand
	if err := json.Unmarshal(data, &commands); err == nil {
		return commands
	}
	var commandStrings []string
	if err := json.Unmarshal(data, &commandStrings); err == nil {
		commands = make([]reviewEvidenceCommand, 0, len(commandStrings))
		for _, command := range commandStrings {
			command = strings.TrimSpace(command)
			if command == "" {
				continue
			}
			commands = append(commands, reviewEvidenceCommand{Command: command})
		}
		return commands
	}
	return nil
}

func parseReviewChecksValue(data json.RawMessage) []reviewEvidenceCheck {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var checks []reviewEvidenceCheck
	if err := json.Unmarshal(data, &checks); err == nil {
		return checks
	}
	var checkMap map[string]any
	if err := json.Unmarshal(data, &checkMap); err == nil {
		checks = make([]reviewEvidenceCheck, 0, len(checkMap))
		for name, value := range checkMap {
			summary := reviewEvidenceValueText(value)
			checks = append(checks, reviewEvidenceCheck{
				Name:    strings.TrimSpace(name),
				Status:  inferReviewStatus(summary),
				Summary: truncate(summary, 600),
			})
		}
		return checks
	}
	return nil
}

func parseReviewResultValue(data json.RawMessage) reviewEvidenceResult {
	if len(data) == 0 || string(data) == "null" {
		return reviewEvidenceResult{}
	}
	var result reviewEvidenceResult
	if err := json.Unmarshal(data, &result); err == nil {
		result.Status = normalizeReviewStatus(result.Status)
		result.Summary = strings.TrimSpace(result.Summary)
		result.Details = truncate(strings.TrimSpace(result.Details), 1200)
		return result
	}
	var summary string
	if err := json.Unmarshal(data, &summary); err == nil {
		summary = strings.TrimSpace(summary)
		return reviewEvidenceResult{Status: inferReviewStatus(summary), Summary: summary}
	}
	summary = reviewEvidenceValueText(data)
	return reviewEvidenceResult{Status: inferReviewStatus(summary), Summary: truncate(summary, 600)}
}

func parseReviewStringListValue(data json.RawMessage) []string {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		return normalizeStringList(values)
	}
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		return normalizeStringList([]string{value})
	}
	var valueMap map[string]any
	if err := json.Unmarshal(data, &valueMap); err == nil {
		values = make([]string, 0, len(valueMap))
		for key, value := range valueMap {
			text := strings.TrimSpace(reviewEvidenceValueText(value))
			if text == "" {
				continue
			}
			values = append(values, fmt.Sprintf("%s: %s", key, text))
		}
		return normalizeStringList(values)
	}
	return nil
}

func reviewEvidenceValueText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err == nil {
			return reviewEvidenceValueText(decoded)
		}
		return strings.TrimSpace(string(typed))
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
}

func deriveReviewCommands(logs []sessionLog, session agentSession) []reviewEvidenceCommand {
	commands := []reviewEvidenceCommand{}
	openCommand := -1
	for _, log := range logs {
		if log.Stream != "command" {
			continue
		}
		message := strings.TrimSpace(log.Message)
		if strings.HasPrefix(message, "$ ") {
			command := strings.TrimSpace(strings.TrimPrefix(message, "$ "))
			commands = append(commands, reviewEvidenceCommand{
				Command:   command,
				Status:    "running",
				Category:  commandEvidenceCategory(command),
				CreatedAt: log.CreatedAt,
			})
			openCommand = len(commands) - 1
			continue
		}
		if strings.HasPrefix(message, "Command ") && openCommand >= 0 && openCommand < len(commands) {
			commands[openCommand].Status = commandCompletionStatus(message)
			commands[openCommand].Summary = summarizeCommandOutput(message)
			openCommand = -1
		}
	}
	if len(commands) == 0 && strings.TrimSpace(session.Command) != "" && session.RuntimeMode != "codex-app-server" {
		commands = append(commands, reviewEvidenceCommand{
			Command:  session.Command,
			Status:   statusForCompletedSession(session.Status),
			Category: commandEvidenceCategory(session.Command),
		})
	}
	for i := range commands {
		if commands[i].Status == "running" && (session.Status == "completed" || session.Status == "failed") {
			commands[i].Status = statusForCompletedSession(session.Status)
		}
	}
	return normalizeReviewCommands(commands, session.Status)
}

func latestFailedReviewCommand(commands []reviewEvidenceCommand) reviewEvidenceCommand {
	for index := len(commands) - 1; index >= 0; index-- {
		if normalizeReviewStatus(commands[index].Status) == "failed" {
			return commands[index]
		}
	}
	return reviewEvidenceCommand{}
}

func latestFailureLogMessage(logs []sessionLog) string {
	for index := len(logs) - 1; index >= 0; index-- {
		message := strings.TrimSpace(logs[index].Message)
		if message == "" {
			continue
		}
		lower := strings.ToLower(message)
		if strings.Contains(lower, "failed") || strings.Contains(lower, "error") || strings.Contains(lower, "interrupted") || strings.Contains(lower, "cancelled") {
			return truncate(message, 600)
		}
	}
	return ""
}

func inferFailurePhase(command, text string) string {
	lower := strings.ToLower(strings.TrimSpace(command + "\n" + text))
	switch {
	case strings.Contains(lower, "interrupted") || strings.Contains(lower, "cancelled") || strings.Contains(lower, "canceled") || strings.Contains(lower, "runner restarted"):
		return "agent_interrupted"
	case strings.Contains(lower, "cleanup") || strings.Contains(lower, "delete namespace") || strings.Contains(lower, "cleanup_failed"):
		return "cleanup"
	case strings.Contains(lower, "docker push") || strings.Contains(lower, "image push") || strings.Contains(lower, "push access denied") || strings.Contains(lower, "registry") || strings.Contains(lower, "unauthorized"):
		return "image_push"
	case strings.Contains(lower, "go test") || strings.Contains(lower, "npm test") || strings.Contains(lower, "pnpm test") || strings.Contains(lower, "pytest") || strings.Contains(lower, "vitest") || strings.Contains(lower, "jest") || strings.Contains(lower, "typecheck") || strings.Contains(lower, "tsc") || strings.Contains(lower, "lint"):
		return "test"
	case strings.Contains(lower, "docker build") || strings.Contains(lower, "buildx") || strings.Contains(lower, "go build") || strings.Contains(lower, "npm run build") || strings.Contains(lower, "pnpm build") || strings.Contains(lower, "cargo build"):
		return "build"
	case strings.Contains(lower, "imagepullbackoff") || strings.Contains(lower, "crashloopbackoff") || strings.Contains(lower, "createcontainerconfigerror") || strings.Contains(lower, "pod") || strings.Contains(lower, "unschedulable"):
		return "pod_startup"
	case strings.Contains(lower, "service") || strings.Contains(lower, "ingress") || strings.Contains(lower, "nodeport") || strings.Contains(lower, "endpoint") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "no route"):
		return "network_exposure"
	case strings.Contains(lower, "preview") || strings.Contains(lower, "probe") || strings.Contains(lower, "candidate url") || strings.Contains(lower, "http "):
		return "preview_probe"
	default:
		return "unknown"
	}
}

func normalizeFailurePhase(phase string) string {
	phase = strings.ToLower(strings.TrimSpace(phase))
	switch phase {
	case "build", "test", "image_push", "pod_startup", "network_exposure", "preview_probe", "agent_interrupted", "cleanup":
		return phase
	case "":
		return "unknown"
	default:
		return "unknown"
	}
}

func normalizeFailureStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "open", "retrying", "continued", "resolved", "stopped", "superseded":
		return status
	case "cancelled", "canceled":
		return "stopped"
	default:
		return "open"
	}
}

func failureStatusForSession(session agentSession) string {
	if strings.TrimSpace(session.Status) == "cancelled" {
		return "stopped"
	}
	return "open"
}

func failurePhaseForNamespaceStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "cleanup_failed":
		return "cleanup"
	case "deploy_interrupted":
		return "agent_interrupted"
	case "preview_unverified":
		return "preview_probe"
	case "deploy_failed":
		return "pod_startup"
	default:
		return ""
	}
}

func shouldRecordFailureForNamespaceStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "deploy_failed", "deploy_interrupted", "preview_unverified", "cleanup_failed":
		return true
	default:
		return false
	}
}

func errorExcerpt(text string) string {
	lines := splitNonEmptyLines(text)
	if len(lines) == 0 {
		return ""
	}
	const maxLines = 12
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n")
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func evidenceID(evidence *deploymentEvidence) string {
	if evidence == nil {
		return ""
	}
	return evidence.ID
}

func failureResourceFromEvidence(details string) (string, string) {
	for _, line := range splitNonEmptyLines(details) {
		if strings.HasPrefix(strings.ToUpper(line), "LAST SEEN ") || strings.HasPrefix(strings.ToUpper(line), "NAME ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.EqualFold(fields[1], "Warning") {
			kind, name := splitKubernetesObjectRef(fields[3])
			if kind != "" || name != "" {
				return kind, name
			}
		}
		if len(fields) >= 2 && !strings.EqualFold(fields[0], "NAME") {
			kind, name := splitKubernetesObjectRef(fields[0])
			if kind != "" || name != "" {
				return kind, name
			}
		}
	}
	return "", ""
}

func splitKubernetesObjectRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ""
	}
	if parts := strings.SplitN(ref, "/", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", ref
}

func normalizeReviewCommands(commands []reviewEvidenceCommand, sessionStatus string) []reviewEvidenceCommand {
	result := make([]reviewEvidenceCommand, 0, len(commands))
	for _, command := range commands {
		command.Command = strings.TrimSpace(command.Command)
		if command.Command == "" {
			continue
		}
		command.Status = normalizeReviewStatus(command.Status)
		if command.Status == "" {
			command.Status = statusForCompletedSession(sessionStatus)
		}
		command.Category = strings.TrimSpace(command.Category)
		if command.Category == "" {
			command.Category = commandEvidenceCategory(command.Command)
		}
		command.Summary = truncate(strings.TrimSpace(command.Summary), 600)
		result = append(result, command)
	}
	return result
}

func normalizeReviewStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "success", "succeeded", "ok", "pass":
		return "passed"
	case "failure", "error", "fail":
		return "failed"
	default:
		return status
	}
}

func inferReviewStatus(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "failed") || strings.Contains(lower, " failure") || strings.Contains(lower, "error"):
		return "failed"
	case strings.Contains(lower, "passed") || strings.Contains(lower, "success") || strings.Contains(lower, " ok"):
		return "passed"
	default:
		return ""
	}
}

func compactReviewEvidenceCommands(commands []reviewEvidenceCommand) []reviewEvidenceCommand {
	commands = normalizeReviewCommands(commands, "")
	latestPassedByCategory := map[string]int{}
	for index, command := range commands {
		if command.Status == "passed" {
			latestPassedByCategory[command.Category] = index
		}
	}
	result := []reviewEvidenceCommand{}
	positions := map[string]int{}
	for index, command := range commands {
		if !reviewCommandIsEvidenceWorthy(command) {
			continue
		}
		if command.Category != "command" {
			if latestPassedIndex, ok := latestPassedByCategory[command.Category]; ok && index < latestPassedIndex && command.Status != "passed" {
				continue
			}
		}
		key := reviewCommandCompactKey(command)
		if index, ok := positions[key]; ok {
			result[index] = command
			continue
		}
		positions[key] = len(result)
		result = append(result, command)
	}
	const maxReviewCommands = 12
	if len(result) > maxReviewCommands {
		return result[len(result)-maxReviewCommands:]
	}
	return result
}

func reviewCommandIsEvidenceWorthy(command reviewEvidenceCommand) bool {
	switch command.Category {
	case "test", "build", "deploy":
		return true
	}
	return reviewCommandIsStateChange(command.Command)
}

func reviewCommandIsStateChange(command string) bool {
	lower := strings.ToLower(command)
	return strings.Contains(lower, "git commit") ||
		strings.Contains(lower, "git diff --check") ||
		strings.Contains(lower, "git status") ||
		strings.Contains(lower, "npm ci") ||
		strings.Contains(lower, "pnpm install") ||
		strings.Contains(lower, "playwright") ||
		(strings.Contains(lower, "curl") && strings.Contains(lower, "/api/issues/") && strings.Contains(lower, "-x put"))
}

func reviewCommandCompactKey(command reviewEvidenceCommand) string {
	return command.Category + "\x00" + normalizeReviewCommandText(command.Command)
}

func normalizeReviewCommandText(command string) string {
	command = strings.TrimSpace(command)
	const zshPrefix = "/bin/zsh -lc "
	if strings.HasPrefix(command, zshPrefix) {
		command = strings.TrimSpace(strings.TrimPrefix(command, zshPrefix))
	}
	if len(command) >= 2 {
		first := command[0]
		last := command[len(command)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			command = command[1 : len(command)-1]
		}
	}
	return strings.Join(strings.Fields(command), " ")
}

func commandCompletionStatus(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "exit 0") || strings.Contains(lower, "completed") {
		return "passed"
	}
	if strings.Contains(lower, "exit ") || strings.Contains(lower, "failed") {
		return "failed"
	}
	return "completed"
}

func summarizeCommandOutput(message string) string {
	lines := splitNonEmptyLines(message)
	if len(lines) <= 1 {
		return strings.TrimSpace(message)
	}
	return truncate(strings.Join(lines[1:], "\n"), 600)
}

func statusForCompletedSession(status string) string {
	switch status {
	case "completed":
		return "passed"
	case "failed":
		return "failed"
	case "cancelled":
		return "cancelled"
	default:
		return strings.TrimSpace(status)
	}
}

func commandEvidenceCategory(command string) string {
	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, " test") || strings.HasPrefix(lower, "test") || strings.Contains(lower, "vitest") || strings.Contains(lower, "jest") || strings.Contains(lower, "pytest") || strings.Contains(lower, "go test") || strings.Contains(lower, "cargo test") || strings.Contains(lower, "typecheck") || strings.Contains(lower, "tsc") || strings.Contains(lower, " lint") || strings.Contains(lower, "eslint"):
		return "test"
	case strings.Contains(lower, "build") || strings.Contains(lower, "docker build") || strings.Contains(lower, "buildx") || strings.Contains(lower, "go build") || strings.Contains(lower, "cargo build"):
		return "build"
	case strings.Contains(lower, "kubectl") || strings.Contains(lower, "helm") || strings.Contains(lower, "rollout") || strings.Contains(lower, "docker push") || strings.Contains(lower, "curl "):
		return "deploy"
	default:
		return "command"
	}
}

func reviewChecksFromCommands(commands []reviewEvidenceCommand, category string) []reviewEvidenceCheck {
	commands = normalizeReviewCommands(commands, "")
	checksByCommand := map[string]reviewEvidenceCheck{}
	order := []string{}
	for _, command := range commands {
		if command.Category != category {
			continue
		}
		key := normalizeReviewCommandText(command.Command)
		if _, ok := checksByCommand[key]; !ok {
			order = append(order, key)
		}
		checksByCommand[key] = reviewEvidenceCheck{
			Name:    command.Command,
			Status:  command.Status,
			Summary: command.Summary,
		}
	}
	checks := make([]reviewEvidenceCheck, 0, len(order))
	for _, key := range order {
		checks = append(checks, checksByCommand[key])
	}
	return checks
}

func reviewResultFromCommands(commands []reviewEvidenceCommand, category, emptySummary string) reviewEvidenceResult {
	commands = normalizeReviewCommands(commands, "")
	matches := []reviewEvidenceCommand{}
	for _, command := range commands {
		if command.Category == category && command.Status != "running" {
			matches = append(matches, command)
		}
	}
	if len(matches) == 0 {
		return reviewEvidenceResult{Status: "not_reported", Summary: emptySummary}
	}
	latest := matches[len(matches)-1]
	status := firstNonEmpty(latest.Status, "completed")
	failedAttempts := 0
	for _, command := range matches[:len(matches)-1] {
		if command.Status == "failed" {
			failedAttempts++
		}
	}
	summary := fmt.Sprintf("Latest %s command %s.", category, status)
	if status == "failed" {
		summary = fmt.Sprintf("Latest %s command failed.", category)
	} else if failedAttempts > 0 {
		summary += fmt.Sprintf(" %d earlier failed attempt(s) kept in session logs.", failedAttempts)
	}
	return reviewEvidenceResult{Status: status, Summary: summary, Details: truncate(latest.Summary, 1200)}
}

func (a *app) deploymentReviewResult(session agentSession, success bool) reviewEvidenceResult {
	if evidence, err := a.latestDeploymentEvidenceForSession(session.ID); err == nil && evidence != nil {
		status := "passed"
		lowerSummary := strings.ToLower(evidence.Summary)
		if strings.Contains(lowerSummary, "but no preview") || strings.Contains(lowerSummary, "unverified") {
			status = "warning"
		}
		if strings.Contains(lowerSummary, "failed") || strings.Contains(lowerSummary, "could not") || strings.Contains(lowerSummary, "interrupted") {
			status = "failed"
		}
		return reviewEvidenceResult{Status: status, Summary: evidence.Summary, Details: truncate(evidence.Details, 1200)}
	}
	if a.isIssueTestDeploySession(session) {
		if success {
			return reviewEvidenceResult{Status: "passed", Summary: "Test deployment session completed."}
		}
		return reviewEvidenceResult{Status: "failed", Summary: "Test deployment session failed."}
	}
	return reviewEvidenceResult{Status: "not_reported", Summary: "Deployment result was not reported."}
}

func latestAgentSummary(logs []sessionLog) string {
	for i := len(logs) - 1; i >= 0; i-- {
		if logs[i].Stream == "agent" && strings.TrimSpace(logs[i].Message) != "" {
			return truncate(strings.TrimSpace(logs[i].Message), 1600)
		}
	}
	return ""
}

func normalizeStringList(values []string) []string {
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func (a *app) latestDeploymentEvidenceForSession(sessionID string) (*deploymentEvidence, error) {
	row := a.db.QueryRow(`
		SELECT id, issue_id, session_id, cluster, namespace, summary, details, created_at
		FROM deployment_evidence
		WHERE session_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, sessionID)
	var evidence deploymentEvidence
	if err := row.Scan(&evidence.ID, &evidence.IssueID, &evidence.SessionID, &evidence.Cluster, &evidence.Namespace, &evidence.Summary, &evidence.Details, &evidence.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &evidence, nil
}

func (a *app) loadIssue(issueID string) (issue, error) {
	var item issue
	row := a.db.QueryRow(`
		SELECT id, project_id, COALESCE(parent_issue_id, ''), sort_order, title, body, status, close_reason, triage_status, assignee, assignee_type, creator_name, creator_avatar_url, environment_url, created_at, updated_at
		FROM issues
		WHERE id = ?
	`, issueID)
	err := row.Scan(&item.ID, &item.ProjectID, &item.ParentIssueID, &item.SortOrder, &item.Title, &item.Body, &item.Status, &item.CloseReason, &item.TriageStatus, &item.Assignee, &item.AssigneeType, &item.CreatorName, &item.CreatorAvatar, &item.EnvironmentURL, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (a *app) loadIssueListItem(issueID string) (issueListItem, error) {
	var item issueListItem
	row := a.db.QueryRow(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			COALESCE(i.parent_issue_id, '') AS parent_issue_id,
			i.sort_order,
			i.title,
			i.body,
			i.status,
			i.close_reason,
			i.triage_status,
			i.assignee,
			i.assignee_type,
			COALESCE(ii.unread, 0) AS unread,
			COUNT(DISTINCT s.id) AS session_count,
			COUNT(DISTINCT child.id) AS child_issue_count,
			COUNT(DISTINCT CASE WHEN child.status IN ('closed', 'completed') THEN child.id END) AS completed_child_issue_count,
			i.updated_at,
			i.created_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN inbox_items ii ON ii.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		LEFT JOIN issues child ON child.parent_issue_id = i.id
		WHERE i.id = ?
		GROUP BY i.id
	`, issueID)
	var unread int
	if err := row.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.ParentIssueID, &item.SortOrder, &item.Title, &item.Body, &item.Status, &item.CloseReason, &item.TriageStatus, &item.Assignee, &item.AssigneeType, &unread, &item.SessionCount, &item.ChildIssueCount, &item.CompletedChildIssueCount, &item.UpdatedAt, &item.CreatedAt); err != nil {
		return item, err
	}
	item.Unread = unread == 1
	labels, err := a.listIssueLabels(item.ID)
	if err != nil {
		return item, err
	}
	item.Labels = labels
	return item, nil
}

func (a *app) listChildIssues(parentIssueID string) ([]issueListItem, error) {
	rows, err := a.db.Query(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			COALESCE(i.parent_issue_id, '') AS parent_issue_id,
			i.sort_order,
			i.title,
			i.body,
			i.status,
			i.close_reason,
			i.triage_status,
			i.assignee,
			i.assignee_type,
			COALESCE(ii.unread, 0) AS unread,
			COUNT(DISTINCT s.id) AS session_count,
			COUNT(DISTINCT child.id) AS child_issue_count,
			COUNT(DISTINCT CASE WHEN child.status IN ('closed', 'completed') THEN child.id END) AS completed_child_issue_count,
			i.updated_at,
			i.created_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN inbox_items ii ON ii.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		LEFT JOIN issues child ON child.parent_issue_id = i.id
		WHERE i.parent_issue_id = ?
		GROUP BY i.id
		ORDER BY i.sort_order ASC, i.created_at ASC
	`, parentIssueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]issueListItem, 0)
	for rows.Next() {
		var item issueListItem
		var unread int
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.ParentIssueID, &item.SortOrder, &item.Title, &item.Body, &item.Status, &item.CloseReason, &item.TriageStatus, &item.Assignee, &item.AssigneeType, &unread, &item.SessionCount, &item.ChildIssueCount, &item.CompletedChildIssueCount, &item.UpdatedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Unread = unread == 1
		labels, err := a.listIssueLabels(item.ID)
		if err != nil {
			return nil, err
		}
		item.Labels = labels
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *app) loadIssueDetail(issueID string) (issueDetail, error) {
	return a.loadIssueDetailForViewer(issueID, "")
}

func (a *app) loadIssueDetailForViewer(issueID, viewerUserID string) (issueDetail, error) {
	var detail issueDetail
	row := a.db.QueryRow(`
		SELECT i.id, i.project_id, COALESCE(i.parent_issue_id, ''), i.sort_order, i.title, i.body, i.status, i.close_reason, i.triage_status, i.assignee, i.assignee_type, i.creator_name, i.creator_avatar_url, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.source_type, p.remote_url, p.git_provider, p.git_owner, p.git_repo, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.kubeconfig_path, p.namespace, p.image_registry_prefix, p.preview_domain, p.ingress_class, p.node_host, p.default_cluster_id,
		       COALESCE(r.status, 'empty'), COALESCE(r.source, ''), COALESCE(r.source_session_id, ''), COALESCE(r.updated_at, ''),
		       p.created_at, p.updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN project_runbooks r ON r.project_id = p.id
		WHERE i.id = ?
	`, issueID)
	if err := row.Scan(
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.ParentIssueID, &detail.Issue.SortOrder, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.CloseReason, &detail.Issue.TriageStatus, &detail.Issue.Assignee, &detail.Issue.AssigneeType, &detail.Issue.CreatorName, &detail.Issue.CreatorAvatar, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.SourceType, &detail.Project.RemoteURL, &detail.Project.GitProvider, &detail.Project.GitOwner, &detail.Project.GitRepo, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.KubeconfigPath, &detail.Project.Namespace, &detail.Project.ImageRegistryPrefix, &detail.Project.PreviewDomain, &detail.Project.IngressClass, &detail.Project.NodeHost, &detail.Project.DefaultClusterID, &detail.Project.RunbookStatus, &detail.Project.RunbookSource, &detail.Project.RunbookSourceSessionID, &detail.Project.RunbookUpdatedAt, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
	); err != nil {
		return detail, err
	}

	children, err := a.listChildIssues(issueID)
	if err != nil {
		return detail, err
	}
	detail.ChildIssues = children

	testEnvironment, err := a.loadIssueTestEnvironment(issueID)
	if err != nil {
		return detail, err
	}
	detail.TestEnvironment = testEnvironment

	labels, err := a.listIssueLabels(issueID)
	if err != nil {
		return detail, err
	}
	detail.Labels = labels

	comments, err := a.listCommentsForViewer(issueID, viewerUserID)
	if err != nil {
		return detail, err
	}
	detail.Comments = comments

	sessions, err := a.listSessions(issueID)
	if err != nil {
		return detail, err
	}
	detail.Sessions = sessions

	evidence, err := a.listEvidence(issueID)
	if err != nil {
		return detail, err
	}
	detail.Evidence = evidence

	failures, err := a.listSessionFailures(issueID)
	if err != nil {
		return detail, err
	}
	detail.Failures = failures

	changeNodes, err := a.listIssueChangeNodes(issueID, detail.Project)
	if err != nil {
		return detail, err
	}
	detail.ChangeNodes = changeNodes

	reviewEvidence, err := a.listSessionReviewEvidence(issueID)
	if err != nil {
		return detail, err
	}
	detail.ReviewEvidence = reviewEvidence

	handoffs, err := a.listIssueHandoffs(issueID)
	if err != nil {
		return detail, err
	}
	detail.Handoffs = handoffs

	return detail, nil
}

func (a *app) loadProject(projectID string) (project, error) {
	var project project
	row := a.db.QueryRow(`
		SELECT
			p.id,
			p.name,
			p.repo_path,
			p.source_type,
			p.remote_url,
			p.git_provider,
			p.git_owner,
			p.git_repo,
			p.default_branch,
			p.deploy_command,
			p.validation_command,
			p.kube_context,
			p.kubeconfig_path,
			p.namespace,
			p.image_registry_prefix,
			p.preview_domain,
			p.ingress_class,
			p.node_host,
			p.default_cluster_id,
			COALESCE(r.status, 'empty') AS runbook_status,
			COALESCE(r.source, '') AS runbook_source,
			COALESCE(r.source_session_id, '') AS runbook_source_session_id,
			COALESCE(r.updated_at, '') AS runbook_updated_at,
			COUNT(DISTINCT i.id) AS issue_count,
			COUNT(DISTINCT s.id) AS session_count,
			MAX(i.updated_at) AS latest_issue_updated_at,
			p.created_at,
			p.updated_at
		FROM projects p
		LEFT JOIN project_runbooks r ON r.project_id = p.id
		LEFT JOIN issues i ON i.project_id = p.id AND COALESCE(i.parent_issue_id, '') = ''
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		WHERE p.id = ?
		GROUP BY p.id
	`, projectID)
	var latestIssueUpdatedAt sql.NullString
	err := row.Scan(
		&project.ID,
		&project.Name,
		&project.RepoPath,
		&project.SourceType,
		&project.RemoteURL,
		&project.GitProvider,
		&project.GitOwner,
		&project.GitRepo,
		&project.DefaultBranch,
		&project.DeployCommand,
		&project.ValidationCommand,
		&project.KubeContext,
		&project.KubeconfigPath,
		&project.Namespace,
		&project.ImageRegistryPrefix,
		&project.PreviewDomain,
		&project.IngressClass,
		&project.NodeHost,
		&project.DefaultClusterID,
		&project.RunbookStatus,
		&project.RunbookSource,
		&project.RunbookSourceSessionID,
		&project.RunbookUpdatedAt,
		&project.IssueCount,
		&project.SessionCount,
		&latestIssueUpdatedAt,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if latestIssueUpdatedAt.Valid {
		project.LatestIssueUpdatedAt = latestIssueUpdatedAt.String
	}
	return project, err
}

func (a *app) loadProjectRunbook(projectID string) (projectRunbook, error) {
	var runbook projectRunbook
	err := a.db.QueryRow(`
		SELECT project_id, content, status, source, source_session_id, content_hash, created_at, updated_at
		FROM project_runbooks
		WHERE project_id = ?
	`, projectID).Scan(&runbook.ProjectID, &runbook.Content, &runbook.Status, &runbook.Source, &runbook.SourceSessionID, &runbook.ContentHash, &runbook.CreatedAt, &runbook.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return projectRunbook{ProjectID: projectID, Status: "empty"}, nil
	}
	if runbook.Status == "" {
		runbook.Status = normalizeRunbookStatus("", runbook.Content)
	}
	return runbook, err
}

func (a *app) saveProjectRunbook(projectID, content, status, source, authorName, sourceSessionID string, createRevision bool) (projectRunbook, error) {
	content = normalizeProjectRunbookContent(content)
	if len([]byte(content)) > projectRunbookMaxBytes {
		return projectRunbook{}, fmt.Errorf("project runbook is too large; limit is %d bytes", projectRunbookMaxBytes)
	}
	status = normalizeRunbookStatus(status, content)
	source = strings.TrimSpace(source)
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	contentHash := projectRunbookContentHash(content)
	now := nowString()

	tx, err := a.db.Begin()
	if err != nil {
		return projectRunbook{}, err
	}
	defer tx.Rollback()

	var createdAt string
	err = tx.QueryRow(`SELECT created_at FROM project_runbooks WHERE project_id = ?`, projectID).Scan(&createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		createdAt = now
	} else if err != nil {
		return projectRunbook{}, err
	}

	if _, err := tx.Exec(`
		INSERT INTO project_runbooks (project_id, content, status, source, source_session_id, content_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			content = excluded.content,
			status = excluded.status,
			source = excluded.source,
			source_session_id = excluded.source_session_id,
			content_hash = excluded.content_hash,
			updated_at = excluded.updated_at
	`, projectID, content, status, source, sourceSessionID, contentHash, createdAt, now); err != nil {
		return projectRunbook{}, err
	}

	if createRevision {
		if _, err := tx.Exec(`
			INSERT INTO project_runbook_revisions (id, project_id, session_id, author_type, author_name, content, content_hash, status, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, uuid.NewString(), projectID, sourceSessionID, firstNonEmpty(source, "human"), strings.TrimSpace(authorName), content, contentHash, status, now); err != nil {
			return projectRunbook{}, err
		}
	}

	if _, err := tx.Exec(`UPDATE projects SET updated_at = ? WHERE id = ?`, now, projectID); err != nil {
		return projectRunbook{}, err
	}
	if err := tx.Commit(); err != nil {
		return projectRunbook{}, err
	}
	return a.loadProjectRunbook(projectID)
}

func (a *app) importProjectRunbookArtifact(session agentSession, project project) {
	artifactDir := strings.TrimSpace(session.ArtifactDir)
	if artifactDir == "" {
		artifactDir = filepath.Join(session.Workdir, ".mspace", "session")
	}
	artifactPath := filepath.Join(artifactDir, projectRunbookArtifactName)
	info, err := os.Stat(artifactPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Project runbook artifact could not be inspected: %v", err))
		return
	}
	if info.Size() > projectRunbookMaxBytes {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Project runbook artifact was ignored because it exceeds %d bytes.", projectRunbookMaxBytes))
		return
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Project runbook artifact could not be read: %v", err))
		return
	}
	content := normalizeProjectRunbookContent(string(data))
	if content == "" {
		return
	}
	existing, err := a.loadProjectRunbook(project.ID)
	if err == nil && existing.ContentHash != "" && existing.ContentHash == projectRunbookContentHash(content) {
		a.appendSessionLog(session.ID, "system", "Project runbook artifact matched the current runbook; no update recorded.")
		return
	}
	if _, err := a.saveProjectRunbook(project.ID, content, "learned", "agent", session.AgentProfile, session.ID, true); err != nil {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Project runbook artifact could not be saved: %v", err))
		return
	}
	a.appendSessionLog(session.ID, "system", "Project runbook updated from session artifact.")
}

func writeProjectRunbookPromptSection(builder *strings.Builder, runbook projectRunbook) {
	builder.WriteString("## Project Runbook\n\n")
	if strings.TrimSpace(runbook.Content) == "" {
		builder.WriteString("No runbook has been learned for this project yet. Inspect the repository and run commands as needed. If this session discovers a durable install, start, test, build, image, deploy, health-check, or troubleshooting path, write it to `${MSPACE_SESSION_ARTIFACT_DIR}/project-runbook.md` before finishing.\n\n")
		return
	}
	builder.WriteString(fmt.Sprintf("- Status: %s\n", valueOrUnset(runbook.Status)))
	builder.WriteString(fmt.Sprintf("- Last source: %s\n", valueOrUnset(runbook.Source)))
	builder.WriteString(fmt.Sprintf("- Source session: %s\n", valueOrUnset(runbook.SourceSessionID)))
	builder.WriteString(fmt.Sprintf("- Updated: %s\n\n", valueOrUnset(runbook.UpdatedAt)))
	builder.WriteString("Use this as advisory project memory, not as a guaranteed truth. Verify against the current repository before relying on it. If it is stale or incomplete, write a corrected Markdown runbook to `${MSPACE_SESSION_ARTIFACT_DIR}/project-runbook.md`.\n\n")
	builder.WriteString("```markdown\n")
	builder.WriteString(truncate(runbook.Content, projectRunbookPromptLimit))
	builder.WriteString("\n```\n\n")
}

func normalizeProjectRunbookContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimSpace(content)
}

func normalizeRunbookStatus(status, content string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "learned", "stale", "empty":
		if normalizeProjectRunbookContent(content) == "" {
			return "empty"
		}
		return status
	default:
		if normalizeProjectRunbookContent(content) == "" {
			return "empty"
		}
		return "learned"
	}
}

func projectRunbookContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

func (a *app) listClusters() ([]cluster, error) {
	rows, err := a.db.Query(`
		SELECT
			c.id,
			c.name,
			c.kubeconfig_path,
			c.kube_context,
			c.image_registry_prefix,
			c.exposure_mode,
			c.node_host,
			c.preview_domain,
			c.ingress_class,
			c.status,
			c.last_checked_at,
			COUNT(DISTINCT p.id) AS project_count,
			COUNT(DISTINCT e.issue_id) AS environment_count,
			c.created_at,
			c.updated_at
		FROM clusters c
		LEFT JOIN projects p ON p.default_cluster_id = c.id
		LEFT JOIN issue_test_environments e ON e.cluster_id = c.id
		GROUP BY c.id
		ORDER BY c.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clusters := make([]cluster, 0)
	for rows.Next() {
		var cluster cluster
		if err := rows.Scan(&cluster.ID, &cluster.Name, &cluster.KubeconfigPath, &cluster.KubeContext, &cluster.ImageRegistryPrefix, &cluster.ExposureMode, &cluster.NodeHost, &cluster.PreviewDomain, &cluster.IngressClass, &cluster.Status, &cluster.LastCheckedAt, &cluster.ProjectCount, &cluster.EnvironmentCount, &cluster.CreatedAt, &cluster.UpdatedAt); err != nil {
			return nil, err
		}
		clusters = append(clusters, cluster)
	}
	return clusters, rows.Err()
}

func (a *app) loadCluster(clusterID string) (cluster, error) {
	var cluster cluster
	row := a.db.QueryRow(`
		SELECT
			c.id,
			c.name,
			c.kubeconfig_path,
			c.kube_context,
			c.image_registry_prefix,
			c.exposure_mode,
			c.node_host,
			c.preview_domain,
			c.ingress_class,
			c.status,
			c.last_checked_at,
			COUNT(DISTINCT p.id) AS project_count,
			COUNT(DISTINCT e.issue_id) AS environment_count,
			c.created_at,
			c.updated_at
		FROM clusters c
		LEFT JOIN projects p ON p.default_cluster_id = c.id
		LEFT JOIN issue_test_environments e ON e.cluster_id = c.id
		WHERE c.id = ?
		GROUP BY c.id
	`, clusterID)
	err := row.Scan(&cluster.ID, &cluster.Name, &cluster.KubeconfigPath, &cluster.KubeContext, &cluster.ImageRegistryPrefix, &cluster.ExposureMode, &cluster.NodeHost, &cluster.PreviewDomain, &cluster.IngressClass, &cluster.Status, &cluster.LastCheckedAt, &cluster.ProjectCount, &cluster.EnvironmentCount, &cluster.CreatedAt, &cluster.UpdatedAt)
	return cluster, err
}

func (a *app) insertCluster(cluster cluster) error {
	_, err := a.db.Exec(`
		INSERT INTO clusters (id, name, kubeconfig_path, kube_context, image_registry_prefix, exposure_mode, node_host, preview_domain, ingress_class, status, last_checked_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, cluster.ID, cluster.Name, cluster.KubeconfigPath, cluster.KubeContext, cluster.ImageRegistryPrefix, cluster.ExposureMode, cluster.NodeHost, cluster.PreviewDomain, cluster.IngressClass, cluster.Status, cluster.LastCheckedAt, cluster.CreatedAt, cluster.UpdatedAt)
	return err
}

func (a *app) updateCluster(cluster cluster) error {
	_, err := a.db.Exec(`
		UPDATE clusters
		SET name = ?, kubeconfig_path = ?, kube_context = ?, image_registry_prefix = ?, exposure_mode = ?, node_host = ?, preview_domain = ?, ingress_class = ?, status = ?, last_checked_at = ?, updated_at = ?
		WHERE id = ?
	`, cluster.Name, cluster.KubeconfigPath, cluster.KubeContext, cluster.ImageRegistryPrefix, cluster.ExposureMode, cluster.NodeHost, cluster.PreviewDomain, cluster.IngressClass, cluster.Status, cluster.LastCheckedAt, cluster.UpdatedAt, cluster.ID)
	return err
}

func (a *app) loadClusterByKubeconfig(kubeconfigPath, kubeContext string) (cluster, error) {
	var cluster cluster
	row := a.db.QueryRow(`
		SELECT id, name, kubeconfig_path, kube_context, image_registry_prefix, exposure_mode, node_host, preview_domain, ingress_class, status, last_checked_at, created_at, updated_at
		FROM clusters
		WHERE kubeconfig_path = ? AND kube_context = ?
		ORDER BY updated_at DESC
		LIMIT 1
	`, kubeconfigPath, kubeContext)
	err := row.Scan(&cluster.ID, &cluster.Name, &cluster.KubeconfigPath, &cluster.KubeContext, &cluster.ImageRegistryPrefix, &cluster.ExposureMode, &cluster.NodeHost, &cluster.PreviewDomain, &cluster.IngressClass, &cluster.Status, &cluster.LastCheckedAt, &cluster.CreatedAt, &cluster.UpdatedAt)
	return cluster, err
}

func (a *app) importKubeconfigs(paths []string) (kubeconfigImportResult, error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return kubeconfigImportResult{}, errors.New("kubectl is not available on PATH")
	}

	result := kubeconfigImportResult{
		Imported: []cluster{},
		Skipped:  []kubeconfigImportSkip{},
	}
	for _, kubeconfigPath := range uniqueStrings(paths) {
		normalizedPath, err := normalizeKubeconfigPath(kubeconfigPath)
		if err != nil {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: kubeconfigPath, Reason: err.Error()})
			continue
		}
		contexts, err := kubeconfigContexts(normalizedPath)
		if err != nil {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Reason: err.Error()})
			continue
		}
		if len(contexts) == 0 {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Reason: "no kube contexts found"})
			continue
		}
		for _, kubeContext := range contexts {
			cluster, err := a.importKubeconfigContext(normalizedPath, kubeContext)
			if err != nil {
				result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Context: kubeContext, Reason: err.Error()})
				continue
			}
			result.Imported = append(result.Imported, cluster)
		}
	}
	return result, nil
}

func (a *app) importKubeconfigContext(kubeconfigPath, kubeContext string) (cluster, error) {
	status := "ready"
	if err := validateKubeconfigContext(kubeconfigPath, kubeContext); err != nil {
		status = "unreachable"
	}
	now := nowString()
	existing, err := a.loadClusterByKubeconfig(kubeconfigPath, kubeContext)
	if err == nil {
		existing.Status = status
		existing.LastCheckedAt = now
		existing.UpdatedAt = now
		if existing.ImageRegistryPrefix == "" {
			existing.ImageRegistryPrefix = defaultImportedClusterImageRegistryPrefix
		}
		if existing.ExposureMode == "" {
			existing.ExposureMode = "nodeport"
		}
		if err := a.updateCluster(existing); err != nil {
			return cluster{}, err
		}
		return a.loadCluster(existing.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return cluster{}, err
	}

	importedCluster := cluster{
		ID:                  uuid.NewString(),
		Name:                importedClusterName(kubeconfigPath, kubeContext),
		KubeconfigPath:      kubeconfigPath,
		KubeContext:         kubeContext,
		ImageRegistryPrefix: defaultImportedClusterImageRegistryPrefix,
		ExposureMode:        "nodeport",
		Status:              status,
		LastCheckedAt:       now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := a.insertCluster(importedCluster); err != nil {
		return cluster{}, err
	}
	return a.loadCluster(importedCluster.ID)
}

func (a *app) prepareSessionWorkspace(session agentSession, project project) (agentSession, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return session, errors.New("git is not available on PATH")
	}
	if err := ensureGitRepository(gitPath, project.RepoPath); err != nil {
		return session, err
	}

	workdir := plannedSessionWorkdir(a.workdir, project.ID, session.ID)
	if err := os.MkdirAll(filepath.Dir(workdir), 0o755); err != nil {
		return session, fmt.Errorf("create workdir parent: %w", err)
	}
	if _, err := os.Stat(workdir); err == nil {
		return session, fmt.Errorf("session workdir already exists: %s", workdir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return session, fmt.Errorf("inspect session workdir: %w", err)
	}

	if sourceCommitSHA := strings.TrimSpace(session.SourceCommitSHA); sourceCommitSHA != "" {
		if err := gitCommitExists(gitPath, project.RepoPath, sourceCommitSHA); err != nil {
			return session, err
		}
		output, err := exec.Command(gitPath, "-C", project.RepoPath, "worktree", "add", "--detach", workdir, sourceCommitSHA).CombinedOutput()
		if err != nil {
			return session, fmt.Errorf("create deploy worktree from source commit %q: %s", shortCommitSHA(sourceCommitSHA), formatCommandFailure(err, output))
		}
		session.Workdir = workdir
		return session, nil
	}

	branchExists, err := gitRefExists(gitPath, project.RepoPath, "refs/heads/"+session.Branch)
	if err != nil {
		return session, err
	}
	if branchExists {
		output, err := exec.Command(gitPath, "-C", project.RepoPath, "worktree", "add", workdir, session.Branch).CombinedOutput()
		if err != nil {
			return session, fmt.Errorf("attach existing branch %q: %s", session.Branch, formatCommandFailure(err, output))
		}
	} else {
		baseRef, err := resolveBaseRef(gitPath, project.RepoPath, project.DefaultBranch)
		if err != nil {
			return session, err
		}
		output, err := exec.Command(gitPath, "-C", project.RepoPath, "worktree", "add", "--detach", workdir, baseRef).CombinedOutput()
		if err != nil {
			return session, fmt.Errorf("create worktree from %q: %s", baseRef, formatCommandFailure(err, output))
		}
		output, err = exec.Command(gitPath, "-C", workdir, "checkout", "-b", session.Branch).CombinedOutput()
		if err != nil {
			_ = removeWorktree(gitPath, project.RepoPath, workdir)
			return session, fmt.Errorf("create branch %q: %s", session.Branch, formatCommandFailure(err, output))
		}
	}

	session.Workdir = workdir
	return session, nil
}

func (a *app) loadSessionDetail(sessionID string) (sessionDetail, error) {
	detail := sessionDetail{
		Logs:     []sessionLog{},
		Evidence: []deploymentEvidence{},
		Workspace: workspaceSnapshot{
			StatusLines: []string{},
			Changes:     []workspaceChange{},
			Comparison: workspaceComparison{
				CommitLines: []string{},
				Changes:     []workspaceChange{},
			},
		},
	}
	row := a.db.QueryRow(`
		SELECT s.id, s.issue_id, s.provider, s.agent_profile, s.runtime_mode, s.runtime_task_id, s.command, s.status, s.branch, s.workdir, s.codex_thread_id, s.codex_turn_id, s.agent_status, s.artifact_dir, s.source_session_id, s.source_commit_sha, s.trigger_comment_id, s.agent_token, s.cleanup_status, s.cleaned_at, s.created_at, s.updated_at,
		       i.id, i.project_id, COALESCE(i.parent_issue_id, ''), i.sort_order, i.title, i.body, i.status, i.close_reason, i.triage_status, i.assignee, i.assignee_type, i.creator_name, i.creator_avatar_url, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.source_type, p.remote_url, p.git_provider, p.git_owner, p.git_repo, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.kubeconfig_path, p.namespace, p.image_registry_prefix, p.preview_domain, p.ingress_class, p.node_host, p.default_cluster_id,
		       COALESCE(r.status, 'empty'), COALESCE(r.source, ''), COALESCE(r.source_session_id, ''), COALESCE(r.updated_at, ''),
		       p.created_at, p.updated_at
		FROM agent_sessions s
		JOIN issues i ON i.id = s.issue_id
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN project_runbooks r ON r.project_id = p.id
		WHERE s.id = ?
	`, sessionID)
	if err := row.Scan(
		&detail.Session.ID, &detail.Session.IssueID, &detail.Session.Provider, &detail.Session.AgentProfile, &detail.Session.RuntimeMode, &detail.Session.RuntimeTaskID, &detail.Session.Command, &detail.Session.Status, &detail.Session.Branch, &detail.Session.Workdir, &detail.Session.CodexThreadID, &detail.Session.CodexTurnID, &detail.Session.AgentStatus, &detail.Session.ArtifactDir, &detail.Session.SourceSessionID, &detail.Session.SourceCommitSHA, &detail.Session.TriggerCommentID, &detail.Session.AgentToken, &detail.Session.CleanupStatus, &detail.Session.CleanedAt, &detail.Session.CreatedAt, &detail.Session.UpdatedAt,
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.ParentIssueID, &detail.Issue.SortOrder, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.CloseReason, &detail.Issue.TriageStatus, &detail.Issue.Assignee, &detail.Issue.AssigneeType, &detail.Issue.CreatorName, &detail.Issue.CreatorAvatar, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.SourceType, &detail.Project.RemoteURL, &detail.Project.GitProvider, &detail.Project.GitOwner, &detail.Project.GitRepo, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.KubeconfigPath, &detail.Project.Namespace, &detail.Project.ImageRegistryPrefix, &detail.Project.PreviewDomain, &detail.Project.IngressClass, &detail.Project.NodeHost, &detail.Project.DefaultClusterID, &detail.Project.RunbookStatus, &detail.Project.RunbookSource, &detail.Project.RunbookSourceSessionID, &detail.Project.RunbookUpdatedAt, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
	); err != nil {
		return detail, err
	}

	logs, err := a.listSessionLogs(sessionID)
	if err != nil {
		return detail, err
	}
	detail.Logs = logs

	evidenceRows, err := a.db.Query(`
		SELECT id, issue_id, session_id, cluster, namespace, summary, details, created_at
		FROM deployment_evidence
		WHERE session_id = ?
		ORDER BY created_at DESC
	`, sessionID)
	if err != nil {
		return detail, err
	}
	defer evidenceRows.Close()

	for evidenceRows.Next() {
		var evidence deploymentEvidence
		if err := evidenceRows.Scan(&evidence.ID, &evidence.IssueID, &evidence.SessionID, &evidence.Cluster, &evidence.Namespace, &evidence.Summary, &evidence.Details, &evidence.CreatedAt); err != nil {
			return detail, err
		}
		detail.Evidence = append(detail.Evidence, evidence)
	}

	failures, err := a.listSessionFailuresForSession(sessionID)
	if err != nil {
		return detail, err
	}
	detail.Failures = failures

	detail.Workspace = inspectWorkspace(detail.Session.Workdir, detail.Project.DefaultBranch)
	if detail.Session.RuntimeMode == "team" && strings.TrimSpace(detail.Session.Workdir) == "" && strings.TrimSpace(detail.Session.RuntimeTaskID) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		task, err := a.loadTeamRuntimeTask(ctx, detail.Session.RuntimeTaskID)
		cancel()
		if err == nil {
			result, _ := parseRuntimeTaskResult(task.Result)
			detail.Session = a.sessionWithTeamRuntimeResult(detail.Session, result)
			detail.Workspace = inspectWorkspace(detail.Session.Workdir, detail.Project.DefaultBranch)
		}
	}
	if detail.Session.CleanupStatus == "cleaned" && !detail.Workspace.Exists {
		detail.Workspace.Error = "Session worktree has been cleaned up."
	}

	return detail, nil
}

func (a *app) cleanupSessionWorktree(sessionID string) error {
	detail, err := a.loadSessionDetail(sessionID)
	if err != nil {
		return err
	}
	session := detail.Session
	if session.Status == "queued" || session.Status == "running" {
		return errSessionActive
	}
	if session.CleanupStatus == "cleaned" {
		return nil
	}

	if session.Status == "completed" || session.Status == "failed" {
		a.recordSessionReviewEvidence(session, detail.Project, nil, session.Status == "completed")
	}

	workdir, err := validateSessionWorkdir(a.workdir, session.Workdir)
	if err != nil {
		return err
	}

	cleanupMessage := fmt.Sprintf("Session worktree cleaned up: %s", workdir)
	if _, err := os.Stat(workdir); err == nil {
		gitPath, err := exec.LookPath("git")
		if err != nil {
			return errors.New("git is not available on PATH")
		}
		if err := removeWorktree(gitPath, detail.Project.RepoPath, workdir); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		cleanupMessage = fmt.Sprintf("Session worktree was already missing; marked cleaned: %s", workdir)
	} else {
		return fmt.Errorf("inspect session workdir: %w", err)
	}

	cleanedAt := nowString()
	if _, err := a.db.Exec(`
		UPDATE agent_sessions SET cleanup_status = 'cleaned', cleaned_at = ?, updated_at = ? WHERE id = ?
	`, cleanedAt, cleanedAt, session.ID); err != nil {
		return err
	}
	a.appendSessionLog(session.ID, "system", cleanupMessage)
	a.updateReviewEvidenceCleanupStatus(session.ID, "cleaned")
	a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` worktree was cleaned up.", shortID(session.ID)))
	a.broker.publish(session.ID, sessionEvent{Type: "status", Payload: "cleaned"})
	return nil
}

func (a *app) listSessionLogs(sessionID string) ([]sessionLog, error) {
	logRows, err := a.db.Query(`
		SELECT id, session_id, stream, message, created_at
		FROM session_logs
		WHERE session_id = ?
		ORDER BY id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer logRows.Close()

	logs := []sessionLog{}
	for logRows.Next() {
		var log sessionLog
		if err := logRows.Scan(&log.ID, &log.SessionID, &log.Stream, &log.Message, &log.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, logRows.Err()
}

func (a *app) listComments(issueID string) ([]comment, error) {
	return a.listCommentsForViewer(issueID, "")
}

func (a *app) listCommentsForViewer(issueID, viewerUserID string) ([]comment, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, author_type, author_user_id, author_name, author_avatar_url, body, created_at, updated_at, edited_at
		FROM comments
		WHERE issue_id = ?
		ORDER BY created_at DESC, id DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := make([]comment, 0)
	for rows.Next() {
		var c comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorType, &c.AuthorUserID, &c.AuthorName, &c.AuthorAvatar, &c.Body, &c.CreatedAt, &c.UpdatedAt, &c.EditedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := a.attachCommentReactions(issueID, viewerUserID, comments); err != nil {
		return nil, err
	}
	return comments, nil
}

func (a *app) attachCommentReactions(issueID, viewerUserID string, comments []comment) error {
	if len(comments) == 0 {
		return nil
	}
	reactionsByComment := make(map[string]map[string]commentReactionSummary)
	rows, err := a.db.Query(`
		SELECT comment_id, reaction, COUNT(*), SUM(CASE WHEN user_id = ? THEN 1 ELSE 0 END)
		FROM comment_reactions
		WHERE issue_id = ?
		GROUP BY comment_id, reaction
	`, strings.TrimSpace(viewerUserID), issueID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var commentID string
		var summary commentReactionSummary
		var reactedByMe int
		if err := rows.Scan(&commentID, &summary.Reaction, &summary.Count, &reactedByMe); err != nil {
			return err
		}
		summary.ReactedByMe = reactedByMe > 0
		if reactionsByComment[commentID] == nil {
			reactionsByComment[commentID] = map[string]commentReactionSummary{}
		}
		reactionsByComment[commentID][summary.Reaction] = summary
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range comments {
		byReaction := reactionsByComment[comments[index].ID]
		if len(byReaction) == 0 {
			comments[index].Reactions = []commentReactionSummary{}
			continue
		}
		comments[index].Reactions = make([]commentReactionSummary, 0, len(byReaction))
		for _, reaction := range commentReactionOrder {
			if summary, ok := byReaction[reaction]; ok {
				comments[index].Reactions = append(comments[index].Reactions, summary)
			}
		}
	}
	return nil
}

func (a *app) loadComment(issueID, commentID string) (comment, error) {
	var c comment
	err := a.db.QueryRow(`
		SELECT id, issue_id, author_type, author_user_id, author_name, author_avatar_url, body, created_at, updated_at, edited_at
		FROM comments
		WHERE issue_id = ? AND id = ?
	`, issueID, commentID).Scan(&c.ID, &c.IssueID, &c.AuthorType, &c.AuthorUserID, &c.AuthorName, &c.AuthorAvatar, &c.Body, &c.CreatedAt, &c.UpdatedAt, &c.EditedAt)
	return c, err
}

func loadCommentTx(tx *sql.Tx, issueID, commentID string) (comment, error) {
	var c comment
	err := tx.QueryRow(`
		SELECT id, issue_id, author_type, author_user_id, author_name, author_avatar_url, body, created_at, updated_at, edited_at
		FROM comments
		WHERE issue_id = ? AND id = ?
	`, issueID, commentID).Scan(&c.ID, &c.IssueID, &c.AuthorType, &c.AuthorUserID, &c.AuthorName, &c.AuthorAvatar, &c.Body, &c.CreatedAt, &c.UpdatedAt, &c.EditedAt)
	return c, err
}

func ensureCommentOnIssueTx(tx *sql.Tx, issueID, commentID string) error {
	var exists int
	return tx.QueryRow(`
		SELECT 1
		FROM comments
		WHERE issue_id = ? AND id = ?
	`, issueID, commentID).Scan(&exists)
}

func normalizeCommentReaction(value string) (string, error) {
	reaction := strings.ToLower(strings.TrimSpace(value))
	reaction = strings.ReplaceAll(reaction, "-", "_")
	if allowedCommentReactions[reaction] {
		return reaction, nil
	}
	return "", fmt.Errorf("unsupported reaction %q", value)
}

func commentBelongsToActor(c comment, actor issueActor) bool {
	if strings.TrimSpace(c.AuthorUserID) != "" && strings.TrimSpace(actor.UserID) != "" {
		return c.AuthorUserID == actor.UserID
	}
	return strings.EqualFold(normalizeHumanActorName(c.AuthorName), normalizeHumanActorName(actor.Name))
}

func ensureLastIssueCommentTx(tx *sql.Tx, issueID, commentID string) error {
	var lastCommentID string
	if err := tx.QueryRow(`
		SELECT id
		FROM comments
		WHERE issue_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, issueID).Scan(&lastCommentID); err != nil {
		return err
	}
	if lastCommentID != commentID {
		return errors.New("only the latest issue comment can be edited")
	}
	return nil
}

func ensureCommentNotSessionTriggerTx(tx *sql.Tx, commentID string) error {
	var count int
	if err := tx.QueryRow(`
		SELECT COUNT(*)
		FROM agent_sessions
		WHERE trigger_comment_id = ?
	`, commentID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("comment already triggered an agent session")
	}
	return nil
}

func (a *app) listIssueLabels(issueID string) ([]issueLabel, error) {
	rows, err := a.db.Query(`
		SELECT
			il.id,
			il.issue_id,
			il.label_id,
			COALESCE(ld.key, '') AS key,
			COALESCE(ld.name, il.name) AS name,
			COALESCE(ld.dimension, '') AS dimension,
			COALESCE(ld.color, il.color) AS color,
			COALESCE(ld.sort_order, 999) AS sort_order,
			il.created_at
		FROM issue_labels il
		LEFT JOIN issue_label_definitions ld ON ld.id = il.label_id
		WHERE il.issue_id = ?
		ORDER BY
			CASE COALESCE(ld.dimension, '')
				WHEN 'type' THEN 0
				WHEN 'priority' THEN 1
				ELSE 2
			END,
			COALESCE(ld.sort_order, 999) ASC,
			il.created_at ASC,
			name COLLATE NOCASE ASC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	labels := make([]issueLabel, 0)
	for rows.Next() {
		var label issueLabel
		if err := rows.Scan(&label.ID, &label.IssueID, &label.LabelID, &label.Key, &label.Name, &label.Dimension, &label.Color, &label.SortOrder, &label.CreatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func (a *app) listSessions(issueID string) ([]agentSession, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, provider, agent_profile, runtime_mode, runtime_task_id, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, source_session_id, source_commit_sha, trigger_comment_id, agent_token, cleanup_status, cleaned_at, created_at, updated_at
		FROM agent_sessions
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make([]agentSession, 0)
	for rows.Next() {
		var s agentSession
		if err := rows.Scan(&s.ID, &s.IssueID, &s.Provider, &s.AgentProfile, &s.RuntimeMode, &s.RuntimeTaskID, &s.Command, &s.Status, &s.Branch, &s.Workdir, &s.CodexThreadID, &s.CodexTurnID, &s.AgentStatus, &s.ArtifactDir, &s.SourceSessionID, &s.SourceCommitSHA, &s.TriggerCommentID, &s.AgentToken, &s.CleanupStatus, &s.CleanedAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func (a *app) listEvidence(issueID string) ([]deploymentEvidence, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, session_id, cluster, namespace, summary, details, created_at
		FROM deployment_evidence
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evidence := make([]deploymentEvidence, 0)
	for rows.Next() {
		var e deploymentEvidence
		if err := rows.Scan(&e.ID, &e.IssueID, &e.SessionID, &e.Cluster, &e.Namespace, &e.Summary, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		evidence = append(evidence, e)
	}
	return evidence, nil
}

func (a *app) listSessionFailures(issueID string) ([]sessionFailure, error) {
	return a.querySessionFailures(`
		SELECT id, issue_id, session_id, phase, status, failed_command, error_summary, error_excerpt, cluster, namespace, resource_kind, resource_name, evidence_id, review_evidence_id, retry_session_id, continued_session_id, created_at, updated_at
		FROM session_failures
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
}

func (a *app) listSessionFailuresForSession(sessionID string) ([]sessionFailure, error) {
	return a.querySessionFailures(`
		SELECT id, issue_id, session_id, phase, status, failed_command, error_summary, error_excerpt, cluster, namespace, resource_kind, resource_name, evidence_id, review_evidence_id, retry_session_id, continued_session_id, created_at, updated_at
		FROM session_failures
		WHERE session_id = ?
		ORDER BY created_at DESC
	`, sessionID)
}

func (a *app) querySessionFailures(query, arg string) ([]sessionFailure, error) {
	rows, err := a.db.Query(query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	failures := []sessionFailure{}
	for rows.Next() {
		var failure sessionFailure
		if err := rows.Scan(
			&failure.ID,
			&failure.IssueID,
			&failure.SessionID,
			&failure.Phase,
			&failure.Status,
			&failure.FailedCommand,
			&failure.ErrorSummary,
			&failure.ErrorExcerpt,
			&failure.Cluster,
			&failure.Namespace,
			&failure.ResourceKind,
			&failure.ResourceName,
			&failure.EvidenceID,
			&failure.ReviewEvidenceID,
			&failure.RetrySessionID,
			&failure.ContinuedSessionID,
			&failure.CreatedAt,
			&failure.UpdatedAt,
		); err != nil {
			return nil, err
		}
		failures = append(failures, failure)
	}
	return failures, rows.Err()
}

func (a *app) listSessionReviewEvidence(issueID string) ([]sessionReviewEvidence, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, session_id, source_session_id, source_commit_sha, branch, agent_summary, commands_json, tests_json, build_result_json, deployment_result_json, risks_json, follow_ups_json, preview_url, cluster, namespace, namespace_status, cleanup_status, created_at, updated_at
		FROM session_review_evidence
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []sessionReviewEvidence{}
	for rows.Next() {
		var item sessionReviewEvidence
		var commandsJSON, testsJSON, buildJSON, deploymentJSON, risksJSON, followUpsJSON string
		if err := rows.Scan(
			&item.ID,
			&item.IssueID,
			&item.SessionID,
			&item.SourceSessionID,
			&item.SourceCommitSHA,
			&item.Branch,
			&item.AgentSummary,
			&commandsJSON,
			&testsJSON,
			&buildJSON,
			&deploymentJSON,
			&risksJSON,
			&followUpsJSON,
			&item.PreviewURL,
			&item.Cluster,
			&item.Namespace,
			&item.NamespaceStatus,
			&item.CleanupStatus,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		decodeReviewJSON(commandsJSON, &item.CommandsRun)
		decodeReviewJSON(testsJSON, &item.Tests)
		decodeReviewJSON(buildJSON, &item.BuildResult)
		decodeReviewJSON(deploymentJSON, &item.DeploymentResult)
		decodeReviewJSON(risksJSON, &item.Risks)
		decodeReviewJSON(followUpsJSON, &item.FollowUps)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *app) listIssueHandoffs(issueID string) ([]issueHandoff, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error, created_at, updated_at
		FROM issue_handoffs
		WHERE issue_id = ?
		ORDER BY updated_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []issueHandoff{}
	for rows.Next() {
		item, err := scanIssueHandoff(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *app) loadIssueHandoff(issueID, handoffID string) (issueHandoff, error) {
	row := a.db.QueryRow(`
		SELECT id, issue_id, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error, created_at, updated_at
		FROM issue_handoffs
		WHERE issue_id = ? AND id = ?
	`, issueID, handoffID)
	return scanIssueHandoff(row)
}

type issueHandoffScanner interface {
	Scan(dest ...any) error
}

func scanIssueHandoff(scanner issueHandoffScanner) (issueHandoff, error) {
	var item issueHandoff
	var commitsJSON string
	if err := scanner.Scan(
		&item.ID,
		&item.IssueID,
		&item.SourceSessionID,
		&item.SourceCommitSHA,
		&item.Branch,
		&item.HeadCommitSHA,
		&commitsJSON,
		&item.Kind,
		&item.PRURL,
		&item.PRNumber,
		&item.PRState,
		&item.PRTitle,
		&item.PreviewURL,
		&item.EvidenceSummary,
		&item.CreatedVia,
		&item.LastCheckedAt,
		&item.Error,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return issueHandoff{}, err
	}
	decodeReviewJSON(commitsJSON, &item.Commits)
	if item.Commits == nil {
		item.Commits = []issueHandoffCommit{}
	}
	if strings.TrimSpace(item.Kind) == "" {
		if strings.TrimSpace(item.PRURL) != "" {
			item.Kind = "pr"
		} else {
			item.Kind = "branch"
		}
	}
	return item, nil
}

func currentIssuePullRequestHandoff(handoffs []issueHandoff) *issueHandoff {
	for index := range handoffs {
		handoff := &handoffs[index]
		if strings.EqualFold(handoff.Kind, "pr") || strings.TrimSpace(handoff.PRURL) != "" {
			return handoff
		}
	}
	return nil
}

func (a *app) loadCurrentIssuePullRequestHandoff(issueID string) (issueHandoff, bool, error) {
	row := a.db.QueryRow(`
		SELECT id, issue_id, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error, created_at, updated_at
		FROM issue_handoffs
		WHERE issue_id = ? AND (kind = 'pr' OR pr_url != '')
		ORDER BY updated_at DESC
		LIMIT 1
	`, issueID)
	handoff, err := scanIssueHandoff(row)
	if errors.Is(err, sql.ErrNoRows) {
		return issueHandoff{}, false, nil
	}
	if err != nil {
		return issueHandoff{}, false, err
	}
	return handoff, true, nil
}

func (a *app) storeIssueHandoff(handoff issueHandoff) (issueHandoff, error) {
	handoff.Kind = strings.ToLower(strings.TrimSpace(handoff.Kind))
	if handoff.Kind == "" {
		handoff.Kind = "branch"
		if handoff.PRURL != "" {
			handoff.Kind = "pr"
		}
	}
	if handoff.Kind == "pr" && strings.TrimSpace(handoff.IssueID) != "" {
		existing, ok, err := a.loadCurrentIssuePullRequestHandoff(handoff.IssueID)
		if err != nil {
			return handoff, err
		}
		if ok && existing.ID != handoff.ID {
			handoff.ID = existing.ID
			if handoff.CreatedAt == "" {
				handoff.CreatedAt = existing.CreatedAt
			}
		}
	}
	if handoff.ID == "" {
		handoff.ID = uuid.NewString()
	}
	now := nowString()
	if handoff.CreatedAt == "" {
		handoff.CreatedAt = now
	}
	handoff.UpdatedAt = now
	_, err := a.db.Exec(`
		INSERT INTO issue_handoffs (id, issue_id, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			source_session_id = excluded.source_session_id,
			source_commit_sha = excluded.source_commit_sha,
			branch = excluded.branch,
			head_commit_sha = excluded.head_commit_sha,
			commits_json = excluded.commits_json,
			kind = excluded.kind,
			pr_url = excluded.pr_url,
			pr_number = excluded.pr_number,
			pr_state = excluded.pr_state,
			pr_title = excluded.pr_title,
			preview_url = excluded.preview_url,
			evidence_summary = excluded.evidence_summary,
			created_via = excluded.created_via,
			last_checked_at = excluded.last_checked_at,
			error = excluded.error,
			updated_at = excluded.updated_at
	`, handoff.ID, handoff.IssueID, handoff.SourceSessionID, handoff.SourceCommitSHA, handoff.Branch, handoff.HeadCommitSHA, reviewJSON(handoff.Commits, "[]"), handoff.Kind, handoff.PRURL, handoff.PRNumber, handoff.PRState, handoff.PRTitle, handoff.PreviewURL, handoff.EvidenceSummary, handoff.CreatedVia, handoff.LastCheckedAt, handoff.Error, handoff.CreatedAt, handoff.UpdatedAt)
	if err != nil {
		return handoff, err
	}
	_, _ = a.db.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, handoff.UpdatedAt, handoff.IssueID)
	_, _ = a.db.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, handoff.UpdatedAt, handoff.IssueID)
	return handoff, nil
}

func decodeReviewJSON(value string, target any) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	_ = json.Unmarshal([]byte(value), target)
}

func (a *app) listIssueChangeNodes(issueID string, project project) ([]issueChangeNode, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, session_id, commit_sha, branch, subject, files_changed, changes_json, diff_preview, diff_truncated, source, remote_workdir, artifact_dir, created_at
		FROM issue_change_nodes
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := []issueChangeNode{}
	for rows.Next() {
		var node issueChangeNode
		var changesJSON string
		var diffTruncated int
		if err := rows.Scan(&node.ID, &node.IssueID, &node.SessionID, &node.CommitSHA, &node.Branch, &node.Subject, &node.FilesChanged, &changesJSON, &node.DiffPreview, &diffTruncated, &node.Source, &node.RemoteWorkdir, &node.ArtifactDir, &node.CreatedAt); err != nil {
			return nil, err
		}
		node.DiffTruncated = diffTruncated != 0
		decodeReviewJSON(changesJSON, &node.Changes)
		hydrateIssueChangeNode(&node, project)
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (a *app) loadIssueChangeNodeByCommit(issueID, commitSHA string, project project) (*issueChangeNode, error) {
	commitSHA = strings.TrimSpace(commitSHA)
	if commitSHA == "" {
		return nil, nil
	}
	var node issueChangeNode
	var changesJSON string
	var diffTruncated int
	err := a.db.QueryRow(`
		SELECT id, issue_id, session_id, commit_sha, branch, subject, files_changed, changes_json, diff_preview, diff_truncated, source, remote_workdir, artifact_dir, created_at
		FROM issue_change_nodes
		WHERE issue_id = ? AND commit_sha = ?
		LIMIT 1
	`, issueID, commitSHA).Scan(&node.ID, &node.IssueID, &node.SessionID, &node.CommitSHA, &node.Branch, &node.Subject, &node.FilesChanged, &changesJSON, &node.DiffPreview, &diffTruncated, &node.Source, &node.RemoteWorkdir, &node.ArtifactDir, &node.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	node.DiffTruncated = diffTruncated != 0
	decodeReviewJSON(changesJSON, &node.Changes)
	hydrateIssueChangeNode(&node, project)
	return &node, nil
}

func (a *app) loadIssueChangeNodeForDeploy(issueID, sourceCommitSHA, sourceSessionID string, project project) (issueChangeNode, error) {
	sourceCommitSHA = strings.TrimSpace(sourceCommitSHA)
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	nodes, err := a.listIssueChangeNodes(issueID, project)
	if err != nil {
		return issueChangeNode{}, err
	}
	if len(nodes) == 0 {
		return issueChangeNode{}, errors.New("no source commits found for this issue; run an agent session that changes code before deploying")
	}

	for _, node := range nodes {
		commitMatches := sourceCommitSHA == "" || node.CommitSHA == sourceCommitSHA || strings.HasPrefix(node.CommitSHA, sourceCommitSHA)
		sessionMatches := sourceSessionID == "" || node.SessionID == sourceSessionID
		if commitMatches && sessionMatches {
			if node.Error != "" {
				return issueChangeNode{}, fmt.Errorf("source commit cannot be deployed: %s", node.Error)
			}
			return node, nil
		}
	}

	if sourceCommitSHA == "" && sourceSessionID == "" {
		node := nodes[0]
		if node.Error != "" {
			return issueChangeNode{}, fmt.Errorf("source commit cannot be deployed: %s", node.Error)
		}
		return node, nil
	}
	return issueChangeNode{}, errors.New("selected source commit was not found on this issue")
}

func hydrateIssueChangeNode(node *issueChangeNode, project project) {
	node.CommitSHA = strings.TrimSpace(node.CommitSHA)
	node.ShortCommitSHA = shortCommitSHA(node.CommitSHA)
	if node.Changes == nil {
		node.Changes = []workspaceChange{}
	}
	if node.CommitSHA == "" {
		node.Error = "Source commit is empty."
		return
	}
	if strings.TrimSpace(node.Source) == "team-runtime" && strings.TrimSpace(node.DiffPreview) != "" {
		if node.FilesChanged == 0 {
			node.FilesChanged = len(node.Changes)
		}
		return
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		node.Error = "git is not available on PATH."
		return
	}
	if err := ensureGitRepository(gitPath, project.RepoPath); err != nil {
		node.Error = err.Error()
		return
	}
	if err := gitCommitExists(gitPath, project.RepoPath, node.CommitSHA); err != nil {
		node.Error = err.Error()
		return
	}
	if node.Subject == "" {
		if subject, err := runGitReadOnly(gitPath, project.RepoPath, "log", "-1", "--pretty=%s", node.CommitSHA); err == nil {
			node.Subject = subject
		}
	}
	nameStatusOutput, err := runGitReadOnly(gitPath, project.RepoPath, "diff-tree", "--root", "--no-commit-id", "--name-status", "-r", "--find-renames", node.CommitSHA)
	if err != nil {
		node.Error = err.Error()
		return
	}
	node.Changes = []workspaceChange{}
	for _, line := range splitNonEmptyLines(nameStatusOutput) {
		node.Changes = append(node.Changes, parseNameStatusChange(line))
	}
	if node.FilesChanged == 0 {
		node.FilesChanged = len(node.Changes)
	}

	diffPreview, err := runGitReadOnly(gitPath, project.RepoPath, "show", "--stat", "--patch", "--find-renames", "--no-ext-diff", "--format=medium", "--no-color", node.CommitSHA)
	if err != nil {
		node.Error = err.Error()
		return
	}
	node.DiffPreview, node.DiffTruncated = truncateWithFlag(diffPreview, 20000)
}

func (a *app) loadIssueTestEnvironment(issueID string) (*issueTestEnvironment, error) {
	row := a.db.QueryRow(`
		SELECT issue_id, cluster_id, namespace, namespace_status, cleanup_status, preview_url, image_registry_prefix, kubeconfig_path, kube_context, exposure_mode, preview_domain, ingress_class, node_host, last_deploy_session_id, last_cleanup_session_id, source_session_id, source_commit_sha, created_at, updated_at
		FROM issue_test_environments
		WHERE issue_id = ?
	`, issueID)
	var environment issueTestEnvironment
	if err := row.Scan(
		&environment.IssueID,
		&environment.ClusterID,
		&environment.Namespace,
		&environment.NamespaceStatus,
		&environment.CleanupStatus,
		&environment.PreviewURL,
		&environment.ImageRegistryPrefix,
		&environment.KubeconfigPath,
		&environment.KubeContext,
		&environment.ExposureMode,
		&environment.PreviewDomain,
		&environment.IngressClass,
		&environment.NodeHost,
		&environment.LastDeploySessionID,
		&environment.LastCleanupSessionID,
		&environment.SourceSessionID,
		&environment.SourceCommitSHA,
		&environment.CreatedAt,
		&environment.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &environment, nil
}

func (a *app) saveIssueTestEnvironment(environment issueTestEnvironment) error {
	now := nowString()
	if environment.CreatedAt == "" {
		environment.CreatedAt = now
	}
	environment.UpdatedAt = now
	_, err := a.db.Exec(`
		INSERT INTO issue_test_environments (issue_id, cluster_id, namespace, namespace_status, cleanup_status, preview_url, image_registry_prefix, kubeconfig_path, kube_context, exposure_mode, preview_domain, ingress_class, node_host, last_deploy_session_id, last_cleanup_session_id, source_session_id, source_commit_sha, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(issue_id) DO UPDATE SET
			cluster_id = excluded.cluster_id,
			namespace = excluded.namespace,
			namespace_status = excluded.namespace_status,
			cleanup_status = excluded.cleanup_status,
			preview_url = excluded.preview_url,
			image_registry_prefix = excluded.image_registry_prefix,
			kubeconfig_path = excluded.kubeconfig_path,
			kube_context = excluded.kube_context,
			exposure_mode = excluded.exposure_mode,
			preview_domain = excluded.preview_domain,
			ingress_class = excluded.ingress_class,
			node_host = excluded.node_host,
			last_deploy_session_id = excluded.last_deploy_session_id,
			last_cleanup_session_id = excluded.last_cleanup_session_id,
			source_session_id = excluded.source_session_id,
			source_commit_sha = excluded.source_commit_sha,
			updated_at = excluded.updated_at
	`, environment.IssueID, environment.ClusterID, environment.Namespace, environment.NamespaceStatus, environment.CleanupStatus, environment.PreviewURL, environment.ImageRegistryPrefix, environment.KubeconfigPath, environment.KubeContext, environment.ExposureMode, environment.PreviewDomain, environment.IngressClass, environment.NodeHost, environment.LastDeploySessionID, environment.LastCleanupSessionID, environment.SourceSessionID, environment.SourceCommitSHA, environment.CreatedAt, environment.UpdatedAt)
	return err
}

func (a *app) appendSessionLog(sessionID, stream, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	createdAt := nowString()
	_, _ = a.db.Exec(`
		INSERT INTO session_logs (session_id, stream, message, created_at)
		VALUES (?, ?, ?, ?)
	`, sessionID, stream, message, createdAt)
	a.broker.publish(sessionID, sessionEvent{Type: "log", Payload: message, Stream: stream})
}

func (a *app) addSystemComment(issueID, body string) {
	a.addActorComment(issueID, issueActor{Kind: "system", Name: systemActorName}, body)
}

func (a *app) addActorComment(issueID string, actor issueActor, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	createdAt := nowString()
	_, _ = a.db.Exec(`
		INSERT INTO comments (id, issue_id, author_type, author_user_id, author_name, author_avatar_url, body, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), issueID, commentActorType(actor), actor.UserID, commentActorName(actor), actor.AvatarURL, body, createdAt, createdAt)
	_, _ = a.db.Exec(`
		UPDATE issues SET updated_at = ? WHERE id = ?
	`, createdAt, issueID)
	_, _ = a.db.Exec(`
		UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?
	`, createdAt, issueID)
	a.publishInboxEvent(issueID, "updated")
}

func (a *app) updateSessionStatus(sessionID, status string) {
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET status = ?, updated_at = ? WHERE id = ?
	`, status, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: status})
}

func (a *app) sessionWasCancelled(sessionID string) bool {
	var status string
	if err := a.db.QueryRow(`SELECT status FROM agent_sessions WHERE id = ?`, sessionID).Scan(&status); err != nil {
		return false
	}
	return status == "cancelled"
}

func (a *app) sessionCancelActor(sessionID string) issueActor {
	a.mu.Lock()
	defer a.mu.Unlock()
	canceller := a.cancellers[sessionID]
	if canceller.actor.Kind != "" {
		return canceller.actor
	}
	return issueActor{Kind: "system", Name: systemActorName}
}

func (a *app) updateSessionAgentStatus(sessionID, agentStatus string) {
	agentStatus = strings.TrimSpace(agentStatus)
	if agentStatus == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET agent_status = ?, updated_at = ? WHERE id = ?
	`, agentStatus, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: agentStatus})
}

func (a *app) updateSessionCodexThread(sessionID, threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET codex_thread_id = ?, updated_at = ? WHERE id = ?
	`, threadID, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: "codex-thread"})
}

func (a *app) updateSessionCodexTurn(sessionID, turnID string) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET codex_turn_id = ?, updated_at = ? WHERE id = ?
	`, turnID, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: "codex-turn"})
}

func (a *app) updateSessionArtifactDir(sessionID, artifactDir string) {
	artifactDir = strings.TrimSpace(artifactDir)
	if artifactDir == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET artifact_dir = ?, updated_at = ? WHERE id = ?
	`, artifactDir, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: "artifacts"})
}

func (a *app) updateSessionWorkdir(sessionID, workdir string) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET workdir = ?, updated_at = ? WHERE id = ?
	`, workdir, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: "workdir"})
}

func (a *app) updateSessionRuntimeTaskID(sessionID, taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET runtime_task_id = ?, updated_at = ? WHERE id = ?
	`, taskID, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: "runtime-task"})
}

func (a *app) updateIssueStatus(issueID, status string) {
	if err := a.transitionIssueStatus(issueID, status, issueActor{Kind: "system", Name: systemActorName}, ""); err != nil && a.logger != nil {
		a.logger.Warn("update issue status", "issue", issueID, "status", status, "error", err)
	}
}

func (a *app) transitionIssueStatus(issueID, status string, actor issueActor, reason string) error {
	nextStatus := normalizeIssueStatus(status)
	if err := validateIssueStatus(nextStatus); err != nil {
		return err
	}
	existing, err := a.loadIssue(issueID)
	if err != nil {
		return err
	}
	if normalizeIssueStatus(existing.Status) == nextStatus {
		return nil
	}
	if err := authorizeIssueStatusTransition(existing, nextStatus, actor); err != nil {
		return err
	}
	updatedAt := nowString()
	closeReason := ""
	if nextStatus == "cancelled" {
		closeReason = "not_planned"
	}
	topIssueID := issueID
	if existing.ParentIssueID != "" {
		topIssueID = existing.ParentIssueID
	}

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE issues SET status = ?, close_reason = ?, updated_at = ? WHERE id = ?
	`, nextStatus, closeReason, updatedAt, issueID); err != nil {
		return err
	}
	if existing.ParentIssueID == "" {
		if _, err := tx.Exec(`
			UPDATE inbox_items SET status = ?, unread = 1, updated_at = ? WHERE issue_id = ?
		`, nextStatus, updatedAt, issueID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, updatedAt, topIssueID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE inbox_items SET unread = 1, updated_at = ? WHERE issue_id = ?`, updatedAt, topIssueID); err != nil {
			return err
		}
	}
	comment := issueStatusTransitionComment(existing, nextStatus, actor, reason)
	if comment != "" {
		if _, err := tx.Exec(`
			INSERT INTO comments (id, issue_id, author_type, author_user_id, author_name, author_avatar_url, body, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, uuid.NewString(), topIssueID, commentActorType(actor), actor.UserID, commentActorName(actor), actor.AvatarURL, comment, updatedAt, updatedAt); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.publishInboxEvent(topIssueID, "status-"+nextStatus)
	return nil
}

func commentActorType(actor issueActor) string {
	switch actor.Kind {
	case "human", "agent", "system":
		return actor.Kind
	default:
		return "system"
	}
}

func commentActorName(actor issueActor) string {
	name := strings.TrimSpace(actor.Name)
	if name != "" {
		return name
	}
	if actor.Kind == "human" {
		return defaultHumanActorName
	}
	if actor.Kind == "agent" {
		return "agent"
	}
	return systemActorName
}

func authorizeIssueStatusTransition(existing issue, nextStatus string, actor issueActor) error {
	if actor.Kind == "" {
		return errUnauthorized
	}
	if existing.ParentIssueID != "" && (nextStatus == "open" || nextStatus == "closed") {
		if actor.Kind == "agent" && existing.ParentIssueID != actor.IssueID {
			return errForbidden
		}
		return nil
	}

	if existing.ParentIssueID == "" {
		switch actor.Kind {
		case "human":
			switch nextStatus {
			case "closed", "cancelled":
				return nil
			case "changes_requested":
				if isClosedIssueStatusValue(existing.Status) {
					return nil
				}
				return errForbidden
			default:
				return errForbidden
			}
		case "agent":
			if existing.ID != actor.IssueID {
				return errForbidden
			}
			switch nextStatus {
			case "needs_review", "ready_for_test", "blocked":
				return nil
			default:
				return errForbidden
			}
		case "system":
			switch nextStatus {
			case "open", "needs_review", "ready_for_test", "blocked":
				return nil
			default:
				return errForbidden
			}
		default:
			return errForbidden
		}
	}

	if actor.Kind == "agent" {
		return errForbidden
	}
	return nil
}

func issueStatusTransitionComment(existing issue, nextStatus string, actor issueActor, reason string) string {
	actorLabel := strings.TrimSpace(actor.Name)
	if actorLabel == "" {
		actorLabel = actor.Kind
	}
	target := "Issue"
	if existing.ParentIssueID != "" {
		target = fmt.Sprintf("Task `%s`", existing.Title)
	}
	body := fmt.Sprintf("%s status changed from `%s` to `%s` by %s.", target, displayStoredIssueStatus(existing.Status), nextStatus, actorLabel)
	reason = strings.TrimSpace(reason)
	if reason != "" {
		body += "\n\n" + reason
	}
	return body
}

func displayStoredIssueStatus(status string) string {
	if status == "completed" {
		return "closed"
	}
	if status == "review" {
		return "needs_review"
	}
	if status == "testing" || status == "test_in_progress" {
		return "needs_review"
	}
	if status == "queued" || status == "running" || status == "in_progress" {
		return "open"
	}
	return status
}

func (a *app) updateIssueAssignment(issueID, assignee, assigneeType, status string, actor issueActor, reason string) error {
	assignee = strings.TrimSpace(assignee)
	assigneeType = normalizeAssigneeType(assigneeType)
	status = strings.TrimSpace(status)
	if assignee == "" {
		return errors.New("assignee cannot be empty")
	}
	if assigneeType != "human" && assigneeType != "agent" {
		return errors.New("assignee type must be human or agent")
	}
	if status == "" {
		return errors.New("issue status cannot be empty")
	}

	updatedAt := nowString()
	if _, err := a.db.Exec(`
		UPDATE issues SET assignee = ?, assignee_type = ?, updated_at = ? WHERE id = ?
	`, assignee, assigneeType, updatedAt, issueID); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		UPDATE inbox_items SET unread = 1, updated_at = ? WHERE issue_id = ?
	`, updatedAt, issueID); err != nil {
		return err
	}
	a.publishInboxEvent(issueID, "assigned")
	return a.transitionIssueStatus(issueID, status, actor, reason)
}

func (a *app) publishInboxEvent(issueID, status string) {
	a.broker.publish("inbox", sessionEvent{Type: "inbox", Payload: strings.TrimSpace(issueID + " " + status)})
	if status != "read" {
		a.publishControlPlaneIssueEvent(issueID, status)
	}
}

func (a *app) publishControlPlaneIssueEvent(issueID, status string) {
	baseURL, token, workspaceID := a.controlPlaneSession()
	if baseURL == "" || token == "" || workspaceID == "" {
		return
	}
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return
	}
	go a.postControlPlaneIssueEvent(baseURL, token, workspaceID, issueID, status)
}

func (a *app) controlPlaneSession() (string, string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.controlPlaneBaseURL, a.controlPlaneToken, a.controlPlaneWorkspaceID
}

func (a *app) requireHumanActor(w http.ResponseWriter, r *http.Request) (issueActor, bool) {
	actor, err := a.authenticateActor(r)
	if err != nil {
		writeAuthError(w, err)
		return issueActor{}, false
	}
	if actor.Kind != "human" {
		writeAuthError(w, errForbidden)
		return issueActor{}, false
	}
	return actor, true
}

func (a *app) authenticateActor(r *http.Request) (issueActor, error) {
	token := requestBearerToken(r)
	if token == "" {
		return issueActor{}, errUnauthorized
	}
	if actor, ok := a.authenticateAgentToken(token); ok {
		return actor, nil
	}
	return a.authenticateHumanToken(r.Context(), token)
}

func (a *app) authenticateAgentToken(token string) (issueActor, bool) {
	if !strings.HasPrefix(token, agentTokenPrefix) {
		return issueActor{}, false
	}
	var actor issueActor
	var profileName string
	err := a.db.QueryRow(`
		SELECT s.id, s.issue_id, COALESCE(NULLIF(p.name, ''), NULLIF(s.agent_profile, ''), s.provider)
		FROM agent_sessions s
		LEFT JOIN agent_profiles p ON p.id = s.agent_profile
		WHERE s.agent_token = ?
	`, token).Scan(&actor.SessionID, &actor.IssueID, &profileName)
	if err != nil {
		return issueActor{}, false
	}
	actor.Kind = "agent"
	actor.Name = profileName
	if strings.TrimSpace(actor.Name) == "" {
		actor.Name = "agent"
	}
	return actor, true
}

func (a *app) authenticateHumanToken(ctx context.Context, token string) (issueActor, error) {
	baseURL, _, workspaceID := a.controlPlaneSession()
	if strings.TrimSpace(baseURL) == "" {
		return issueActor{}, errors.New("sign in to mspace before changing issues")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/auth/me", nil)
	if err != nil {
		return issueActor{}, errUnauthorized
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return issueActor{}, errUnauthorized
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return issueActor{}, errUnauthorized
	}
	var payload struct {
		User struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Email     string `json:"email"`
			AvatarURL string `json:"avatarUrl"`
		} `json:"user"`
		Workspaces []struct {
			ID string `json:"id"`
		} `json:"workspaces"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return issueActor{}, errUnauthorized
	}
	if workspaceID != "" {
		member := false
		for _, workspace := range payload.Workspaces {
			if workspace.ID == workspaceID {
				member = true
				break
			}
		}
		if !member {
			return issueActor{}, errForbidden
		}
	}
	name := normalizeHumanActorName(firstNonEmpty(payload.User.Name, payload.User.Email, defaultHumanActorName))
	return issueActor{Kind: "human", Name: name, AvatarURL: strings.TrimSpace(payload.User.AvatarURL), UserID: payload.User.ID}, nil
}

func requestBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

func writeAuthError(w http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	if errors.Is(err, errForbidden) {
		status = http.StatusForbidden
	}
	writeError(w, status, err)
}

func (a *app) postControlPlaneIssueEvent(baseURL, token, workspaceID, issueID, status string) {
	body := map[string]any{
		"issueId": issueID,
		"kind":    inboxStatusToIssueEventKind(status),
		"summary": inboxStatusToIssueEventSummary(status),
		"payload": a.controlPlaneIssueEventPayload(issueID, status),
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/api/workspaces/" + url.PathEscape(workspaceID) + "/issue-events"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("publish control-plane issue event", "error", err)
		}
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && a.logger != nil {
		a.logger.Warn("publish control-plane issue event failed", "status", res.StatusCode)
	}
}

func (a *app) controlPlaneIssueEventPayload(issueID, status string) map[string]string {
	payload := map[string]string{"status": status}
	var projectID, projectName, title, issueStatus, assignee, assigneeType string
	err := a.db.QueryRow(`
		SELECT i.project_id, p.name, i.title, i.status, i.assignee, i.assignee_type
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.id = ?
	`, issueID).Scan(&projectID, &projectName, &title, &issueStatus, &assignee, &assigneeType)
	if err != nil {
		return payload
	}
	payload["projectId"] = projectID
	payload["projectName"] = projectName
	payload["title"] = title
	payload["issueTitle"] = title
	payload["issueStatus"] = issueStatus
	payload["assignee"] = assignee
	payload["assigneeType"] = assigneeType
	return payload
}

func inboxStatusToIssueEventKind(status string) string {
	status = strings.TrimSpace(status)
	if strings.HasPrefix(status, "status-") {
		return "issue_status_changed"
	}
	switch status {
	case "needs_review":
		return "issue_needs_review"
	case "ready_for_test", "test_passed", "test_failed":
		return "issue_test_status_changed"
	case "closed":
		return "issue_closed"
	case "failed":
		return "agent_failed"
	case "test-deploy", "test-environment":
		return "test_environment_updated"
	case "handoff":
		return "issue_handoff_updated"
	case "test-cleanup":
		return "test_environment_cleanup_requested"
	case "test-retain":
		return "test_environment_retained"
	case "labels", "triaged", "triage-failed":
		return "issue_triage_updated"
	case "updated":
		return "issue_updated"
	default:
		if status == "" {
			return "issue_updated"
		}
		return "issue_" + strings.ReplaceAll(status, "-", "_")
	}
}

func inboxStatusToIssueEventSummary(status string) string {
	status = strings.TrimSpace(status)
	if strings.HasPrefix(status, "status-") {
		return "Issue status changed."
	}
	switch status {
	case "needs_review":
		return "Issue is ready for human review."
	case "ready_for_test":
		return "Issue is ready for testing."
	case "test_passed":
		return "Issue test passed."
	case "test_failed":
		return "Issue test failed."
	case "closed":
		return "Issue was closed."
	case "failed":
		return "Agent session failed."
	case "test-deploy":
		return "Issue test deployment was requested."
	case "test-environment":
		return "Issue test environment changed."
	case "test-cleanup":
		return "Issue test environment cleanup was requested."
	case "test-retain":
		return "Issue test environment was retained."
	case "handoff":
		return "Issue branch or pull request handoff changed."
	case "labels":
		return "Issue labels changed."
	case "triaged":
		return "Issue triage completed."
	case "triage-failed":
		return "Issue triage failed."
	default:
		return "Issue activity needs review."
	}
}

func (a *app) storeEvidence(evidence deploymentEvidence, addComment bool) {
	_, _ = a.db.Exec(`
		INSERT INTO deployment_evidence (id, issue_id, session_id, cluster, namespace, summary, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, evidence.ID, evidence.IssueID, evidence.SessionID, evidence.Cluster, evidence.Namespace, evidence.Summary, evidence.Details, evidence.CreatedAt)
	if addComment {
		a.addSystemComment(evidence.IssueID, fmt.Sprintf(
			"Kubernetes evidence captured for session `%s` in `%s/%s`.\n\n%s",
			shortID(evidence.SessionID),
			clusterLabel(evidence.Cluster),
			evidence.Namespace,
			evidence.Summary,
		))
	}
	a.broker.publish(evidence.SessionID, sessionEvent{Type: "status", Payload: "evidence"})
}

func (a *app) storeSessionReviewEvidence(evidence sessionReviewEvidence) error {
	if evidence.ID == "" {
		evidence.ID = uuid.NewString()
	}
	now := nowString()
	if evidence.CreatedAt == "" {
		evidence.CreatedAt = now
	}
	evidence.UpdatedAt = now
	_, err := a.db.Exec(`
		INSERT INTO session_review_evidence (id, issue_id, session_id, source_session_id, source_commit_sha, branch, agent_summary, commands_json, tests_json, build_result_json, deployment_result_json, risks_json, follow_ups_json, preview_url, cluster, namespace, namespace_status, cleanup_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			source_session_id = excluded.source_session_id,
			source_commit_sha = excluded.source_commit_sha,
			branch = excluded.branch,
			agent_summary = excluded.agent_summary,
			commands_json = excluded.commands_json,
			tests_json = excluded.tests_json,
			build_result_json = excluded.build_result_json,
			deployment_result_json = excluded.deployment_result_json,
			risks_json = excluded.risks_json,
			follow_ups_json = excluded.follow_ups_json,
			preview_url = excluded.preview_url,
			cluster = excluded.cluster,
			namespace = excluded.namespace,
			namespace_status = excluded.namespace_status,
			cleanup_status = excluded.cleanup_status,
			updated_at = excluded.updated_at
	`, evidence.ID, evidence.IssueID, evidence.SessionID, evidence.SourceSessionID, evidence.SourceCommitSHA, evidence.Branch, evidence.AgentSummary, reviewJSON(evidence.CommandsRun, "[]"), reviewJSON(evidence.Tests, "[]"), reviewJSON(evidence.BuildResult, "{}"), reviewJSON(evidence.DeploymentResult, "{}"), reviewJSON(evidence.Risks, "[]"), reviewJSON(evidence.FollowUps, "[]"), evidence.PreviewURL, evidence.Cluster, evidence.Namespace, evidence.NamespaceStatus, evidence.CleanupStatus, evidence.CreatedAt, evidence.UpdatedAt)
	if err == nil {
		_, _ = a.db.Exec(`
			UPDATE session_failures SET review_evidence_id = ?, updated_at = ? WHERE session_id = ? AND review_evidence_id = ''
		`, evidence.ID, evidence.UpdatedAt, evidence.SessionID)
		a.broker.publish(evidence.SessionID, sessionEvent{Type: "status", Payload: "review-evidence"})
		a.publishInboxEvent(evidence.IssueID, "evidence")
	}
	return err
}

func (a *app) storeSessionFailure(failure sessionFailure) (sessionFailure, error) {
	stored, err := a.storeSessionFailureRecord(failure)
	if err != nil {
		return stored, err
	}
	if a.broker != nil {
		a.broker.publish(stored.SessionID, sessionEvent{Type: "status", Payload: "failure"})
	}
	a.publishInboxEvent(stored.IssueID, "failed")
	return stored, nil
}

func (a *app) storeSessionFailureRecord(failure sessionFailure) (sessionFailure, error) {
	if failure.ID == "" {
		failure.ID = uuid.NewString()
	}
	now := nowString()
	if failure.CreatedAt == "" {
		failure.CreatedAt = now
	}
	failure.UpdatedAt = now
	failure.Phase = normalizeFailurePhase(failure.Phase)
	failure.Status = normalizeFailureStatus(failure.Status)
	_, err := a.db.Exec(`
		INSERT INTO session_failures (id, issue_id, session_id, phase, status, failed_command, error_summary, error_excerpt, cluster, namespace, resource_kind, resource_name, evidence_id, review_evidence_id, retry_session_id, continued_session_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			phase = excluded.phase,
			status = excluded.status,
			failed_command = excluded.failed_command,
			error_summary = excluded.error_summary,
			error_excerpt = excluded.error_excerpt,
			cluster = excluded.cluster,
			namespace = excluded.namespace,
			resource_kind = excluded.resource_kind,
			resource_name = excluded.resource_name,
			evidence_id = excluded.evidence_id,
			review_evidence_id = CASE WHEN excluded.review_evidence_id != '' THEN excluded.review_evidence_id ELSE session_failures.review_evidence_id END,
			retry_session_id = CASE WHEN excluded.retry_session_id != '' THEN excluded.retry_session_id ELSE session_failures.retry_session_id END,
			continued_session_id = CASE WHEN excluded.continued_session_id != '' THEN excluded.continued_session_id ELSE session_failures.continued_session_id END,
			updated_at = excluded.updated_at
	`, failure.ID, failure.IssueID, failure.SessionID, failure.Phase, failure.Status, failure.FailedCommand, failure.ErrorSummary, failure.ErrorExcerpt, failure.Cluster, failure.Namespace, failure.ResourceKind, failure.ResourceName, failure.EvidenceID, failure.ReviewEvidenceID, failure.RetrySessionID, failure.ContinuedSessionID, failure.CreatedAt, failure.UpdatedAt)
	if err != nil {
		return failure, err
	}
	return failure, nil
}

func reviewJSON(value any, fallback string) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func (a *app) updateReviewEvidenceCleanupStatus(sessionID, cleanupStatus string) {
	_, _ = a.db.Exec(`
		UPDATE session_review_evidence SET cleanup_status = ?, updated_at = ? WHERE session_id = ?
	`, cleanupStatus, nowString(), sessionID)
}

type sessionEvent struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Stream  string `json:"stream,omitempty"`
}

func jsonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func shortID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func formatIssueLabels(labels []issueLabel) string {
	if len(labels) == 0 {
		return "none"
	}
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label.Name) == "" {
			continue
		}
		names = append(names, label.Name)
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func writeIssueTaskList(builder *strings.Builder, childIssues []issueListItem) {
	builder.WriteString("## Task List\n\n")
	builder.WriteString("Task list items are child issues. Treat these rows as the source of truth instead of Markdown checkbox text in the issue body.\n")
	builder.WriteString("When this session needs to create, check, or delete tasks, use the mspace API if `MSPACE_API_BASE_URL` and `MSPACE_AGENT_TOKEN` are available. Include `Authorization: Bearer ${MSPACE_AGENT_TOKEN}`. Use `POST /api/issues/${MSPACE_ISSUE_ID}/tasks` to add a task, `PUT /api/issues/<task-id>` with `{\"status\":\"closed\"}` to check one off, and `DELETE /api/issues/${MSPACE_ISSUE_ID}/tasks/<task-id>` to remove an obsolete task. Do not set the top-level issue to `closed`; only a human can close the issue.\n\n")
	if len(childIssues) == 0 {
		builder.WriteString("(no child issue tasks yet)\n\n")
		return
	}
	for _, task := range childIssues {
		marker := "[ ]"
		if task.Status == "closed" || task.Status == "completed" {
			marker = "[x]"
		}
		builder.WriteString(fmt.Sprintf("- %s %s (`%s`, status: %s)\n", marker, task.Title, task.ID, task.Status))
	}
	builder.WriteString("\n")
}

func plannedSessionWorkdir(root, projectID, sessionID string) string {
	return filepath.Join(root, projectID, sessionID)
}

func validateSessionWorkdir(root, workdir string) (string, error) {
	root = strings.TrimSpace(root)
	workdir = strings.TrimSpace(workdir)
	if root == "" || workdir == "" {
		return "", errUnsafeSessionWorkdir
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absWorkdir)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errUnsafeSessionWorkdir
	}
	return absWorkdir, nil
}

func (a *app) normalizeProjectInput(input projectInput) (project, error) {
	name := strings.TrimSpace(input.Name)
	sourceType := normalizeSourceType(input.SourceType)
	repoPath := strings.TrimSpace(input.RepoPath)
	repoURL := strings.TrimSpace(input.RepoURL)
	if repoURL == "" && looksLikeGitRemoteURL(repoPath) {
		repoURL = repoPath
		repoPath = ""
	}
	if sourceType == "" {
		if repoURL != "" {
			sourceType = "github"
		} else {
			sourceType = "local"
		}
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return project{}, errors.New("git is not available on PATH")
	}

	remoteURL := ""
	gitInfo := gitRemoteInfo{}
	nameCandidate := ""
	switch sourceType {
	case "local":
		var err error
		repoPath, remoteURL, gitInfo, nameCandidate, err = normalizeLocalProjectRepository(gitPath, repoPath)
		if err != nil {
			return project{}, err
		}
	case "github":
		var err error
		repoPath, remoteURL, gitInfo, nameCandidate, err = a.normalizeGitHubProjectRepository(gitPath, repoURL)
		if err != nil {
			return project{}, err
		}
	default:
		return project{}, errors.New("project source must be local or github")
	}

	if name == "" {
		name = nameCandidate
	}
	if name == "" {
		name = filepath.Base(repoPath)
	}

	defaultBranch := strings.TrimSpace(input.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = detectDefaultBranch(gitPath, repoPath)
	}
	defaultClusterID := strings.TrimSpace(input.DefaultClusterID)
	if defaultClusterID != "" {
		if _, err := a.loadCluster(defaultClusterID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return project{}, errClusterNotFound
			}
			return project{}, err
		}
	}

	return project{
		Name:                name,
		RepoPath:            repoPath,
		SourceType:          sourceType,
		RemoteURL:           remoteURL,
		GitProvider:         gitInfo.Provider,
		GitOwner:            gitInfo.Owner,
		GitRepo:             gitInfo.Repo,
		DefaultBranch:       defaultBranch,
		DeployCommand:       strings.TrimSpace(input.DeployCommand),
		ValidationCommand:   strings.TrimSpace(input.ValidationCommand),
		KubeContext:         strings.TrimSpace(input.KubeContext),
		KubeconfigPath:      strings.TrimSpace(input.KubeconfigPath),
		Namespace:           strings.TrimSpace(input.Namespace),
		ImageRegistryPrefix: strings.TrimSpace(input.ImageRegistryPrefix),
		PreviewDomain:       strings.TrimSpace(input.PreviewDomain),
		IngressClass:        strings.TrimSpace(input.IngressClass),
		NodeHost:            strings.TrimSpace(input.NodeHost),
		DefaultClusterID:    defaultClusterID,
	}, nil
}

func normalizeClusterInput(existing cluster, input clusterInput) (cluster, error) {
	exposureMode, err := normalizeExposureMode(input.ExposureMode)
	if err != nil {
		return cluster{}, err
	}
	if exposureMode == "" {
		exposureMode = "nodeport"
	}

	name := strings.TrimSpace(input.Name)
	kubeconfigPath := strings.TrimSpace(input.KubeconfigPath)
	imageRegistryPrefix := strings.TrimSpace(input.ImageRegistryPrefix)
	previewDomain := strings.TrimSpace(input.PreviewDomain)
	if name == "" {
		return cluster{}, errors.New("cluster name is required")
	}
	if kubeconfigPath == "" {
		return cluster{}, errors.New("kubeconfig path is required")
	}
	if imageRegistryPrefix == "" {
		return cluster{}, errors.New("image registry prefix is required")
	}
	if exposureMode == "ingress" && previewDomain == "" {
		return cluster{}, errors.New("preview domain is required when ingress is the default exposure mode")
	}

	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = existing.Status
	}
	if status == "" {
		status = "configured"
	}

	return cluster{
		ID:                  existing.ID,
		Name:                name,
		KubeconfigPath:      kubeconfigPath,
		KubeContext:         strings.TrimSpace(input.KubeContext),
		ImageRegistryPrefix: imageRegistryPrefix,
		ExposureMode:        exposureMode,
		NodeHost:            strings.TrimSpace(input.NodeHost),
		PreviewDomain:       previewDomain,
		IngressClass:        strings.TrimSpace(input.IngressClass),
		Status:              status,
		LastCheckedAt:       existing.LastCheckedAt,
		CreatedAt:           existing.CreatedAt,
		UpdatedAt:           existing.UpdatedAt,
	}, nil
}

func normalizeKubeconfigImportPaths(input kubeconfigImportRequest) []string {
	paths := append([]string{}, input.Paths...)
	if strings.TrimSpace(input.Path) != "" {
		paths = append(paths, input.Path)
	}
	return uniqueStrings(paths)
}

func discoverKubeconfigs(paths []string) kubeconfigDiscoveryResult {
	result := kubeconfigDiscoveryResult{
		Candidates: []kubeconfigCandidate{},
		Skipped:    []kubeconfigImportSkip{},
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		result.Skipped = append(result.Skipped, kubeconfigImportSkip{Reason: "kubectl is not available on PATH"})
		return result
	}
	for _, kubeconfigPath := range uniqueStrings(paths) {
		normalizedPath, err := normalizeKubeconfigPath(kubeconfigPath)
		if err != nil {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: kubeconfigPath, Reason: err.Error()})
			continue
		}
		contexts, err := kubeconfigContexts(normalizedPath)
		if err != nil {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Reason: err.Error()})
			continue
		}
		if len(contexts) == 0 {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Reason: "no kube contexts found"})
			continue
		}
		result.Candidates = append(result.Candidates, kubeconfigCandidate{
			Path:     normalizedPath,
			Contexts: contexts,
		})
	}
	return result
}

func discoverDefaultKubeconfigPaths(kubeDir string) ([]string, error) {
	entries, err := os.ReadDir(kubeDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read default kube directory: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(kubeDir, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func normalizeKubeconfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("kubeconfig path is empty")
	}
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(userHomeDir(), strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve kubeconfig path: %w", err)
		}
		path = absolutePath
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("kubeconfig file does not exist")
		}
		return "", fmt.Errorf("read kubeconfig file: %w", err)
	}
	if info.IsDir() {
		return "", errors.New("kubeconfig path points to a directory")
	}
	return path, nil
}

type kubeconfigView struct {
	CurrentContext string `json:"current-context"`
	Contexts       []struct {
		Name string `json:"name"`
	} `json:"contexts"`
}

func kubeconfigContexts(kubeconfigPath string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "config", "view", "-o", "json").CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, errors.New("kubectl config view timed out")
	}
	if err != nil {
		return nil, fmt.Errorf("kubectl config view failed: %s", truncate(strings.TrimSpace(string(output)), 240))
	}

	var view kubeconfigView
	if err := json.Unmarshal(output, &view); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	contexts := make([]string, 0, len(view.Contexts)+1)
	for _, item := range view.Contexts {
		if name := strings.TrimSpace(item.Name); name != "" {
			contexts = append(contexts, name)
		}
	}
	contexts = uniqueStrings(contexts)
	currentContext := strings.TrimSpace(view.CurrentContext)
	if currentContext == "" {
		return contexts, nil
	}
	filtered := make([]string, 0, len(contexts))
	for _, name := range contexts {
		if name != currentContext {
			filtered = append(filtered, name)
		}
	}
	sort.Strings(filtered)
	return append([]string{currentContext}, filtered...), nil
}

func validateKubeconfigContext(kubeconfigPath, kubeContext string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	args := []string{"--kubeconfig", kubeconfigPath}
	if strings.TrimSpace(kubeContext) != "" {
		args = append(args, "--context", kubeContext)
	}
	args = append(args, "--request-timeout=5s", "get", "--raw=/version")
	output, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("cluster version check timed out")
	}
	if err != nil {
		return fmt.Errorf("cluster version check failed: %s", truncate(strings.TrimSpace(string(output)), 240))
	}
	return nil
}

func importedClusterName(kubeconfigPath, kubeContext string) string {
	if strings.TrimSpace(kubeContext) != "" {
		return strings.Join(strings.Fields(kubeContext), " ")
	}
	base := strings.TrimSuffix(filepath.Base(kubeconfigPath), filepath.Ext(kubeconfigPath))
	if base == "" {
		return filepath.Base(kubeconfigPath)
	}
	return base
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func normalizeExposureMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "default":
		return "", nil
	case "nodeport", "node-port", "node_port":
		return "nodeport", nil
	case "ingress":
		return "ingress", nil
	default:
		return "", fmt.Errorf("unsupported exposure mode %q", value)
	}
}

func normalizeSourceType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "local", "github":
		return value
	case "github-url", "github_repo", "github-repo":
		return "github"
	default:
		return value
	}
}

func normalizeAssigneeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "human", "agent":
		return value
	default:
		return value
	}
}

func normalizeHumanActorName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "me" {
		return defaultHumanActorName
	}
	return truncate(value, 120)
}

func normalizeActorAvatarURL(value string) string {
	return truncate(strings.TrimSpace(value), 1024)
}

func defaultAgentProfiles(now string) []agentProfile {
	return []agentProfile{
		{
			ID:          "triage",
			Name:        "Triage",
			Mention:     "@triage",
			Provider:    "codex",
			Description: "Automatic issue type classification for new issues.",
			Instructions: strings.TrimSpace(`
Use the triage profile only for classification. Read the issue title, body, and project context, then choose exactly one Conventional Commit type: feat, fix, docs, style, refactor, perf, test, build, ci, chore, or revert. Do not assign priority, change status, or perform implementation work.
`),
			Enabled:   false,
			BuiltIn:   true,
			SortOrder: 5,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          "codex",
			Name:        "Codex",
			Mention:     "@codex",
			Provider:    "codex",
			Description: "General implementation, explanation, and follow-up work.",
			Instructions: strings.TrimSpace(`
Use the general Codex profile. Answer questions directly, make focused code changes when requested, and choose the smallest practical path through implementation and validation.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 10,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          "bugfix",
			Name:        "Bugfix",
			Mention:     "@bugfix",
			Provider:    "codex",
			Description: "Debugging, reproduction, root cause analysis, and narrow fixes.",
			Instructions: strings.TrimSpace(`
Use the bugfix profile. Start by identifying the failing behavior and the most likely root cause. Reproduce or inspect the real path before editing when practical. Keep the fix narrow, add or update regression coverage when the codebase supports it, and report the exact validation result.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 20,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          "design",
			Name:        "Design",
			Mention:     "@design",
			Provider:    "codex",
			Description: "UI, UX, interaction polish, and product surface decisions.",
			Instructions: strings.TrimSpace(`
Use the design profile. Improve product UI with the existing design system and the Issue/document-first mspace direction. Keep the interface quiet, text-led, keyboard-accessible, and avoid dashboard/card-heavy patterns unless they are already the local convention for that surface.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 30,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func (a *app) seedDefaultAgentProfiles() error {
	now := nowString()
	for _, profile := range defaultAgentProfiles(now) {
		if _, err := a.db.Exec(`
			INSERT OR IGNORE INTO agent_profiles (id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, profile.ID, profile.Name, profile.Mention, profile.Provider, profile.Description, profile.Instructions, boolToInt(profile.Enabled), boolToInt(profile.BuiltIn), profile.SortOrder, profile.CreatedAt, profile.UpdatedAt); err != nil {
			return fmt.Errorf("seed agent profile %s: %w", profile.ID, err)
		}
	}
	return nil
}

func (a *app) listAgentProfiles() ([]agentProfile, error) {
	rows, err := a.db.Query(`
		SELECT id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at
		FROM agent_profiles
		ORDER BY sort_order ASC, created_at ASC, name COLLATE NOCASE ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := []agentProfile{}
	for rows.Next() {
		profile, err := scanAgentProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (a *app) loadAgentProfile(value string) (agentProfile, error) {
	key := agentProfileLookupKey(value)
	if key == "" {
		key = "codex"
	}
	mention := "@" + key
	row := a.db.QueryRow(`
		SELECT id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at
		FROM agent_profiles
		WHERE lower(id) = ? OR lower(mention) = ?
	`, key, mention)
	return scanAgentProfile(row)
}

func (a *app) resolveEnabledAgentProfile(value string) (agentProfile, error) {
	profile, err := a.loadAgentProfile(value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agentProfile{}, fmt.Errorf("%w %q", errUnknownAgentProfile, value)
		}
		return agentProfile{}, err
	}
	if !profile.Enabled {
		return agentProfile{}, fmt.Errorf("%w %q is disabled", errUnknownAgentProfile, value)
	}
	return profile, nil
}

type agentProfileScanner interface {
	Scan(dest ...any) error
}

func scanAgentProfile(scanner agentProfileScanner) (agentProfile, error) {
	var profile agentProfile
	var enabled int
	var builtIn int
	if err := scanner.Scan(&profile.ID, &profile.Name, &profile.Mention, &profile.Provider, &profile.Description, &profile.Instructions, &enabled, &builtIn, &profile.SortOrder, &profile.CreatedAt, &profile.UpdatedAt); err != nil {
		return agentProfile{}, err
	}
	profile.Enabled = enabled == 1
	profile.BuiltIn = builtIn == 1
	return profile, nil
}

func normalizeAgentProfileInput(existing agentProfile, input agentProfileInput, isUpdate bool) (agentProfile, error) {
	profile := existing
	profile.Name = strings.Join(strings.Fields(strings.TrimSpace(input.Name)), " ")
	if profile.Name == "" {
		return agentProfile{}, errors.New("agent name is required")
	}

	mentionInput := input.Mention
	if strings.TrimSpace(mentionInput) == "" {
		mentionInput = profile.Name
	}
	mention, err := normalizeAgentMention(mentionInput)
	if err != nil {
		return agentProfile{}, err
	}
	profile.Mention = mention
	if !isUpdate {
		profile.ID = strings.TrimPrefix(mention, "@")
		profile.BuiltIn = false
		profile.SortOrder = 100
	}

	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" && isUpdate {
		provider = existing.Provider
	}
	if provider == "" {
		provider = "codex"
	}
	if provider != "codex" {
		return agentProfile{}, errors.New("only the codex provider is supported right now")
	}
	profile.Provider = provider

	profile.Description = strings.TrimSpace(input.Description)
	profile.Instructions = strings.TrimSpace(input.Instructions)
	if profile.Instructions == "" {
		return agentProfile{}, errors.New("agent instructions are required")
	}

	if input.Enabled == nil {
		profile.Enabled = !isUpdate || existing.Enabled
	} else {
		profile.Enabled = *input.Enabled
	}
	return profile, nil
}

func normalizeAgentMention(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return "", errors.New("agent mention is required")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("agent mention %q can only use letters, numbers, hyphen, or underscore", value)
	}
	return "@" + value, nil
}

func agentProfileLookupKey(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isCodexProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "codex")
}

func normalizeLocalProjectRepository(gitPath, repoPath string) (string, string, gitRemoteInfo, string, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return "", "", gitRemoteInfo{}, "", errors.New("repo path cannot be empty")
	}
	if !filepath.IsAbs(repoPath) {
		return "", "", gitRemoteInfo{}, "", errors.New("repo path must be an absolute path")
	}
	repoInfo, err := os.Stat(repoPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", gitRemoteInfo{}, "", errors.New("repo path does not exist")
		}
		return "", "", gitRemoteInfo{}, "", fmt.Errorf("repo path validation failed: %w", err)
	}
	if !repoInfo.IsDir() {
		return "", "", gitRemoteInfo{}, "", errors.New("repo path must point to a directory")
	}
	if err := ensureGitRepository(gitPath, repoPath); err != nil {
		return "", "", gitRemoteInfo{}, "", err
	}

	remoteURL := detectRemoteURL(gitPath, repoPath)
	gitInfo, _ := parseGitRemoteInfo(remoteURL)
	return repoPath, remoteURL, gitInfo, filepath.Base(repoPath), nil
}

func (a *app) normalizeGitHubProjectRepository(gitPath, repoURL string) (string, string, gitRemoteInfo, string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", "", gitRemoteInfo{}, "", errors.New("github repo url cannot be empty")
	}

	gitInfo, ok := parseGitRemoteInfo(repoURL)
	if !ok || gitInfo.Provider != "github" {
		return "", "", gitRemoteInfo{}, "", errors.New("github repo url must point to a GitHub repository")
	}

	repoPath := filepath.Join(a.repoRootPath(), safePathPart(gitInfo.Owner), safePathPart(gitInfo.Repo))
	if err := ensureClonedRepository(gitPath, repoURL, repoPath); err != nil {
		return "", "", gitRemoteInfo{}, "", err
	}
	return repoPath, repoURL, gitInfo, gitInfo.Repo, nil
}

func (a *app) repoRootPath() string {
	if a != nil && a.repoRoot != "" {
		return a.repoRoot
	}
	return filepath.Join(userHomeDir(), ".mspace", "repos")
}

func ensureClonedRepository(gitPath, repoURL, repoPath string) error {
	if repoInfo, err := os.Stat(repoPath); err == nil {
		if !repoInfo.IsDir() {
			return fmt.Errorf("clone path exists and is not a directory: %s", repoPath)
		}
		if err := ensureGitRepository(gitPath, repoPath); err != nil {
			return fmt.Errorf("clone path exists but is not a git work tree: %w", err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect clone path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		return fmt.Errorf("create clone parent: %w", err)
	}
	output, err := exec.Command(gitPath, "clone", repoURL, repoPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone github repo: %s", formatCommandFailure(err, output))
	}
	return nil
}

func detectRemoteURL(gitPath, repoPath string) string {
	if output, err := exec.Command(gitPath, "-C", repoPath, "remote", "get-url", "origin").CombinedOutput(); err == nil {
		return strings.TrimSpace(string(output))
	}
	output, err := exec.Command(gitPath, "-C", repoPath, "remote").CombinedOutput()
	if err != nil {
		return ""
	}
	remotes := strings.Fields(string(output))
	if len(remotes) == 0 {
		return ""
	}
	output, err = exec.Command(gitPath, "-C", repoPath, "remote", "get-url", remotes[0]).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func detectDefaultBranch(gitPath, repoPath string) string {
	if output, err := exec.Command(gitPath, "-C", repoPath, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").CombinedOutput(); err == nil {
		branch := strings.TrimSpace(string(output))
		if branch != "" {
			return strings.TrimPrefix(branch, "origin/")
		}
	}
	if output, err := exec.Command(gitPath, "-C", repoPath, "branch", "--show-current").CombinedOutput(); err == nil {
		branch := strings.TrimSpace(string(output))
		if branch != "" {
			return branch
		}
	}
	return "main"
}

func looksLikeGitRemoteURL(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "ssh://") ||
		strings.HasPrefix(value, "git@") ||
		strings.HasPrefix(value, "github.com/")
}

func parseGitRemoteInfo(remoteURL string) (gitRemoteInfo, bool) {
	value := strings.TrimSpace(strings.TrimPrefix(remoteURL, "git+"))
	if value == "" {
		return gitRemoteInfo{}, false
	}

	host := ""
	repoPath := ""
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		host = parsed.Host
		repoPath = parsed.Path
	} else if strings.Contains(value, "@") && strings.Contains(value, ":") {
		afterAt := value[strings.LastIndex(value, "@")+1:]
		parts := strings.SplitN(afterAt, ":", 2)
		if len(parts) == 2 {
			host = parts[0]
			repoPath = parts[1]
		}
	} else if strings.HasPrefix(value, "github.com/") {
		host = "github.com"
		repoPath = strings.TrimPrefix(value, "github.com/")
	}
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	repoPath = strings.Trim(strings.TrimSpace(repoPath), "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	parts := strings.Split(repoPath, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return gitRemoteInfo{}, false
	}

	provider := host
	if host == "github.com" {
		provider = "github"
	}
	return gitRemoteInfo{
		Provider: provider,
		Owner:    parts[0],
		Repo:     parts[1],
	}, true
}

func safePathPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "..", "")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "\\", "-")
	if value == "" {
		return "repo"
	}
	return value
}

func buildSessionCommand(issueID string, project project, override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}

	steps := []string{
		"set -e",
		"set -o pipefail",
		fmt.Sprintf("printf '%%s\\n' %s", shellQuote(fmt.Sprintf("Issue: %s", issueID))),
		fmt.Sprintf("printf '%%s\\n' %s", shellQuote(fmt.Sprintf("Project: %s", project.Name))),
		fmt.Sprintf("printf '%%s\\n' %s", shellQuote(fmt.Sprintf("Repo: %s", project.RepoPath))),
		"printf '%s\n' \"Context: ${MSPACE_SESSION_CONTEXT:-not available}\"",
	}

	hasWorkflowStep := false
	if project.DeployCommand != "" {
		hasWorkflowStep = true
		steps = append(
			steps,
			"",
			fmt.Sprintf("printf '%%s\\n' %s", shellQuote("==> Deploy")),
			project.DeployCommand,
		)
	}
	if project.ValidationCommand != "" {
		hasWorkflowStep = true
		steps = append(
			steps,
			"",
			fmt.Sprintf("printf '%%s\\n' %s", shellQuote("==> Validate")),
			project.ValidationCommand,
		)
	} else if project.Namespace != "" {
		hasWorkflowStep = true
		steps = append(
			steps,
			"",
			fmt.Sprintf("printf '%%s\\n' %s", shellQuote("==> Cluster snapshot")),
			buildKubectlCommand(project, "get", "pods,deploy,svc,ingress"),
		)
	}
	if !hasWorkflowStep {
		steps = append(
			steps,
			"",
			fmt.Sprintf("printf '%%s\\n' %s", shellQuote("No deploy or validation command configured for this project.")),
		)
	}

	return strings.Join(steps, "\n")
}

func buildKubeEnv(project project) []string {
	env := []string{}
	if project.KubeContext != "" {
		env = append(env, "MSPACE_KUBE_CONTEXT="+project.KubeContext)
	}
	if project.KubeconfigPath != "" {
		env = append(env, "KUBECONFIG="+project.KubeconfigPath)
		env = append(env, "MSPACE_KUBECONFIG="+project.KubeconfigPath)
	}
	if project.Namespace != "" {
		env = append(env, "MSPACE_KUBE_NAMESPACE="+project.Namespace)
	}
	if project.ImageRegistryPrefix != "" {
		env = append(env, "MSPACE_IMAGE_REGISTRY_PREFIX="+project.ImageRegistryPrefix)
	}
	if project.PreviewDomain != "" {
		env = append(env, "MSPACE_PREVIEW_DOMAIN="+project.PreviewDomain)
	}
	if project.IngressClass != "" {
		env = append(env, "MSPACE_INGRESS_CLASS="+project.IngressClass)
	}
	if project.NodeHost != "" {
		env = append(env, "MSPACE_NODE_HOST="+project.NodeHost)
	}
	return env
}

func (a *app) buildSessionEnv(session agentSession, project project, contextPath string) []string {
	env := buildKubeEnv(project)
	if environment, err := a.loadIssueTestEnvironment(session.IssueID); err == nil && environment != nil {
		if environment.KubeconfigPath != "" {
			env = append(env, "KUBECONFIG="+environment.KubeconfigPath)
			env = append(env, "MSPACE_KUBECONFIG="+environment.KubeconfigPath)
		}
		if environment.KubeContext != "" {
			env = append(env, "MSPACE_KUBE_CONTEXT="+environment.KubeContext)
		}
		if environment.Namespace != "" {
			env = append(env, "MSPACE_TEST_NAMESPACE="+environment.Namespace)
			env = append(env, "MSPACE_KUBE_NAMESPACE="+environment.Namespace)
		}
		if environment.ClusterID != "" {
			env = append(env, "MSPACE_CLUSTER_ID="+environment.ClusterID)
		}
		if environment.ExposureMode != "" {
			env = append(env, "MSPACE_EXPOSURE_MODE="+environment.ExposureMode)
		}
		if environment.ImageRegistryPrefix != "" {
			env = append(env, "MSPACE_IMAGE_REGISTRY_PREFIX="+environment.ImageRegistryPrefix)
		}
		if environment.PreviewDomain != "" {
			env = append(env, "MSPACE_PREVIEW_DOMAIN="+environment.PreviewDomain)
		}
		if environment.IngressClass != "" {
			env = append(env, "MSPACE_INGRESS_CLASS="+environment.IngressClass)
		}
		if environment.NodeHost != "" {
			env = append(env, "MSPACE_NODE_HOST="+environment.NodeHost)
		}
	}
	env = append(env,
		"MSPACE_API_BASE_URL="+mspaceAPIBaseURL(),
		"MSPACE_ISSUE_ID="+session.IssueID,
		"MSPACE_SESSION_ID="+session.ID,
		"MSPACE_AGENT_TOKEN="+session.AgentToken,
		"MSPACE_AGENT_PROFILE="+session.AgentProfile,
		"MSPACE_SESSION_BRANCH="+session.Branch,
		"MSPACE_SESSION_WORKDIR="+session.Workdir,
	)
	if session.SourceSessionID != "" {
		env = append(env, "MSPACE_SOURCE_SESSION_ID="+session.SourceSessionID)
	}
	if session.SourceCommitSHA != "" {
		env = append(env, "MSPACE_SOURCE_COMMIT_SHA="+session.SourceCommitSHA)
	}
	if contextPath != "" {
		env = append(env, "MSPACE_SESSION_CONTEXT="+contextPath)
	}
	if session.ArtifactDir != "" {
		env = append(env, "MSPACE_SESSION_ARTIFACT_DIR="+session.ArtifactDir)
	}
	return env
}

func mspaceAPIBaseURL() string {
	port := strings.TrimSpace(os.Getenv("MSPACE_PORT"))
	if port == "" {
		port = "7788"
	}
	return "http://127.0.0.1:" + port
}

func buildKubectlArgs(project project, args ...string) []string {
	kubectlArgs := []string{}
	if project.KubeconfigPath != "" {
		kubectlArgs = append(kubectlArgs, "--kubeconfig", project.KubeconfigPath)
	}
	if project.KubeContext != "" {
		kubectlArgs = append(kubectlArgs, "--context", project.KubeContext)
	}
	if project.Namespace != "" {
		kubectlArgs = append(kubectlArgs, "-n", project.Namespace)
	}
	kubectlArgs = append(kubectlArgs, args...)
	return kubectlArgs
}

func buildKubectlCommand(project project, args ...string) string {
	commandArgs := append([]string{"kubectl"}, buildKubectlArgs(project, args...)...)
	return shellJoin(commandArgs)
}

func ensureGitRepository(gitPath, repoPath string) error {
	output, err := exec.Command(gitPath, "-C", repoPath, "rev-parse", "--is-inside-work-tree").CombinedOutput()
	if err != nil {
		return fmt.Errorf("repo path is not a git work tree: %s", formatCommandFailure(err, output))
	}
	if strings.TrimSpace(string(output)) != "true" {
		return errors.New("repo path is not a git work tree")
	}
	return nil
}

func gitRefExists(gitPath, repoPath, ref string) (bool, error) {
	cmd := exec.Command(gitPath, "-C", repoPath, "rev-parse", "--verify", "--quiet", ref)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("check git ref %q: %w", ref, err)
	}
	return true, nil
}

func gitCommitExists(gitPath, repoPath, commitSHA string) error {
	commitSHA = strings.TrimSpace(commitSHA)
	if commitSHA == "" {
		return errors.New("source commit is empty")
	}
	output, err := exec.Command(gitPath, "-C", repoPath, "cat-file", "-e", commitSHA+"^{commit}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("source commit %s is not available in the project repo: %s", shortCommitSHA(commitSHA), formatCommandFailure(err, output))
	}
	return nil
}

func resolveBaseRef(gitPath, repoPath, defaultBranch string) (string, error) {
	defaultBranch = strings.TrimSpace(defaultBranch)
	if defaultBranch != "" {
		exists, err := gitRefExists(gitPath, repoPath, "refs/heads/"+defaultBranch)
		if err != nil {
			return "", err
		}
		if exists {
			return defaultBranch, nil
		}

		remoteRef := "refs/remotes/origin/" + defaultBranch
		exists, err = gitRefExists(gitPath, repoPath, remoteRef)
		if err != nil {
			return "", err
		}
		if exists {
			return "origin/" + defaultBranch, nil
		}
	}

	output, err := exec.Command(gitPath, "-C", repoPath, "rev-parse", "--verify", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve base ref: %s", formatCommandFailure(err, output))
	}
	return "HEAD", nil
}

func removeWorktree(gitPath, repoPath, workdir string) error {
	output, err := exec.Command(gitPath, "-C", repoPath, "worktree", "remove", "--force", workdir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove worktree %s: %s", workdir, formatCommandFailure(err, output))
	}
	return nil
}

func inspectWorkspace(workdir, defaultBranch string) workspaceSnapshot {
	snapshot := workspaceSnapshot{
		StatusLines: []string{},
		Changes:     []workspaceChange{},
		Comparison: workspaceComparison{
			CommitLines: []string{},
			Changes:     []workspaceChange{},
		},
	}

	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		snapshot.Error = "Workspace path has not been recorded for this session yet."
		return snapshot
	}

	info, err := os.Stat(workdir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			snapshot.Error = "Workspace has not been prepared yet."
			return snapshot
		}
		snapshot.Error = fmt.Sprintf("Inspect workspace: %v", err)
		return snapshot
	}
	if !info.IsDir() {
		snapshot.Error = "Workspace path is not a directory."
		return snapshot
	}
	snapshot.Exists = true

	gitPath, err := exec.LookPath("git")
	if err != nil {
		snapshot.Error = "git is not available on PATH."
		return snapshot
	}
	if err := ensureGitRepository(gitPath, workdir); err != nil {
		snapshot.Error = err.Error()
		return snapshot
	}
	snapshot.IsGitRepository = true

	if branch, err := runGitReadOnly(gitPath, workdir, "branch", "--show-current"); err == nil {
		snapshot.Branch = branch
	} else {
		snapshot.Error = err.Error()
	}

	if head, err := runGitReadOnly(gitPath, workdir, "rev-parse", "HEAD"); err == nil {
		snapshot.Head = head
		snapshot.ShortHead = shortCommitSHA(head)
	} else if snapshot.Error == "" {
		snapshot.Error = err.Error()
	}

	statusOutput, err := runGitReadOnly(gitPath, workdir, "status", "--short")
	if err != nil {
		if snapshot.Error == "" {
			snapshot.Error = err.Error()
		}
		return snapshot
	}

	for _, line := range strings.Split(statusOutput, "\n") {
		line = strings.TrimRight(line, " ")
		if line == "" {
			continue
		}
		snapshot.StatusLines = append(snapshot.StatusLines, line)
		snapshot.Changes = append(snapshot.Changes, parseWorkspaceChange(line))
		if strings.HasPrefix(line, "??") {
			snapshot.UntrackedFiles++
			continue
		}
		snapshot.ChangedFiles++
	}
	snapshot.HasChanges = snapshot.ChangedFiles > 0 || snapshot.UntrackedFiles > 0

	diffPreview, err := runGitReadOnly(gitPath, workdir, "diff", "--stat", "--patch", "--find-renames", "HEAD")
	if err != nil {
		if snapshot.Error == "" {
			snapshot.Error = err.Error()
		}
		return snapshot
	}
	snapshot.DiffPreview, snapshot.DiffTruncated = truncateWithFlag(diffPreview, 20000)
	inspectWorkspaceComparison(&snapshot, gitPath, workdir, defaultBranch)

	return snapshot
}

func inspectWorkspaceComparison(snapshot *workspaceSnapshot, gitPath, workdir, defaultBranch string) {
	baseRef, err := resolveBaseRef(gitPath, workdir, defaultBranch)
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	snapshot.Comparison.BaseRef = baseRef

	mergeBase, err := runGitReadOnly(gitPath, workdir, "merge-base", "HEAD", baseRef)
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	snapshot.Comparison.MergeBase = strings.TrimSpace(mergeBase)
	snapshot.Comparison.MergeBaseShort = shortCommitSHA(snapshot.Comparison.MergeBase)

	aheadBehind, err := runGitReadOnly(gitPath, workdir, "rev-list", "--left-right", "--count", baseRef+"...HEAD")
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	fields := strings.Fields(aheadBehind)
	if len(fields) == 2 {
		snapshot.Comparison.BehindCount = parseIntOrZero(fields[0])
		snapshot.Comparison.AheadCount = parseIntOrZero(fields[1])
	}

	commitLines, err := runGitReadOnly(gitPath, workdir, "log", "--oneline", "--decorate", "--no-color", snapshot.Comparison.MergeBase+"..HEAD")
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	snapshot.Comparison.CommitLines = splitNonEmptyLines(commitLines)

	nameStatusOutput, err := runGitReadOnly(gitPath, workdir, "diff", "--name-status", "--find-renames", snapshot.Comparison.MergeBase)
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	for _, line := range splitNonEmptyLines(nameStatusOutput) {
		snapshot.Comparison.Changes = append(snapshot.Comparison.Changes, parseNameStatusChange(line))
	}

	diffPreview, err := runGitReadOnly(gitPath, workdir, "diff", "--stat", "--patch", "--find-renames", snapshot.Comparison.MergeBase)
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	snapshot.Comparison.DiffPreview, snapshot.Comparison.DiffTruncated = truncateWithFlag(diffPreview, 20000)
}

func parseWorkspaceChange(line string) workspaceChange {
	change := workspaceChange{}
	if len(line) < 3 {
		change.Path = strings.TrimSpace(line)
		return change
	}

	change.StatusCode = line[:2]
	pathPart := strings.TrimSpace(line[3:])
	parts := strings.SplitN(pathPart, " -> ", 2)
	if len(parts) == 2 {
		change.PreviousPath = parts[0]
		change.Path = parts[1]
		return change
	}
	change.Path = pathPart
	return change
}

func parseNameStatusChange(line string) workspaceChange {
	change := workspaceChange{}
	fields := strings.Split(line, "\t")
	if len(fields) == 0 {
		return change
	}
	change.StatusCode = strings.TrimSpace(fields[0])
	if len(fields) >= 3 {
		change.PreviousPath = strings.TrimSpace(fields[1])
		change.Path = strings.TrimSpace(fields[2])
		return change
	}
	if len(fields) >= 2 {
		change.Path = strings.TrimSpace(fields[1])
	}
	return change
}

func runGitReadOnly(gitPath, repoPath string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repoPath}, args...)
	output, err := exec.Command(gitPath, commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), formatCommandFailure(err, output))
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

func runGitWriteWithIndexLockRetry(gitPath, repoPath string, extraEnv []string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", repoPath}, args...)
	var output []byte
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		command := exec.Command(gitPath, commandArgs...)
		if len(extraEnv) > 0 {
			command.Env = append(os.Environ(), extraEnv...)
		}
		output, err = command.CombinedOutput()
		if err == nil {
			return output, nil
		}
		if !isGitIndexLockFailure(err, output) || attempt == 5 {
			return output, err
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	return output, err
}

func isGitIndexLockFailure(err error, output []byte) bool {
	message := strings.ToLower(err.Error() + "\n" + string(output))
	return strings.Contains(message, "index.lock") &&
		strings.Contains(message, "unable to create") &&
		strings.Contains(message, "file exists")
}

func formatCommandFailure(err error, output []byte) string {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err.Error()
	}
	return message
}

func shellJoin(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func clusterLabel(cluster string) string {
	cluster = strings.TrimSpace(cluster)
	if cluster == "" {
		return "current-context"
	}
	return cluster
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "\n\n...truncated..."
}

func truncateWithFlag(value string, max int) (string, bool) {
	if len(value) <= max {
		return value, false
	}
	return value[:max] + "\n\n...truncated...", true
}

func splitNonEmptyLines(value string) []string {
	lines := strings.Split(value, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func parseIntOrZero(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func shortCommitSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
