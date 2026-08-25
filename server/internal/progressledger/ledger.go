// Package progressledger keeps the code-progress account in a git repository of
// YAML files rather than in the product database.
//
// Why it left the database. The account answers "where is the code, who is
// driving it, what happens next" — questions an agent asks when it picks up
// work in a checkout, often before it knows which workspace it is in and
// sometimes when the product is not running at all. A ledger that only exists
// inside one Postgres instance dies with that instance, which is exactly what
// happened, and it cannot be read by the person on another machine or reviewed
// as a diff. Git gives the account history, blame, and a remote for free, and
// those are the three properties a ledger most needs.
//
// One file per card. The unit is the card, not the checkout: a card may be
// worked in several checkouts over its life (renamed, re-cut, rebased) and
// reading its progress should not mean gathering fragments. A card with no
// branch at all still gets a file, with role "discussion" — the account of a
// conversation is progress too, and having nowhere to put it is why those
// conversations used to leave no trace.
//
// Writes go through here and nowhere else. There is no hand-written protocol to
// follow, because the previous incarnation of this repository proved that a
// protocol maintained by whoever remembers it drifts within weeks. The command
// and the hook write; a person writes by running the command.
package progressledger

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned by Find and Remove when no record answers to the
// reference given. Callers translate it into 404; it is not a failure of the
// store.
var ErrNotFound = errors.New("progressledger: record not found")

// ErrUnavailable is returned when the ledger directory does not exist. Reads
// degrade to empty rather than failing — a machine with no ledger checked out
// should still be able to open the product — but writes must say so loudly,
// because a write that silently goes nowhere is the worst outcome available.
var ErrUnavailable = errors.New("progressledger: ledger directory not found")

const (
	// ledgerDir is the subdirectory inside the repository that holds the
	// accounts. Everything else in that repository (README, tooling) is not
	// ours to read or write.
	ledgerDir = "ledger"

	// DefaultRootEnv overrides where the ledger lives. Set it in tests and on
	// machines that keep the repository somewhere else.
	DefaultRootEnv = "MULTICA_AGENT_PROGRESS_DIR"
)

// Session is the navigation slot: who is driving this account right now.
//
// WaitingForHuman is recorded, never inferred. "Waiting on a person" cannot be
// derived from the other fields without guessing, and a guessed wait status is
// unfalsifiable — the reader has no way to tell a real block from a stale
// heartbeat. Something has to say so explicitly, which is why the CLI has a
// flag for it.
type Session struct {
	Agent           string     `yaml:"agent,omitempty"`
	Resume          string     `yaml:"resume,omitempty"`
	Owner           string     `yaml:"owner,omitempty"`
	SessionID       string     `yaml:"session_id,omitempty"`
	NextAction      string     `yaml:"next_action,omitempty"`
	WaitingForHuman bool       `yaml:"waiting_for_human,omitempty"`
	UpdatedAt       *time.Time `yaml:"updated_at,omitempty"`
}

// Facts are the measurements, written only by `worktree sync` running inside
// the checkout. VerifiedAt stamps when git last actually said so, which is what
// makes a stale account visible instead of merely wrong.
type Facts struct {
	HeadSHA    string     `yaml:"head_sha,omitempty"`
	MergedSHA  string     `yaml:"merged_sha,omitempty"`
	MergedInto string     `yaml:"merged_into,omitempty"`
	Dirty      bool       `yaml:"dirty,omitempty"`
	VerifiedAt *time.Time `yaml:"verified_at,omitempty"`
}

// Entry is one appended line of what happened. Append only: a round recorded
// here cannot be tidied out of the history later, which is the property that
// makes the log worth reading at all.
type Entry struct {
	At         time.Time `yaml:"at"`
	Kind       string    `yaml:"kind"`
	Body       string    `yaml:"body"`
	SHA        string    `yaml:"sha,omitempty"`
	Issue      string    `yaml:"issue,omitempty"`
	IssueID    string    `yaml:"issue_id,omitempty"`
	AuthorType string    `yaml:"author_type,omitempty"`
	AuthorID   string    `yaml:"author_id,omitempty"`
}

// Record is one card's account.
type Record struct {
	// Key is the file stem and the stable identity. It is the card identifier
	// (COC-348) whenever the account has one, so the file a reader wants is the
	// file they can guess the name of.
	Key         string `yaml:"key"`
	WorkspaceID string `yaml:"workspace_id,omitempty"`

	// Name is what the commands address. It usually tracks Key lowercased, but
	// it can be anything the person driving the tree finds easier to type.
	Name    string `yaml:"name"`
	Issue   string `yaml:"issue,omitempty"`
	IssueID string `yaml:"issue_id,omitempty"`

	Repo    string `yaml:"repo,omitempty"`
	Path    string `yaml:"path,omitempty"`
	Branch  string `yaml:"branch,omitempty"`
	BaseRef string `yaml:"base_ref,omitempty"`
	Role    string `yaml:"role"`
	Status  string `yaml:"status"`
	Parent  string `yaml:"parent,omitempty"`

	// DependsOn holds the keys of accounts this one waits on. Keys, not free
	// text, so a reader can follow them.
	DependsOn []string `yaml:"depends_on,omitempty"`
	// Artifacts is where this card's output landed: paths, documents, links.
	Artifacts []string `yaml:"artifacts,omitempty"`

	Facts   Facts   `yaml:"facts,omitempty"`
	Session Session `yaml:"session,omitempty"`
	Log     []Entry `yaml:"log,omitempty"`

	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at"`
}

