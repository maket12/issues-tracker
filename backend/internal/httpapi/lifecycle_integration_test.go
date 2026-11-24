// Integration tests (real HTTP -> service -> MySQL) for LC-1 .. LC-4, MA-4 and MA-5.
// They reuse the harness of issue_integration_test.go: users alice/bob/carol (member), vera (viewer),
// dana (admin: CONFIGURE_LIFECYCLE) and quinn (role qa, no permissions); statuses open and done.
package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maket12/issues-tracker/backend/internal/domain"
	"github.com/maket12/issues-tracker/backend/internal/repository"
	"github.com/maket12/issues-tracker/backend/internal/service"
)

type transitionResp struct {
	From         ref   `json:"from"`
	To           ref   `json:"to"`
	AllowAuthor  bool  `json:"allowAuthor"`
	AllowedRoles []ref `json:"allowedRoles"`
}

func (e *env) defineStatus(t *testing.T, user, name string) (int, []byte) {
	t.Helper()
	return e.do(t, http.MethodPost, "/api/statuses", user, map[string]any{"name": name})
}

func (e *env) defineTransition(t *testing.T, user, from, to string) (int, []byte) {
	t.Helper()
	return e.do(t, http.MethodPost, "/api/transitions", user, map[string]any{"fromId": from, "toId": to})
}

func (e *env) setRules(t *testing.T, user, from, to, role string, allowAuthor bool) (int, []byte) {
	t.Helper()
	return e.do(t, http.MethodPut, "/api/transitions/"+from+"/"+to, user,
		map[string]any{"roleId": role, "allowAuthor": allowAuthor})
}

func (e *env) changeStatus(t *testing.T, user, issueID, statusID string) (int, []byte) {
	t.Helper()
	return e.do(t, http.MethodPost, "/api/issues/"+issueID+"/status", user, map[string]any{"statusId": statusID})
}

// allowTransition defines from -> to and sets its rule, as the administrator dana.
func (e *env) allowTransition(t *testing.T, from, to, role string, allowAuthor bool) {
	t.Helper()
	status, raw := e.defineTransition(t, "dana", from, to)
	require.Equal(t, http.StatusCreated, status, string(raw))
	status, raw = e.setRules(t, "dana", from, to, role, allowAuthor)
	require.Equal(t, http.StatusOK, status, string(raw))
}

func (e *env) storedStatus(t *testing.T, issueID string) string {
	return e.text(t, "SELECT s.id FROM Issue i JOIN Statuses s ON s.`key` = i.status WHERE i.id = ?", issueID)
}

// ================================================================ LC-1 define statuses

func TestDefineStatus_Registers(t *testing.T) {
	e := newEnv(t)

	status, raw := e.defineStatus(t, "dana", "  In Review ")

	require.Equal(t, http.StatusCreated, status, string(raw))
	var got ref
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Regexp(t, `^STS-\d+$`, got.ID)
	assert.Equal(t, "In Review", got.Name)
	assert.Equal(t, 1, e.count(t, "SELECT COUNT(*) FROM Statuses WHERE id = ? AND name = 'In Review'", got.ID))
}

func TestDefineStatus_Rejections(t *testing.T) {
	e := newEnv(t)
	before := e.count(t, "SELECT COUNT(*) FROM Statuses")

	status, _ := e.defineStatus(t, "", "Fresh")
	assert.Equal(t, http.StatusUnauthorized, status)
	for _, user := range []string{"alice", "vera", "quinn"} { // none of them has CONFIGURE_LIFECYCLE
		status, _ = e.defineStatus(t, user, "Fresh")
		assert.Equal(t, http.StatusForbidden, status, user)
	}

	for name, c := range map[string]struct {
		input string
		code  int
	}{
		"empty name":               {"", http.StatusUnprocessableEntity},
		"blank name":               {"   ", http.StatusUnprocessableEntity},
		"name too long":            {strings.Repeat("я", 101), http.StatusUnprocessableEntity},
		"duplicate":                {"Open", http.StatusConflict},
		"duplicate, other case":    {"dONE", http.StatusConflict},
		"duplicate, other accents": {"Ópen", http.StatusConflict}, // the database compares names accent-insensitively
	} {
		status, raw := e.defineStatus(t, "dana", c.input)
		assert.Equal(t, c.code, status, "%s: %s", name, raw)
	}
	assert.Equal(t, before, e.count(t, "SELECT COUNT(*) FROM Statuses"), "nothing was registered")

	status, _ = e.do(t, http.MethodPost, "/api/statuses", "dana", `{"name":"X","color":"red"}`)
	assert.Equal(t, http.StatusBadRequest, status, "unknown field")
}

