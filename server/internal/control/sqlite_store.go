package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteSnapshotSchemaVersion = 1

type SQLiteStore struct {
	*MemoryStore

	db   *sql.DB
	path string
	mu   sync.Mutex
}

type memoryStoreSnapshot struct {
	SchemaVersion        int                                          `json:"schemaVersion"`
	States               map[string]OAuthState                        `json:"states"`
	Results              map[string]memoryOAuthResult                 `json:"results"`
	Users                map[string]User                              `json:"users"`
	Identities           map[string]string                            `json:"identities"`
	IdentityLogins       map[string]string                            `json:"identityLogins"`
	PasswordCredentials  map[string]memoryPasswordCredential          `json:"passwordCredentials"`
	Workspaces           map[string][]Workspace                       `json:"workspaces"`
	WorkspaceMembers     map[string]map[string]string                 `json:"workspaceMembers"`
	WorkspaceInvitations map[string]memoryWorkspaceInvitation         `json:"workspaceInvitations"`
	Events               map[string]IssueEvent                        `json:"events"`
	Receipts             map[string]memoryReceipt                     `json:"receipts"`
	Watchers             map[string]map[string]bool                   `json:"watchers"`
	Projects             map[string]Project                           `json:"projects"`
	ProjectRunbooks      map[string]ProjectRunbook                    `json:"projectRunbooks"`
	Issues               map[string]Issue                             `json:"issues"`
	Comments             map[string]Comment                           `json:"comments"`
	CommentReactions     map[string]map[string]CommentReactionSummary `json:"commentReactions"`
	Attachments          map[string]IssueAttachment                   `json:"attachments"`
	IssueLabels          map[string][]IssueLabel                      `json:"issueLabels"`
	WorkspaceSettings    map[string]WorkspaceSettings                 `json:"workspaceSettings"`
	AgentProfiles        map[string]AgentProfile                      `json:"agentProfiles"`
	Clusters             map[string]Cluster                           `json:"clusters"`
	TestEnvironments     map[string]IssueTestEnvironment              `json:"testEnvironments"`
	ReviewEvidence       map[string]SessionReviewEvidence             `json:"reviewEvidence"`
	SessionFailures      map[string]SessionFailure                    `json:"sessionFailures"`
	Handoffs             map[string]IssueHandoff                      `json:"handoffs"`
	SessionHash          map[string]memorySession                     `json:"sessionHash"`
	RuntimeTokens        map[string]memoryRuntimeRegistrationToken    `json:"runtimeTokens"`
	RuntimeWorkers       map[string]RuntimeWorker                     `json:"runtimeWorkers"`
	RuntimeTasks         map[string]RuntimeTask                       `json:"runtimeTasks"`
	RuntimeTaskEvents    map[string]RuntimeTaskEvent                  `json:"runtimeTaskEvents"`
	RuntimeTaskLogs      map[string]RuntimeTaskLog                    `json:"runtimeTaskLogs"`
	NextID               int                                          `json:"nextId"`
}

func NewSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	store := &SQLiteStore{
		MemoryStore: NewMemoryStore(),
		db:          db,
		path:        path,
	}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.load(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Path() string {
	return s.path
}

func (s *SQLiteStore) Persist() error {
	return s.persist()
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS store_snapshots (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			schema_version INTEGER NOT NULL,
			payload TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	return err
}

func (s *SQLiteStore) load(ctx context.Context) error {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM store_snapshots WHERE id = 1`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot memoryStoreSnapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return err
	}
	if snapshot.SchemaVersion != sqliteSnapshotSchemaVersion {
		return fmt.Errorf("unsupported sqlite store snapshot schema version %d", snapshot.SchemaVersion)
	}
	s.MemoryStore.restoreSnapshot(snapshot)
	return nil
}

func (s *SQLiteStore) persist() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := s.MemoryStore.snapshotJSON()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO store_snapshots (id, schema_version, payload, updated_at)
		VALUES (1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			schema_version = excluded.schema_version,
			payload = excluded.payload,
			updated_at = excluded.updated_at
`, sqliteSnapshotSchemaVersion, string(payload), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *MemoryStore) snapshotJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return json.Marshal(memoryStoreSnapshot{
		SchemaVersion:        sqliteSnapshotSchemaVersion,
		States:               copyMap(s.states),
		Results:              copyMap(s.results),
		Users:                copyMap(s.users),
		Identities:           copyMap(s.identities),
		IdentityLogins:       copyMap(s.identityLogins),
		PasswordCredentials:  copyMap(s.passwordCredentials),
		Workspaces:           copyMap(s.workspaces),
		WorkspaceMembers:     copyMap(s.workspaceMembers),
		WorkspaceInvitations: copyMap(s.workspaceInvitations),
		Events:               copyMap(s.events),
		Receipts:             copyMap(s.receipts),
		Watchers:             copyMap(s.watchers),
		Projects:             copyMap(s.projects),
		ProjectRunbooks:      copyMap(s.projectRunbooks),
		Issues:               copyMap(s.issues),
		Comments:             copyMap(s.comments),
		CommentReactions:     copyMap(s.commentReactions),
		Attachments:          copyMap(s.attachments),
		IssueLabels:          copyMap(s.issueLabels),
		WorkspaceSettings:    copyMap(s.workspaceSettings),
		AgentProfiles:        copyMap(s.agentProfiles),
		Clusters:             copyMap(s.clusters),
		TestEnvironments:     copyMap(s.testEnvironments),
		ReviewEvidence:       copyMap(s.reviewEvidence),
		SessionFailures:      copyMap(s.sessionFailures),
		Handoffs:             copyMap(s.handoffs),
		SessionHash:          copyMap(s.sessionHash),
		RuntimeTokens:        copyMap(s.runtimeTokens),
		RuntimeWorkers:       copyMap(s.runtimeWorkers),
		RuntimeTasks:         copyMap(s.runtimeTasks),
		RuntimeTaskEvents:    copyMap(s.runtimeTaskEvents),
		RuntimeTaskLogs:      copyMap(s.runtimeTaskLogs),
		NextID:               s.nextID,
	})
}

func (s *MemoryStore) restoreSnapshot(snapshot memoryStoreSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.states = ensureMap(snapshot.States)
	s.results = ensureMap(snapshot.Results)
	s.users = ensureMap(snapshot.Users)
	s.identities = ensureMap(snapshot.Identities)
	s.identityLogins = ensureMap(snapshot.IdentityLogins)
	s.passwordCredentials = ensureMap(snapshot.PasswordCredentials)
	s.workspaces = ensureMap(snapshot.Workspaces)
	s.workspaceMembers = ensureMap(snapshot.WorkspaceMembers)
	s.workspaceInvitations = ensureMap(snapshot.WorkspaceInvitations)
	s.events = ensureMap(snapshot.Events)
	s.receipts = ensureMap(snapshot.Receipts)
	s.watchers = ensureMap(snapshot.Watchers)
	s.projects = ensureMap(snapshot.Projects)
	s.projectRunbooks = ensureMap(snapshot.ProjectRunbooks)
	s.issues = ensureMap(snapshot.Issues)
	s.comments = ensureMap(snapshot.Comments)
	s.commentReactions = ensureMap(snapshot.CommentReactions)
	s.attachments = ensureMap(snapshot.Attachments)
	s.issueLabels = ensureMap(snapshot.IssueLabels)
	s.workspaceSettings = ensureMap(snapshot.WorkspaceSettings)
	s.agentProfiles = ensureMap(snapshot.AgentProfiles)
	s.clusters = ensureMap(snapshot.Clusters)
	s.testEnvironments = ensureMap(snapshot.TestEnvironments)
	s.reviewEvidence = ensureMap(snapshot.ReviewEvidence)
	s.sessionFailures = ensureMap(snapshot.SessionFailures)
	s.handoffs = ensureMap(snapshot.Handoffs)
	s.sessionHash = ensureMap(snapshot.SessionHash)
	s.runtimeTokens = ensureMap(snapshot.RuntimeTokens)
	s.runtimeWorkers = ensureMap(snapshot.RuntimeWorkers)
	s.runtimeTasks = ensureMap(snapshot.RuntimeTasks)
	s.runtimeTaskEvents = ensureMap(snapshot.RuntimeTaskEvents)
	s.runtimeTaskLogs = ensureMap(snapshot.RuntimeTaskLogs)
	s.nextID = snapshot.NextID
}

func copyMap[M ~map[K]V, K comparable, V any](input M) M {
	if input == nil {
		return M{}
	}
	output := make(M, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func ensureMap[M ~map[K]V, K comparable, V any](input M) M {
	if input == nil {
		return M{}
	}
	return input
}