// EntryID addresses one line for the API. Positional, because the log is
// append-only: line three stays line three.
func (r Record) EntryID(index int) string {
	return r.Key + "#" + strconv.Itoa(index+1)
}

// rolePipelineOrder sorts accounts the way the work flows rather than the way
// rows happened to be typed: the base sits under everything, features are the
// work, integration and release carry batches, and discussions are not a stage
// of the pipeline at all so they sit at the end.
var rolePipelineOrder = map[string]int{
	"base": 0, "feature": 1, "integration": 2, "release": 3, "launch": 3,
	"hotfix": 4, "discussion": 5,
}

// keyUnsafe matches everything a file stem must not contain. Keys come from
// card identifiers and tree names, both of which are human strings that may
// carry slashes.
var keyUnsafe = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

// SanitizeKey turns a reference into a file stem. It is deliberately lossy in
// only one direction: two references that differ solely by a separator collapse
// to the same file, which is what we want for "COC-348" and "coc/348".
func SanitizeKey(raw string) string {
	key := keyUnsafe.ReplaceAllString(strings.TrimSpace(raw), "-")
	key = strings.Trim(key, "-.")
	if key == "" {
		return ""
	}
	if len([]rune(key)) > 96 {
		key = string([]rune(key)[:96])
	}
	return key
}

// issueIdentifierRE picks a card identifier out of a branch or tree name, so
// `worktree add coc-348 --branch feature/wy/COC-348/x` files itself under the
// card without anyone having to say so twice.
var issueIdentifierRE = regexp.MustCompile(`(?i)\b([a-z]{2,6})-(\d{1,6})\b`)

// GuessIssueIdentifier returns the card identifier a string carries, uppercased,
// or "" when it carries none.
func GuessIssueIdentifier(raw string) string {
	m := issueIdentifierRE.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1]) + "-" + m[2]
}

// Store reads and writes the accounts under one repository root.
//
// The mutex serialises writers inside this process, which is the only writer
// there is: the CLI and the hooks reach the ledger through the server, not by
// opening the files themselves. Two processes writing the same file would need
// locking this does not attempt, and adding a second writer is the change that
// would require it.
type Store struct {
	root string
	mu   sync.Mutex
}

// DefaultRoot resolves where the ledger repository lives: the environment
// override first, then the checkout this workflow actually uses.
func DefaultRoot() string {
	if dir := strings.TrimSpace(os.Getenv(DefaultRootEnv)); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "开源工具", "agent-progress")
}

// NewStore opens the ledger at root. An empty root means "resolve the default".
func NewStore(root string) *Store {
	if strings.TrimSpace(root) == "" {
		root = DefaultRoot()
	}
	return &Store{root: root}
}

// Root is the repository directory, for diagnostics and for the message a
// failed write prints.
func (s *Store) Root() string { return s.root }

func (s *Store) dir() string { return filepath.Join(s.root, ledgerDir) }

// Available reports whether the ledger repository is present. A machine without
// it can still read the product; it just has no code-progress account.
func (s *Store) Available() bool {
	if s.root == "" {
		return false
	}
	info, err := os.Stat(s.root)
	return err == nil && info.IsDir()
}

func (s *Store) path(key string) string {
	return filepath.Join(s.dir(), key+".yaml")
}