// ================================================================ LC-2 define transitions

func TestDefineTransition_CreatesAnUnusableTransition(t *testing.T) {
	e := newEnv(t)

	status, raw := e.defineTransition(t, "dana", "open", "done")

	require.Equal(t, http.StatusCreated, status, string(raw))
	var got transitionResp
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "open", got.From.ID)
	assert.Equal(t, "done", got.To.ID)
	assert.False(t, got.AllowAuthor)
	assert.Empty(t, got.AllowedRoles, "nobody may use a new transition until LC-3 grants it")
	assert.Equal(t, 1, e.count(t, "SELECT COUNT(*) FROM TransitionRules"))
}

func TestDefineTransition_Rejections(t *testing.T) {
	e := newEnv(t)

	status, _ := e.defineTransition(t, "", "open", "done")
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = e.defineTransition(t, "alice", "open", "done")
	assert.Equal(t, http.StatusForbidden, status)

	status, raw := e.defineTransition(t, "dana", "ghost", "phantom")
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Contains(t, fieldsOf(t, raw), "fromId")
	assert.Contains(t, fieldsOf(t, raw), "toId")

	status, raw = e.defineTransition(t, "dana", "open", "open")
	require.Equal(t, http.StatusUnprocessableEntity, status, "a status cannot lead to itself")
	assert.Contains(t, fieldsOf(t, raw), "toId")

	status, _ = e.defineTransition(t, "dana", "", "")
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, 0, e.count(t, "SELECT COUNT(*) FROM TransitionRules"))

	status, _ = e.defineTransition(t, "dana", "open", "done")
	require.Equal(t, http.StatusCreated, status)
	status, _ = e.defineTransition(t, "dana", "open", "done")
	assert.Equal(t, http.StatusConflict, status, "duplicate link")
	status, _ = e.defineTransition(t, "dana", "done", "open")
	assert.Equal(t, http.StatusCreated, status, "the opposite direction is a different transition")
	assert.Equal(t, 2, e.count(t, "SELECT COUNT(*) FROM TransitionRules"))
}

// ================================================================ LC-3 set transition rules

func TestSetTransitionRules_StoresRolesAndTheAuthorFlag(t *testing.T) {
	e := newEnv(t)
	status, _ := e.defineTransition(t, "dana", "open", "done")
	require.Equal(t, http.StatusCreated, status)

	status, raw := e.setRules(t, "dana", "open", "done", "qa", true)
	require.Equal(t, http.StatusOK, status, string(raw))
	var got transitionResp
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, []ref{{ID: "qa", Name: "QA"}}, got.AllowedRoles)
	assert.True(t, got.AllowAuthor)

	// another role is added, the first one stays; the author flag follows the last call
	status, raw = e.setRules(t, "dana", "open", "done", "member", false)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.ElementsMatch(t, []ref{{ID: "qa", Name: "QA"}, {ID: "member", Name: "Member"}}, got.AllowedRoles)
	assert.False(t, got.AllowAuthor)

	// the same role again changes nothing
	status, _ = e.setRules(t, "dana", "open", "done", "qa", false)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, 2, e.count(t, "SELECT COUNT(*) FROM TransitionRules_Roles"))
	assert.Equal(t, 0, e.count(t, "SELECT COUNT(*) FROM TransitionRules WHERE allowAuthor"))
}

