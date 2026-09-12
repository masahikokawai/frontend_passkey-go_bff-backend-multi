package handler_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/client"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/handler"
)

// fakeUserClient はbackendへのHTTP通信を伴わない、UserHandlerの単体テスト用フェイク
type fakeUserClient struct {
	users         []client.User
	createErr     error
	updateRoleErr error
	deleteErr     error
	lastCreate    client.CreateInput
	lastRoleID    uint64
	lastRole      string
	lastDeleteID  uint64
}

func (f *fakeUserClient) List(ctx context.Context) ([]client.User, error) {
	return f.users, nil
}

func (f *fakeUserClient) Create(ctx context.Context, in client.CreateInput) (client.User, error) {
	f.lastCreate = in
	if f.createErr != nil {
		return client.User{}, f.createErr
	}
	return client.User{ID: 99, Name: in.Name, Email: in.Email, Role: in.Role}, nil
}

func (f *fakeUserClient) UpdateRole(ctx context.Context, id uint64, role string) error {
	f.lastRoleID = id
	f.lastRole = role
	return f.updateRoleErr
}

func (f *fakeUserClient) Delete(ctx context.Context, id uint64) error {
	f.lastDeleteID = id
	return f.deleteErr
}

func newTestRouterWithFakeUsers(t *testing.T, fake *fakeUserClient) http.Handler {
	t.Helper()
	handler.LoadTemplates("../../web/templates/*.html")
	uh := handler.NewUserHandler(fake)
	// このテストファイルは /users 系ルートしか叩かないため、FeatureFlagHandlerは
	// nilのまま渡して構わない(nilレシーバのメソッド値はGinへの登録時点では
	// 呼び出されないため、"/"・"/flags/*" に実際にリクエストしない限り問題ない)
	return handler.NewRouter(nil, uh, testBasicAuthUser, testBasicAuthPassword)
}

func TestUserHandler_Index_ListsUsers(t *testing.T) {
	fake := &fakeUserClient{users: []client.User{{ID: 1, Name: "太郎", Email: "taro@example.com", Role: "general"}}}
	router := newTestRouterWithFakeUsers(t, fake)

	w := doRequest(router, http.MethodGet, "/users", nil, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "taro@example.com") {
		t.Errorf("一覧に taro@example.com が表示されていない: %s", w.Body.String())
	}
}

// CONTRACT.mdセクション22.7: 一覧にパスキー登録有無が表示されることを確認する
func TestUserHandler_Index_ShowsPasskeyStatus(t *testing.T) {
	fake := &fakeUserClient{users: []client.User{
		{ID: 1, Name: "登録済み太郎", Email: "with-passkey@example.com", Role: "general", HasPasskey: true},
		{ID: 2, Name: "未登録花子", Email: "without-passkey@example.com", Role: "general", HasPasskey: false},
	}}
	router := newTestRouterWithFakeUsers(t, fake)

	w := doRequest(router, http.MethodGet, "/users", nil, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "登録済み") {
		t.Errorf("HasPasskey=trueのユーザーに「登録済み」の表示が無い: %s", body)
	}
	if !strings.Contains(body, "未登録") {
		t.Errorf("HasPasskey=falseのユーザーに「未登録」の表示が無い: %s", body)
	}
}

func TestUserHandler_Create_Success_RedirectsToUsers(t *testing.T) {
	fake := &fakeUserClient{}
	router := newTestRouterWithFakeUsers(t, fake)

	form := url.Values{"name": {"新規"}, "email": {"new@example.com"}, "password": {"password"}, "role": {"general"}}
	w := doRequest(router, http.MethodPost, "/users", form, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if got := w.Header().Get("Location"); got != "/users" {
		t.Errorf("Location = %q, want /users", got)
	}
	if fake.lastCreate.Email != "new@example.com" {
		t.Errorf("Create()に渡されたemail = %q, want new@example.com", fake.lastCreate.Email)
	}
}

func TestUserHandler_Create_ValidationError_ShowsMessage(t *testing.T) {
	fake := &fakeUserClient{createErr: client.ErrValidation}
	router := newTestRouterWithFakeUsers(t, fake)

	form := url.Values{"name": {"x"}, "email": {"dup@example.com"}, "password": {"p"}, "role": {"general"}}
	w := doRequest(router, http.MethodPost, "/users", form, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestUserHandler_UpdateRole_LastManagerGuard_ReturnsConflict(t *testing.T) {
	fake := &fakeUserClient{updateRoleErr: client.ErrLastManager}
	router := newTestRouterWithFakeUsers(t, fake)

	form := url.Values{"role": {"general"}}
	w := doRequest(router, http.MethodPost, "/users/3/role", form, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	if fake.lastRoleID != 3 || fake.lastRole != "general" {
		t.Errorf("UpdateRole()呼び出し引数 = (%d, %q), want (3, general)", fake.lastRoleID, fake.lastRole)
	}
}

func TestUserHandler_Delete_Success_RedirectsToUsers(t *testing.T) {
	fake := &fakeUserClient{}
	router := newTestRouterWithFakeUsers(t, fake)

	w := doRequest(router, http.MethodPost, "/users/4/delete", url.Values{}, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if fake.lastDeleteID != 4 {
		t.Errorf("Delete()に渡されたID = %d, want 4", fake.lastDeleteID)
	}
}

func TestUserHandler_Delete_LastManagerGuard_ReturnsConflict(t *testing.T) {
	fake := &fakeUserClient{deleteErr: client.ErrLastManager}
	router := newTestRouterWithFakeUsers(t, fake)

	w := doRequest(router, http.MethodPost, "/users/4/delete", url.Values{}, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestUserHandler_RequireBasicAuth(t *testing.T) {
	router := newTestRouterWithFakeUsers(t, &fakeUserClient{})

	w := doRequest(router, http.MethodGet, "/users", nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d(Basic Auth無し)", w.Code, http.StatusUnauthorized)
	}
}
