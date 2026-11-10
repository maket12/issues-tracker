package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maket12/issues-tracker/backend/internal/domain"
	"github.com/maket12/issues-tracker/backend/internal/httpapi"
	"github.com/maket12/issues-tracker/backend/internal/repository"
	"github.com/maket12/issues-tracker/backend/internal/service"
)

const (
	testDB      = "issuetrack_it"
	testAppUser = "it_app"
	testAppPass = "it_app_pw"
	dateLayout  = "2006-01-02"
)

type env struct {
	srv   *httptest.Server
	admin *sqlx.DB // privileged: seeding and assertions
	repo  *repository.IssueRepo
}

func newEnv(t *testing.T) *env {
	t.Helper()
	rootDSN := os.Getenv("TEST_MYSQL_ROOT_DSN")
	if rootDSN == "" {
		t.Skip("TEST_MYSQL_ROOT_DSN is not set")
	}
	cfg, err := mysql.ParseDSN(rootDSN)
	require.NoError(t, err)
	cfg.ParseTime, cfg.Loc, cfg.MultiStatements = true, time.UTC, true
	cfg.Params = map[string]string{"charset": "utf8mb4"}

	cfg.DBName = ""
	root, err := sqlx.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	root.MustExec("DROP DATABASE IF EXISTS " + testDB)
	root.MustExec("CREATE DATABASE " + testDB + " CHARACTER SET utf8mb4")
	require.NoError(t, root.Close())

	cfg.DBName = testDB
	admin, err := sqlx.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "../../migrations"
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no *.up.sql files found in %s", dir)
	sort.Strings(files)
	for _, f := range files {
		sqlText, err := os.ReadFile(f)
		require.NoError(t, err)
		_, err = admin.Exec(string(sqlText))
		require.NoError(t, err, f)
	}

	admin.MustExec("CREATE USER IF NOT EXISTS '" + testAppUser + "'@'%' IDENTIFIED BY '" + testAppPass + "'")
	admin.MustExec("GRANT SELECT, INSERT, UPDATE, DELETE ON " + testDB + ".* TO '" + testAppUser + "'@'%'")

	seed := []string{
		"INSERT INTO Roles (id, name) VALUES ('member','Member'), ('viewer','Viewer')",
		"INSERT INTO RolesPermissions (role, permission) SELECT `key`, 'CREATE_ISSUE' FROM Roles WHERE id = 'member'",
		"INSERT INTO RolesPermissions (role, permission) SELECT `key`, 'VIEW_ISSUE' FROM Roles WHERE id = 'member'",
		"INSERT INTO RolesPermissions (role, permission) SELECT `key`, 'EDIT_ISSUE' FROM Roles WHERE id = 'member'",
		"INSERT INTO IssueTypes (id, name) VALUES ('bug','Bug'), ('task','Task')",
		"INSERT INTO Priorities (id, name) VALUES ('low','Low'), ('high','High')",
		"INSERT INTO Statuses (id, name) VALUES ('open','Open'), ('done','Done')",
		userSQL("alice", "member"), userSQL("bob", "member"), userSQL("carol", "member"), userSQL("vera", "viewer"),
	}
	for _, s := range seed {
		admin.MustExec(s)
	}

	appCfg := cfg.Clone()
	appCfg.User, appCfg.Passwd, appCfg.MultiStatements = testAppUser, testAppPass, false
	appDB, err := sqlx.Open("mysql", appCfg.FormatDSN())
	require.NoError(t, err)

	repo := repository.NewIssueRepo(appDB)
	srv := httptest.NewServer(httpapi.NewRouter(service.NewIssueService(repo), testAuth(repository.NewUserRepo(admin))))
	t.Cleanup(func() { srv.Close(); appDB.Close(); admin.Close() })
	return &env{srv: srv, admin: admin, repo: repo}
}

func userSQL(id, role string) string {
	return "INSERT INTO Users (id, name, email, passwdHash, role) " +
		"SELECT '" + id + "', '" + id + "', '" + id + "@example.com', 'x', `key` FROM Roles WHERE id = '" + role + "'"
}