func TestSetTransitionRules_Rejections(t *testing.T) {
	e := newEnv(t)
	status, _ := e.defineTransition(t, "dana", "open", "done")
	require.Equal(t, http.StatusCreated, status)

	status, _ = e.setRules(t, "", "open", "done", "qa", false)
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = e.setRules(t, "alice", "open", "done", "qa", false)
	assert.Equal(t, http.StatusForbidden, status)

	status, _ = e.setRules(t, "dana", "done", "open", "qa", false)
	assert.Equal(t, http.StatusNotFound, status, "the transition done -> open is not defined")

	status, raw := e.setRules(t, "dana", "open", "done", "ghost", false)
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Contains(t, fieldsOf(t, raw), "roleId", "role missing: the rule is not saved")
	status, raw = e.setRules(t, "dana", "open", "done", "", false)
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Contains(t, fieldsOf(t, raw), "roleId")
	status, raw = e.setRules(t, "dana", "ghost", "done", "qa", false)
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Contains(t, fieldsOf(t, raw), "fromId")

	assert.Equal(t, 0, e.count(t, "SELECT COUNT(*) FROM TransitionRules_Roles"))
	status, _ = e.do(t, http.MethodPut, "/api/transitions/open/done", "dana", `{"roleId":"qa","everyone":true}`)
	assert.Equal(t, http.StatusBadRequest, status, "unknown field")
}

// ================================================================ LC-4 / LC-5 change issue status

func TestChangeIssueStatus_OnlyTheRuleDecides(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t) // alice's issue, status open
	require.Equal(t, "open", created.Status.ID)
	e.allowTransition(t, "open", "done", "qa", false)

	// the author is not a qa and allowAuthor is off: the role lacks the right
	status, _ := e.changeStatus(t, "alice", created.ID, "done")
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, "open", e.storedStatus(t, created.ID))

	// quinn (qa) may move any issue, although he has no permission at all and is neither author nor assignee
	status, raw := e.changeStatus(t, "quinn", created.ID, "done")
	require.Equal(t, http.StatusOK, status, string(raw))
	got := decodeIssue(t, raw)
	assert.Equal(t, ref{ID: "done", Name: "Done"}, got.Status)
	assert.NotEqual(t, created.Version, got.Version, "a status change is a save: the version changes")
	assert.Equal(t, "done", e.storedStatus(t, created.ID))
	assert.Equal(t, "alice", got.Author.ID, "nothing else changed")
	assert.Equal(t, created.Name, got.Name)

	// the new status is what everybody sees
	status, raw = e.viewIssue(t, "alice", created.ID)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "done", decodeIssue(t, raw).Status.ID)

	// done -> open was never defined
	status, _ = e.changeStatus(t, "quinn", created.ID, "open")
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, "done", e.storedStatus(t, created.ID))
}

func TestChangeIssueStatus_TheAuthorTransition(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	e.allowTransition(t, "open", "done", "qa", true)

	status, _ := e.changeStatus(t, "carol", created.ID, "done") // neither author nor qa
	assert.Equal(t, http.StatusForbidden, status)

	status, raw := e.changeStatus(t, "alice", created.ID, "done") // the author
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "done", e.storedStatus(t, created.ID))
}

func TestChangeIssueStatus_ATransitionNobodyMayUse(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	status, _ := e.defineTransition(t, "dana", "open", "done") // LC-2 only, no LC-3
	require.Equal(t, http.StatusCreated, status)

	for _, user := range []string{"alice", "quinn", "dana"} {
		status, _ = e.changeStatus(t, user, created.ID, "done")
		assert.Equal(t, http.StatusForbidden, status, user)
	}
	assert.Equal(t, "open", e.storedStatus(t, created.ID))
}