// List returns every account visible to a workspace, in pipeline order.
//
// A record with no workspace recorded is visible everywhere: files written by
// hand, or by a version of this code that predates the field, should show up
// rather than silently vanish.
func (s *Store) List(workspaceID string) ([]Record, error) {
	entries, err := os.ReadDir(s.dir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	records := make([]Record, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		rec, err := s.read(strings.TrimSuffix(e.Name(), ".yaml"))
		if err != nil {
			// One unreadable file must not blank the whole ledger. Skipping it
			// is visible in the UI as a missing row; failing the request is not
			// visible as anything except a broken page.
			continue
		}
		if workspaceID != "" && rec.WorkspaceID != "" && rec.WorkspaceID != workspaceID {
			continue
		}
		records = append(records, rec)
	}
	sortRecords(records)
	return records, nil
}

func sortRecords(records []Record) {
	sort.SliceStable(records, func(i, j int) bool {
		ri, ok := rolePipelineOrder[records[i].Role]
		if !ok {
			ri = 9
		}
		rj, ok := rolePipelineOrder[records[j].Role]
		if !ok {
			rj = 9
		}
		if ri != rj {
			return ri < rj
		}
		return records[i].Name < records[j].Name
	})
}

func (s *Store) read(key string) (Record, error) {
	raw, err := os.ReadFile(s.path(key))
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := yaml.Unmarshal(raw, &rec); err != nil {
		return Record{}, err
	}
	if rec.Key == "" {
		rec.Key = key
	}
	if rec.Name == "" {
		rec.Name = key
	}
	if rec.Role == "" {
		rec.Role = "feature"
	}
	if rec.Status == "" {
		rec.Status = "active"
	}
	return rec, nil
}

// Find resolves a reference the way a person would: the file stem first, then
// the addressable name, case-insensitively. `worktree show COC-348`,
// `worktree show coc-348` and `worktree show coc-348-tab` all land on the same
// account when that is the account they name.
func (s *Store) Find(workspaceID, ref string) (Record, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Record{}, ErrNotFound
	}
	if key := SanitizeKey(ref); key != "" {
		if rec, err := s.read(key); err == nil {
			if workspaceID == "" || rec.WorkspaceID == "" || rec.WorkspaceID == workspaceID {
				return rec, nil
			}
		}
	}
	records, err := s.List(workspaceID)
	if err != nil {
		return Record{}, err
	}
	lower := strings.ToLower(ref)
	for _, rec := range records {
		if strings.ToLower(rec.Name) == lower || strings.ToLower(rec.Key) == lower {
			return rec, nil
		}
	}
	// A card identifier reaches its account even when the account is addressed
	// by a tree name: the card is the thing most callers have in hand.
	if id := GuessIssueIdentifier(ref); id != "" {
		for _, rec := range records {
			if strings.EqualFold(rec.Issue, id) {
				return rec, nil
			}
		}
	}
	return Record{}, ErrNotFound
}

// FindByIssue returns the account belonging to a card identifier, if any.
func (s *Store) FindByIssue(workspaceID, identifier string) (Record, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return Record{}, ErrNotFound
	}
	records, err := s.List(workspaceID)
	if err != nil {
		return Record{}, err
	}
	for _, rec := range records {
		if strings.EqualFold(rec.Issue, identifier) {
			return rec, nil
		}
	}
	return Record{}, ErrNotFound
}

// FindByName returns the account whose addressable name matches exactly. Used
// by the create path to reject a duplicate name before writing.
func (s *Store) FindByName(workspaceID, name string) (Record, error) {
	records, err := s.List(workspaceID)
	if err != nil {
		return Record{}, err
	}
	for _, rec := range records {
		if strings.EqualFold(rec.Name, name) {
			return rec, nil
		}
	}
	return Record{}, ErrNotFound
}

// Save writes the account and commits it. subject becomes the commit subject.
//
// The commit is best effort: a ledger whose file is written but whose commit
// failed is still a correct ledger, and refusing the write because git was busy
// would lose the measurement instead of the history. A failed commit is
// reported to the caller as nil error with the file on disk, and the next
// successful write picks it up.
func (s *Store) Save(rec *Record, subject string) error {
	if !s.Available() {
		return fmt.Errorf("%w: %s", ErrUnavailable, s.root)
	}
	if rec.Key == "" {
		return errors.New("progressledger: record needs a key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now

	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return err
	}
	raw, err := yaml.Marshal(rec)
	if err != nil {
		return err
	}
	target := s.path(rec.Key)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	s.commit(subject)
	return nil
}

// Remove deletes an account and commits the removal.
func (s *Store) Remove(workspaceID, ref, subject string) error {
	rec, err := s.Find(workspaceID, ref)
	if err != nil {
		return err
	}
	if !s.Available() {
		return fmt.Errorf("%w: %s", ErrUnavailable, s.root)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(rec.Key)); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.commit(subject)
	return nil
}

// commit records the current state of the ledger directory.
//
// Silent by design. Every failure mode here — not a git repository, nothing
// staged, an identity that is not configured — means "no history was written",
// never "the account is wrong", and the request that triggered it has already
// succeeded at the thing it was asked to do.
func (s *Store) commit(subject string) {
	if subject == "" {
		subject = "progress: ledger update"
	}
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	if err := exec.Command("git", "-C", s.root, "rev-parse", "--git-dir").Run(); err != nil {
		return
	}
	if err := exec.Command("git", "-C", s.root, "add", "--", ledgerDir).Run(); err != nil {
		return
	}
	// Nothing staged is the common case when a sync re-measures identical
	// facts. Committing anyway would fill the history with empty rounds.
	if err := exec.Command("git", "-C", s.root, "diff", "--cached", "--quiet").Run(); err == nil {
		return
	}
	_ = exec.Command("git", "-C", s.root, "commit", "--no-verify", "-m", subject).Run()
}