func testAuth(users *repository.UserRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.Header.Get("X-Test-User"); id != "" {
				if u, err := users.FindByID(r.Context(), id); err == nil {
					r = r.WithContext(httpapi.WithUser(r.Context(), u))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// do sends a request (a string body is sent as-is, anything else as JSON). Safe to call from goroutines.
func (e *env) do(t *testing.T, method, path, user string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rdr = strings.NewReader(b)
	default:
		j, err := json.Marshal(b)
		if err != nil {
			t.Errorf("marshal: %v", err)
			return -1, nil
		}
		rdr = bytes.NewReader(j)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		t.Errorf("request: %v", err)
		return -1, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("%s %s: %v", method, path, err)
		return -1, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (e *env) createIssue(t *testing.T, user string, body any) (int, []byte) {
	t.Helper()
	return e.do(t, http.MethodPost, "/api/issues", user, body)
}

func (e *env) fillAttributes(t *testing.T, user, issueID string, body any) (int, []byte) {
	t.Helper()
	return e.do(t, http.MethodPut, "/api/issues/"+issueID, user, body)
}

func (e *env) viewIssue(t *testing.T, user, issueID string) (int, []byte) {
	t.Helper()
	return e.do(t, http.MethodGet, "/api/issues/"+issueID, user, nil)
}

func (e *env) editIssue(t *testing.T, user, issueID string, body any) (int, []byte) {
	t.Helper()
	return e.do(t, http.MethodPatch, "/api/issues/"+issueID, user, body)
}

// newIssue creates an issue as alice and returns it.
func (e *env) newIssue(t *testing.T) issueResp {
	t.Helper()
	status, raw := e.createIssue(t, "alice", map[string]any{"name": "Original", "typeId": "bug"})
	require.Equal(t, http.StatusCreated, status, string(raw))
	return decodeIssue(t, raw)
}

func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, e.admin.Get(&n, query, args...))
	return n
}

func (e *env) text(t *testing.T, query string, args ...any) string {
	t.Helper()
	var s string
	require.NoError(t, e.admin.Get(&s, query, args...))
	return s
}

func (e *env) issueCount(t *testing.T) int { return e.count(t, "SELECT COUNT(*) FROM Issue") }

type ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type issueResp struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Type        ref     `json:"type"`
	Author      ref     `json:"author"`
	Assignee    *ref    `json:"assignee"`
	Status      ref     `json:"status"`
	Priority    ref     `json:"priority"`
	StartDate   string  `json:"startDate"`
	DueDate     *string `json:"dueDate"`
	Version     string  `json:"version"`
}

func decodeIssue(t *testing.T, raw []byte) issueResp {
	t.Helper()
	var got issueResp
	require.NoError(t, json.Unmarshal(raw, &got), string(raw))
	return got
}

func fieldsOf(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	var resp struct {
		Fields map[string]string `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(raw, &resp), string(raw))
	return resp.Fields
}

func fillBody() map[string]any {
	return map[string]any{
		"name": "Login fails on Safari", "typeId": "task", "description": "<p>updated</p>",
		"assigneeId": "bob", "priorityId": "high", "startDate": "2026-11-10", "dueDate": "2026-11-20",
	}
}

func TestCreateIssue_HappyPath(t *testing.T) {
	e := newEnv(t)
	desc := "<p>Steps to <b>reproduce</b></p>"

	before := time.Now().UTC().Format(dateLayout)
	status, raw := e.createIssue(t, "alice", map[string]any{
		"name": "Кнопка входа не работает 🐞", "typeId": "bug", "description": desc, // UTF-8 incl. 4-byte characters
	})
	after := time.Now().UTC().Format(dateLayout)
	require.Equal(t, http.StatusCreated, status, string(raw))
	got := decodeIssue(t, raw)

	assert.Equal(t, "ISS-1", got.ID)
	assert.Equal(t, "Кнопка входа не работает 🐞", got.Name)
	assert.Equal(t, ref{ID: "bug", Name: "Bug"}, got.Type)
	require.NotNil(t, got.Description)
	assert.Equal(t, desc, *got.Description)
	assert.Equal(t, "alice", got.Author.ID, "the author is the authenticated user")
	assert.Equal(t, ref{ID: "open", Name: "Open"}, got.Status, "a new issue gets the initial status")
	assert.Equal(t, "low", got.Priority.ID, "default priority until IM-2 chooses one")
	assert.Nil(t, got.Assignee)
	assert.Nil(t, got.DueDate)
	assert.Contains(t, []string{before, after}, got.StartDate, "start date is today (UTC)")
	assert.NotEmpty(t, got.Version)

	assert.Equal(t, 1, e.issueCount(t))
	assert.Equal(t, 1, e.count(t,
		"SELECT COUNT(*) FROM Issue i JOIN Users a ON a.`key` = i.author JOIN Statuses s ON s.`key` = i.status "+
			"WHERE i.id = 'ISS-1' AND i.name = ? AND a.id = 'alice' AND s.id = 'open'", "Кнопка входа не работает 🐞"))
}

func TestCreateIssue_DescriptionIsOptional(t *testing.T) {
	e := newEnv(t)
	for name, body := range map[string]map[string]any{
		"omitted": {"name": "A", "typeId": "bug"},
		"blank":   {"name": "B", "typeId": "bug", "description": "  \n "},
	} {
		status, raw := e.createIssue(t, "alice", body)
		require.Equal(t, http.StatusCreated, status, "%s: %s", name, raw)
		assert.Nil(t, decodeIssue(t, raw).Description, name)
	}
	assert.Equal(t, 2, e.count(t, "SELECT COUNT(*) FROM Issue WHERE description IS NULL"))
}

func TestCreateIssue_RequiresAuthenticationAndPermission(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"name": "A", "typeId": "bug"}

	for _, user := range []string{"", "ghost"} { // no header / unknown user
		status, _ := e.createIssue(t, user, body)
		assert.Equal(t, http.StatusUnauthorized, status, "user %q", user)
	}
	status, _ := e.createIssue(t, "vera", body) // role without CREATE_ISSUE
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, 0, e.issueCount(t))
}

func TestCreateIssue_RejectsInvalidInput(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"missing name", map[string]any{"typeId": "bug"}, "name"},
		{"blank name", map[string]any{"name": "   ", "typeId": "bug"}, "name"},
		{"name too long", map[string]any{"name": strings.Repeat("a", 201), "typeId": "bug"}, "name"},
		{"missing type", map[string]any{"name": "A"}, "typeId"},
		{"unknown type", map[string]any{"name": "A", "typeId": "epic"}, "typeId"},
		{"description too large", map[string]any{"name": "A", "typeId": "bug", "description": strings.Repeat("x", domain.MaxDescriptionBytes+1)}, "description"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := e.createIssue(t, "alice", tc.body)
			require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
			assert.Contains(t, fieldsOf(t, raw), tc.field)
			assert.Equal(t, 0, e.issueCount(t), "nothing is saved")
		})
	}
}

func TestCreateIssue_ClientCannotChooseStatusPriorityAuthorOrId(t *testing.T) {
	e := newEnv(t)
	for _, extra := range []string{`"status":"done"`, `"priority":"high"`, `"author":"bob"`, `"id":"ISS-99"`, `"assigneeId":"bob"`} {
		status, _ := e.createIssue(t, "alice", `{"name":"A","typeId":"bug",`+extra+`}`)
		assert.Equal(t, http.StatusBadRequest, status, extra)
	}
	assert.Equal(t, 0, e.issueCount(t))
}

func TestCreateIssue_RejectsMalformedRequests(t *testing.T) {
	e := newEnv(t)
	for name, body := range map[string]string{
		"empty body":  ``,
		"broken json": `{"name": `,
		"wrong type":  `{"name":123,"typeId":"bug"}`,
	} {
		status, _ := e.createIssue(t, "alice", body)
		assert.Equal(t, http.StatusBadRequest, status, name)
	}
	assert.Equal(t, 0, e.issueCount(t))
}

func TestCreateIssue_FailsCleanlyWhenNoInitialStatusExists(t *testing.T) {
	e := newEnv(t)
	e.admin.MustExec("DELETE FROM Statuses")

	status, _ := e.createIssue(t, "alice", map[string]any{"name": "A", "typeId": "bug"})
	assert.Equal(t, http.StatusInternalServerError, status, "a server misconfiguration, not a client error")
	assert.Equal(t, 0, e.issueCount(t))
}

func TestCreateIssue_ConcurrentCreatesGetUniqueIDs(t *testing.T) {
	e := newEnv(t)
	const n = 10
	ids := make([]string, n)
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, raw := e.createIssue(t, "alice", map[string]any{"name": "Race", "typeId": "bug"})
			codes[i] = code
			var got issueResp
			if json.Unmarshal(raw, &got) == nil {
				ids[i] = got.ID
			}
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := range ids {
		assert.Equal(t, http.StatusCreated, codes[i])
		seen[ids[i]] = true
	}
	assert.Len(t, seen, n, "every issue gets its own id")
	assert.Equal(t, 0, e.count(t, "SELECT COUNT(*) FROM Issue WHERE id NOT LIKE 'ISS-%'"), "no row keeps a temporary id")
}

func TestFillAttributes_HappyPath(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	status, raw := e.fillAttributes(t, "alice", created.ID, fillBody())
	require.Equal(t, http.StatusOK, status, string(raw))
	got := decodeIssue(t, raw)

	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "Login fails on Safari", got.Name)
	assert.Equal(t, ref{ID: "task", Name: "Task"}, got.Type)
	require.NotNil(t, got.Description)
	assert.Equal(t, "<p>updated</p>", *got.Description)
	require.NotNil(t, got.Assignee)
	assert.Equal(t, ref{ID: "bob", Name: "bob"}, *got.Assignee)
	assert.Equal(t, ref{ID: "high", Name: "High"}, got.Priority)
	assert.Equal(t, "2026-11-10", got.StartDate)
	require.NotNil(t, got.DueDate)
	assert.Equal(t, "2026-11-20", *got.DueDate)
	assert.Equal(t, "alice", got.Author.ID, "the author never changes")
	assert.Equal(t, "open", got.Status.ID, "the status is not an attribute of IM-2")
	assert.NotEqual(t, created.Version, got.Version, "every save produces a new version")

	assert.Equal(t, 1, e.count(t,
		"SELECT COUNT(*) FROM Issue i JOIN IssueTypes t ON t.`key` = i.type JOIN Priorities p ON p.`key` = i.priority "+
			"JOIN Users a ON a.`key` = i.assignee WHERE i.id = ? AND i.name = ? AND t.id = 'task' AND p.id = 'high' AND a.id = 'bob' "+
			"AND i.startDate = '2026-11-10' AND i.dueDate = '2026-11-20'", created.ID, "Login fails on Safari"))
}

func TestFillAttributes_OptionalAttributesCanBeCleared(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	status, raw := e.fillAttributes(t, "alice", created.ID, fillBody())
	require.Equal(t, http.StatusOK, status, string(raw))

	status, raw = e.fillAttributes(t, "alice", created.ID, map[string]any{
		"name": "Plain", "typeId": "bug", "priorityId": "low", "startDate": "2026-11-10",
	})
	require.Equal(t, http.StatusOK, status, string(raw))
	got := decodeIssue(t, raw)
	assert.Nil(t, got.Description)
	assert.Nil(t, got.Assignee)
	assert.Nil(t, got.DueDate)
	assert.Equal(t, 1, e.count(t,
		"SELECT COUNT(*) FROM Issue WHERE id = ? AND description IS NULL AND assignee IS NULL AND dueDate IS NULL", created.ID))
}

func TestFillAttributes_AuthorAndAssigneeHaveAccess_StrangersDoNot(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t) // author alice
	status, raw := e.fillAttributes(t, "alice", created.ID, fillBody()) // assigns bob
	require.Equal(t, http.StatusOK, status, string(raw))
	current := decodeIssue(t, raw)

	body := fillBody()
	body["name"] = "Edited by bob"
	status, _ = e.fillAttributes(t, "bob", created.ID, body)
	assert.Equal(t, http.StatusOK, status)

	for _, user := range []string{"carol", "vera"} {
		body["name"] = "Edited by " + user
		status, _ = e.fillAttributes(t, user, created.ID, body)
		assert.Equal(t, http.StatusForbidden, status, user)
	}
	
	assert.Equal(t, "Edited by bob", e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID))
	assert.NotEqual(t, current.Version, e.text(t, "SELECT version FROM Issue WHERE id = ?", created.ID))
}

func TestFillAttributes_RequiresAuthenticationAndAnExistingIssue(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	status, _ := e.fillAttributes(t, "", created.ID, fillBody())
	assert.Equal(t, http.StatusUnauthorized, status)

	status, _ = e.fillAttributes(t, "alice", "ISS-9999", fillBody())
	assert.Equal(t, http.StatusNotFound, status)
}

func TestFillAttributes_RejectsInvalidAttributes(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	cases := []struct {
		name   string
		mutate func(m map[string]any)
		field  string
	}{
		{"missing name", func(m map[string]any) { delete(m, "name") }, "name"},
		{"blank name", func(m map[string]any) { m["name"] = "   " }, "name"},
		{"name too long", func(m map[string]any) { m["name"] = strings.Repeat("a", 201) }, "name"},
		{"missing type", func(m map[string]any) { delete(m, "typeId") }, "typeId"},
		{"unknown type", func(m map[string]any) { m["typeId"] = "epic" }, "typeId"},
		{"missing priority", func(m map[string]any) { delete(m, "priorityId") }, "priorityId"},
		{"unknown priority", func(m map[string]any) { m["priorityId"] = "urgent" }, "priorityId"},
		{"unknown assignee", func(m map[string]any) { m["assigneeId"] = "nobody" }, "assigneeId"},
		{"missing start date", func(m map[string]any) { delete(m, "startDate") }, "startDate"},
		{"due before start", func(m map[string]any) { m["startDate"], m["dueDate"] = "2026-11-10", "2026-11-09" }, "dueDate"},
		{"description too large", func(m map[string]any) { m["description"] = strings.Repeat("x", domain.MaxDescriptionBytes+1) }, "description"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fillBody()
			tc.mutate(body)
			status, raw := e.fillAttributes(t, "alice", created.ID, body)
			require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
			assert.Contains(t, fieldsOf(t, raw), tc.field)

			assert.Equal(t, "Original", e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID), "issue is unchanged")
			assert.Equal(t, created.Version, e.text(t, "SELECT version FROM Issue WHERE id = ?", created.ID))
		})
	}

	t.Run("several problems are reported together", func(t *testing.T) {
		body := fillBody()
		body["typeId"], body["priorityId"], body["assigneeId"] = "epic", "urgent", "nobody"
		status, raw := e.fillAttributes(t, "alice", created.ID, body)
		require.Equal(t, http.StatusUnprocessableEntity, status)
		for _, f := range []string{"typeId", "priorityId", "assigneeId"} {
			assert.Contains(t, fieldsOf(t, raw), f)
		}
	})
}

func TestFillAttributes_RejectsMalformedRequestsAndUneditableFields(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	for name, body := range map[string]string{
		"empty body":     ``,
		"broken json":    `{"name": `,
		"bad date":       `{"name":"x","typeId":"bug","priorityId":"low","startDate":"10/11/2026"}`,
		"status field":   `{"name":"x","typeId":"bug","priorityId":"low","startDate":"2026-11-10","status":"done"}`,
		"author field":   `{"name":"x","typeId":"bug","priorityId":"low","startDate":"2026-11-10","author":"bob"}`,
		"wrong type":     `{"name":123,"typeId":"bug","priorityId":"low","startDate":"2026-11-10"}`,
	} {
		status, _ := e.fillAttributes(t, "alice", created.ID, body)
		assert.Equal(t, http.StatusBadRequest, status, name)
	}
	assert.Equal(t, "Original", e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID))
}

func TestSave_StaleVersionIsRejected(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	ctx := context.Background()

	first, err := e.repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	second, err := e.repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	typ, err := e.repo.FindTypeByID(ctx, "task")
	require.NoError(t, err)
	prio, err := e.repo.FindPriorityByID(ctx, "high")
	require.NoError(t, err)

	require.NoError(t, first.Edit("first writer", typ, nil, nil, prio, first.StartDate, nil))
	require.NoError(t, second.Edit("second writer", typ, nil, nil, prio, second.StartDate, nil))

	_, err = e.repo.Save(ctx, first)
	require.NoError(t, err)
	_, err = e.repo.Save(ctx, second)
	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.Equal(t, "first writer", e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID))
}

func TestFillAttributes_ConcurrentEditsNeverCorruptTheIssue(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	const n = 8
	codes := make([]int, n)
	names := make([]string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			names[i] = "writer-" + string(rune('a'+i))
			body := fillBody()
			body["name"] = names[i]
			<-start
			codes[i], _ = e.fillAttributes(t, "alice", created.ID, body)
		}(i)
	}
	close(start)
	wg.Wait()

	winners := map[string]bool{}
	for i, c := range codes {
		assert.Contains(t, []int{http.StatusOK, http.StatusConflict}, c, "writer %d", i)
		if c == http.StatusOK {
			winners[names[i]] = true
		}
	}
	require.NotEmpty(t, winners, "at least one writer succeeds")
	assert.True(t, winners[e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID)],
		"the stored issue is the work of a writer that was told it succeeded")
}

func TestViewIssue_HappyPathForAuthorAndAssignee(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	status, raw := e.fillAttributes(t, "alice", created.ID, fillBody()) // assigns bob
	require.Equal(t, http.StatusOK, status, string(raw))

	for _, user := range []string{"alice", "bob"} {
		status, raw := e.viewIssue(t, user, created.ID)
		require.Equal(t, http.StatusOK, status, "%s: %s", user, string(raw))
		got := decodeIssue(t, raw)

		assert.Equal(t, created.ID, got.ID)
		assert.Equal(t, "Login fails on Safari", got.Name)
		assert.Equal(t, ref{ID: "task", Name: "Task"}, got.Type)
		require.NotNil(t, got.Description)
		assert.Equal(t, "<p>updated</p>", *got.Description)
		assert.Equal(t, ref{ID: "alice", Name: "alice"}, got.Author)
		require.NotNil(t, got.Assignee)
		assert.Equal(t, ref{ID: "bob", Name: "bob"}, *got.Assignee)
		assert.Equal(t, ref{ID: "open", Name: "Open"}, got.Status)
		assert.Equal(t, ref{ID: "high", Name: "High"}, got.Priority)
		assert.Equal(t, "2026-11-10", got.StartDate)
		require.NotNil(t, got.DueDate)
		assert.Equal(t, "2026-11-20", *got.DueDate)
	}
}

func TestViewIssue_AccessAndPermissionRules(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	// Unauthenticated
	status, _ := e.viewIssue(t, "", created.ID)
	assert.Equal(t, http.StatusUnauthorized, status)

	// Non-existent issue
	status, _ = e.viewIssue(t, "alice", "ISS-9999")
	assert.Equal(t, http.StatusNotFound, status)

	// Stranger with VIEW_ISSUE permission (carol) cannot view
	status, _ = e.viewIssue(t, "carol", created.ID)
	assert.Equal(t, http.StatusForbidden, status)

	// Assignee without VIEW_ISSUE permission (vera) cannot view
	body := fillBody()
	body["assigneeId"] = "vera"
	status, _ = e.fillAttributes(t, "alice", created.ID, body)
	require.Equal(t, http.StatusOK, status)

	status, _ = e.viewIssue(t, "vera", created.ID)
	assert.Equal(t, http.StatusForbidden, status)
}

func TestEditIssue_HappyPath(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	body := fillBody()
	body["version"] = created.Version
	status, raw := e.editIssue(t, "alice", created.ID, body)
	require.Equal(t, http.StatusOK, status, string(raw))
	got := decodeIssue(t, raw)

	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "Login fails on Safari", got.Name)
	assert.Equal(t, ref{ID: "task", Name: "Task"}, got.Type)
	require.NotNil(t, got.Assignee)
	assert.Equal(t, ref{ID: "bob", Name: "bob"}, *got.Assignee)
	assert.Equal(t, ref{ID: "high", Name: "High"}, got.Priority)
	assert.NotEqual(t, created.Version, got.Version)
}

func TestEditIssue_AccessAndPermissionRules(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	// Unauthenticated
	status, _ := e.editIssue(t, "", created.ID, fillBody())
	assert.Equal(t, http.StatusUnauthorized, status)

	// Non-existent issue
	status, _ = e.editIssue(t, "alice", "ISS-9999", fillBody())
	assert.Equal(t, http.StatusNotFound, status)

	// Stranger with EDIT_ISSUE permission (carol) is denied
	status, _ = e.editIssue(t, "carol", created.ID, fillBody())
	assert.Equal(t, http.StatusForbidden, status)

	// Assignee without EDIT_ISSUE permission (vera) is denied
	assignVera := fillBody()
	assignVera["assigneeId"] = "vera"
	status, _ = e.fillAttributes(t, "alice", created.ID, assignVera)
	require.Equal(t, http.StatusOK, status)

	status, _ = e.editIssue(t, "vera", created.ID, fillBody())
	assert.Equal(t, http.StatusForbidden, status)
}

func TestEditIssue_StaleVersionReturnsConflict(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	firstEdit := fillBody()
	firstEdit["name"] = "First update"
	firstEdit["version"] = created.Version
	status, _ := e.editIssue(t, "alice", created.ID, firstEdit)
	require.Equal(t, http.StatusOK, status)

	secondEdit := fillBody()
	secondEdit["name"] = "Stale update"
	secondEdit["version"] = created.Version // outdated version
	status, _ = e.editIssue(t, "alice", created.ID, secondEdit)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, "First update", e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID))
}