func TestChangeIssueStatus_InputProblems(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	e.allowTransition(t, "open", "done", "qa", false)

	status, _ := e.changeStatus(t, "", created.ID, "done")
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = e.changeStatus(t, "quinn", "ISS-9999", "done")
	assert.Equal(t, http.StatusNotFound, status)

	status, raw := e.changeStatus(t, "quinn", created.ID, "limbo")
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Contains(t, fieldsOf(t, raw), "statusId")
	status, raw = e.changeStatus(t, "quinn", created.ID, "")
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Contains(t, fieldsOf(t, raw), "statusId")

	status, _ = e.changeStatus(t, "quinn", created.ID, "open") // staying in the same status
	assert.Equal(t, http.StatusConflict, status)

	status, _ = e.do(t, http.MethodPost, "/api/issues/"+created.ID+"/status", "quinn", `{"statusId":"done","force":true}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "open", e.storedStatus(t, created.ID), "nothing changed")
}

func TestChangeIssueStatus_ConcurrentRequestsMoveTheIssueOnce(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	e.allowTransition(t, "open", "done", "qa", false)

	const n = 6
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i], _ = e.changeStatus(t, "quinn", created.ID, "done")
		}(i)
	}
	close(start)
	wg.Wait()

	ok := 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		default:
			// either the issue was already changed (version conflict) or it is already in "done" (no such transition)
			assert.Equal(t, http.StatusConflict, c)
		}
	}
	assert.Equal(t, 1, ok, "exactly one request moves the issue: %v", codes)
	assert.Equal(t, "done", e.storedStatus(t, created.ID))
}

// A stale copy of an issue can never overwrite a newer status (the version guards the whole row).
func TestSave_StaleStatusChangeIsRejected(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)
	ctx := context.Background()

	first, err := e.repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	second, err := e.repo.FindByID(ctx, created.ID) // same version as `first`
	require.NoError(t, err)
	done, err := e.repo.FindStatusByID(ctx, "done")
	require.NoError(t, err)
	require.NoError(t, first.ChangeStatus(done))
	require.NoError(t, second.ChangeStatus(done))

	_, err = e.repo.Save(ctx, first)
	require.NoError(t, err)
	_, err = e.repo.Save(ctx, second)
	assert.ErrorIs(t, err, domain.ErrConflict)

	_, err = e.repo.FindStatusByID(ctx, "no-such-status")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

// ================================================================ MA-5 work simultaneously

func TestEditIssue_ExpectedVersionIsRequired(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t)

	status, raw := e.editIssue(t, "alice", created.ID, fillBody()) // no "version"
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	assert.Contains(t, fieldsOf(t, raw), "version")
	assert.Equal(t, created.Name, e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID))
}

func TestEditIssue_ConcurrentEditsOfTheSameVersionHaveOneWinner(t *testing.T) {
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
			body["name"], body["version"] = names[i], created.Version // everybody opened version 1
			<-start
			codes[i], _ = e.editIssue(t, "alice", created.ID, body)
		}(i)
	}
	close(start)
	wg.Wait()

	winner := ""
	for i, c := range codes {
		switch c {
		case http.StatusOK:
			assert.Empty(t, winner, "only one writer may win: %v", codes)
			winner = names[i]
		default:
			assert.Equal(t, http.StatusConflict, c, "writer %d is asked to refresh", i)
		}
	}
	require.NotEmpty(t, winner, "one writer succeeds: %v", codes)
	assert.Equal(t, winner, e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID), "the stored issue is the winner's")
}

func TestEditIssue_AStatusChangeMakesOlderCopiesStale(t *testing.T) {
	e := newEnv(t)
	created := e.newIssue(t) // alice opens it at created.Version
	e.allowTransition(t, "open", "done", "qa", false)
	status, _ := e.changeStatus(t, "quinn", created.ID, "done")
	require.Equal(t, http.StatusOK, status)

	body := fillBody()
	body["version"] = created.Version
	status, _ = e.editIssue(t, "alice", created.ID, body)
	assert.Equal(t, http.StatusConflict, status, "alice must refresh: the issue changed under her")
	assert.Equal(t, created.Name, e.text(t, "SELECT name FROM Issue WHERE id = ?", created.ID))

	// after refreshing she can edit, and the status is kept
	status, raw := e.viewIssue(t, "alice", created.ID)
	require.Equal(t, http.StatusOK, status)
	fresh := decodeIssue(t, raw)
	body["version"] = fresh.Version
	status, raw = e.editIssue(t, "alice", created.ID, body)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, "done", decodeIssue(t, raw).Status.ID, "an attribute edit does not undo the status change")
}

// ================================================================ MA-4 check permissions

func TestAccessService_ChecksTheRoleStoredInTheDatabase(t *testing.T) {
	e := newEnv(t)
	access := service.NewAccessService(repository.NewUserRepo(e.admin))
	ctx := context.Background()

	granted, err := access.CheckPermissions(ctx, "dana", domain.ConfigureLifecycle)
	require.NoError(t, err)
	assert.True(t, granted)

	for _, id := range []string{"alice", "vera", "quinn"} {
		granted, err = access.CheckPermissions(ctx, id, domain.ConfigureLifecycle)
		assert.False(t, granted, id)
		assert.ErrorIs(t, err, domain.ErrForbidden, id)
	}
	granted, err = access.CheckPermissions(ctx, "alice", domain.CreateIssue)
	require.NoError(t, err)
	assert.True(t, granted)

	granted, err = access.CheckPermissions(ctx, "ghost", domain.ViewIssue)
	assert.False(t, granted)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)

	// the role is determined on every check: after promoting alice she is allowed
	e.admin.MustExec("UPDATE Users SET role = (SELECT `key` FROM Roles WHERE id = 'admin') WHERE id = 'alice'")
	granted, err = access.CheckPermissions(ctx, "alice", domain.ConfigureLifecycle)
	require.NoError(t, err)
	assert.True(t, granted)
}

// ================================================================ LifecycleRepo

func TestLifecycleRepo_SavingNeverErasesAnotherAdministratorsWork(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	lifecycles, roles, statuses := repository.NewLifecycleRepo(e.admin), repository.NewRoleRepo(e.admin), repository.NewStatusRepo(e.admin)
	open, _ := statuses.FindByID(ctx, "open")
	done, _ := statuses.FindByID(ctx, "done")
	qa, _ := roles.FindByID(ctx, "qa")
	member, _ := roles.FindByID(ctx, "member")

	// two administrators read the same graph, then each adds a different transition
	first, err := lifecycles.GetLifecycle(ctx)
	require.NoError(t, err)
	second, err := lifecycles.GetLifecycle(ctx)
	require.NoError(t, err)
	r1, _ := domain.NewTransitionRule(open, done)
	r1.AddRole(qa)
	require.NoError(t, first.AddTransitionRule(r1))
	r2, _ := domain.NewTransitionRule(done, open)
	r2.AddRole(member)
	r2.SetAllowAuthor(true)
	require.NoError(t, second.AddTransitionRule(r2))

	_, err = lifecycles.Save(ctx, first)
	require.NoError(t, err)
	saved, err := lifecycles.Save(ctx, second) // `second` knows nothing about r1
	require.NoError(t, err)

	require.Len(t, saved.GetRules(), 2, "both transitions survive")
	forward := saved.FindRule(open, done)
	require.NotNil(t, forward)
	assert.Equal(t, "qa", forward.GetAllowedRoles()[0].ID)
	assert.False(t, forward.AllowsAuthor())
	back := saved.FindRule(done, open)
	require.NotNil(t, back)
	assert.Equal(t, "member", back.GetAllowedRoles()[0].ID)
	assert.True(t, back.AllowsAuthor())
	assert.Len(t, saved.GetStatuses(), 2)
	assert.Equal(t, "open", saved.GetStatuses()[0].ID, "statuses come in the order they were defined")
}

func TestLifecycleRepo_AFailedSaveLeavesNothingBehind(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	lifecycles, statuses := repository.NewLifecycleRepo(e.admin), repository.NewStatusRepo(e.admin)
	open, _ := statuses.FindByID(ctx, "open")
	done, _ := statuses.FindByID(ctx, "done")

	lc, err := lifecycles.GetLifecycle(ctx)
	require.NoError(t, err)
	rule, _ := domain.NewTransitionRule(open, done)
	rule.AddRole(&domain.Role{ID: "no-such-role"}) // does not exist
	require.NoError(t, lc.AddTransitionRule(rule))

	_, err = lifecycles.Save(ctx, lc)

	assert.Error(t, err, "an unknown role is an error, not silently skipped")
	assert.Equal(t, 0, e.count(t, "SELECT COUNT(*) FROM TransitionRules"), "the transaction was rolled back")
}

func TestStatusRepo_SaveAssignsTheIDInPlaceAndRenames(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	statuses := repository.NewStatusRepo(e.admin)

	fresh, err := domain.NewStatus("In Review")
	require.NoError(t, err)
	saved, err := statuses.Save(ctx, fresh)
	require.NoError(t, err)
	assert.Regexp(t, `^STS-\d+$`, saved.ID)
	assert.Equal(t, saved.ID, fresh.ID, "the object that is part of the lifecycle gets the stored id")

	saved.Name = "Code Review"
	renamed, err := statuses.Save(ctx, saved)
	require.NoError(t, err)
	assert.Equal(t, "Code Review", renamed.Name)

	saved.Name = "Open" // taken by another status
	_, err = statuses.Save(ctx, saved)
	assert.ErrorIs(t, err, domain.ErrStatusExists)
}
