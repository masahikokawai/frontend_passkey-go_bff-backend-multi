package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// fakeUserRepo はuserRepositoryのインメモリフェイク実装
// 実MySQLを使わずにJITプロビジョニング・role更新のロジックを検証する
//
// 【セクション16.2で変更】model.Userはkeycloak_subを持たなくなった(user_keycloaksへ分離)
// ため、フェイク内部だけで使うbySubで対応関係を保持する
type fakeUserRepo struct {
	byID   map[uint64]*model.User
	bySub  map[string]*model.User
	nextID uint64
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{byID: map[uint64]*model.User{}, bySub: map[string]*model.User{}}
}

func (f *fakeUserRepo) UpsertByKeycloakSub(ctx context.Context, keycloakSub, email, name string, role model.Role) (*model.User, error) {
	if u, ok := f.bySub[keycloakSub]; ok {
		u.Email = email
		u.Name = name
		if role == model.RoleManagement {
			u.Role = model.RoleManagement
		}
		return u, nil
	}
	f.nextID++
	u := &model.User{ID: f.nextID, Email: email, Name: name, Role: role}
	f.byID[u.ID] = u
	f.bySub[keycloakSub] = u
	return u, nil
}

func (f *fakeUserRepo) List(ctx context.Context) ([]model.User, error) {
	out := make([]model.User, 0, len(f.byID))
	for _, u := range f.byID {
		out = append(out, *u)
	}
	return out, nil
}

func (f *fakeUserRepo) Get(ctx context.Context, id uint64) (*model.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return u, nil
}

func (f *fakeUserRepo) countManagement() int64 {
	var n int64
	for _, u := range f.byID {
		if u.Role == model.RoleManagement {
			n++
		}
	}
	return n
}

// checkNotLastManagerLocked は本物の repository.User.checkNotLastManagerLocked と
// 同じ判定ロジックをインメモリで再現したもの
//
// 実際の行ロック(FOR UPDATE)による
// 同時実行制御そのものはインメモリのfakeでは意味を持たないため再現しないが、
// 「判定結果としてどちらが優先される」という業務ロジックの単体テストはこのfake経由で引き続きできるようにする
//
// 実際の同時実行(TOCTOU)対策の検証は test/integration/user_last_manager_race_test.go の実 DB を使った統合テストが担う
func (f *fakeUserRepo) checkNotLastManagerLocked(id uint64) error {
	target, ok := f.byID[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if target.Role != model.RoleManagement {
		return nil
	}
	if f.countManagement() <= 1 {
		return repository.ErrLastManagerUser
	}
	return nil
}

func (f *fakeUserRepo) UpdateRoleGuarded(ctx context.Context, id uint64, role model.Role) error {
	if role == model.RoleGeneral {
		if err := f.checkNotLastManagerLocked(id); err != nil {
			return err
		}
	}
	u, ok := f.byID[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	u.Role = role
	return nil
}

func (f *fakeUserRepo) Create(ctx context.Context, name, email, passwordDigest string, role model.Role, passwordExpiresAt time.Time) (*model.User, error) {
	for _, u := range f.byID {
		if u.Email == email {
			return nil, fmt.Errorf("Error 1062: Duplicate entry '%s' for key 'index_users_on_email'", email)
		}
	}
	f.nextID++
	u := &model.User{ID: f.nextID, Email: email, Name: name, Role: role}
	f.byID[u.ID] = u
	return u, nil
}

func (f *fakeUserRepo) Delete(ctx context.Context, id uint64) error {
	if err := f.checkNotLastManagerLocked(id); err != nil {
		return err
	}
	delete(f.byID, id)
	return nil
}

func TestUserService_Provision(t *testing.T) {
	ctx := context.Background()

	t.Run("初回はrole=generalで作成される", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		dto, err := s.Provision(ctx, "sub-1", "太郎", "taro@example.com", nil)
		if err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		want := UserDTO{ID: 1, Name: "太郎", Email: "taro@example.com", Role: "general"}
		if diff := cmp.Diff(want, dto); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("rolesにmanagementを含む場合はrole=managementで作成される", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		dto, err := s.Provision(ctx, "sub-2", "花子", "hanako@example.com", []string{"offline_access", "management"})
		if err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		if dto.Role != "management" {
			t.Errorf("Role = %q, want management", dto.Role)
		}
	})

	t.Run("2回目以降は既存行が更新されて返る(IDは変わらない)", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		first, err := s.Provision(ctx, "sub-3", "一郎", "ichiro@example.com", nil)
		if err != nil {
			t.Fatalf("1回目のProvision失敗: %v", err)
		}
		second, err := s.Provision(ctx, "sub-3", "一郎(更新後)", "ichiro@example.com", nil)
		if err != nil {
			t.Fatalf("2回目のProvision失敗: %v", err)
		}
		if second.ID != first.ID {
			t.Errorf("2回目でIDが変わった: first=%d second=%d", first.ID, second.ID)
		}
		if second.Name != "一郎(更新後)" {
			t.Errorf("Name = %q, want 一郎(更新後)", second.Name)
		}
	})

	t.Run("既にmanagementのユーザーはKeycloak側roleが同期不足でもgeneralへ降格しない", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		_, err := s.Provision(ctx, "sub-4", "管理者", "admin@example.com", []string{"management"})
		if err != nil {
			t.Fatalf("1回目のProvision失敗: %v", err)
		}
		second, err := s.Provision(ctx, "sub-4", "管理者", "admin@example.com", nil)
		if err != nil {
			t.Fatalf("2回目のProvision失敗: %v", err)
		}
		if second.Role != "management" {
			t.Errorf("Role = %q, want management(降格しないはず)", second.Role)
		}
	})
}

func TestUserService_List(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUserRepo()
	s := NewUserService(repo)
	_, _ = s.Provision(ctx, "sub-1", "太郎", "taro@example.com", nil)
	_, _ = s.Provision(ctx, "sub-2", "花子", "hanako@example.com", []string{"management"})

	dtos, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List失敗: %v", err)
	}
	if len(dtos) != 2 {
		t.Fatalf("len(dtos) = %d, want 2", len(dtos))
	}
}

// fakePasskeyChecker はpasskeyCheckerのインメモリフェイク実装(CONTRACT.mdセクション22.7)
type fakePasskeyChecker struct {
	userIDs map[uint64]bool
}

func (f *fakePasskeyChecker) UserIDsWithPasskey(ctx context.Context) (map[uint64]bool, error) {
	return f.userIDs, nil
}

func TestUserService_List_HasPasskey未設定なら常にfalse(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUserRepo()
	s := NewUserService(repo) // WithPasskeyCheckerを呼んでいない
	_, _ = s.Provision(ctx, "sub-1", "太郎", "taro@example.com", nil)

	dtos, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List失敗: %v", err)
	}
	if dtos[0].HasPasskey {
		t.Error("passkeyCheckerを設定していないのにHasPasskey=trueになっている")
	}
}

func TestUserService_List_HasPasskey(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUserRepo()
	s := NewUserService(repo).WithPasskeyChecker(&fakePasskeyChecker{userIDs: map[uint64]bool{1: true}})
	_, _ = s.Provision(ctx, "sub-1", "太郎(登録済み)", "taro@example.com", nil)
	_, _ = s.Provision(ctx, "sub-2", "花子(未登録)", "hanako@example.com", nil)

	dtos, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List失敗: %v", err)
	}
	got := map[uint64]bool{}
	for _, dto := range dtos {
		got[dto.ID] = dto.HasPasskey
	}
	if !got[1] {
		t.Error("user_id=1はHasPasskey=trueを期待")
	}
	if got[2] {
		t.Error("user_id=2はHasPasskey=falseを期待")
	}
}

func TestUserService_UpdateRole(t *testing.T) {
	ctx := context.Background()

	t.Run("最後の管理者をgeneralに変更しようとするとエラー", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		if _, err := s.Provision(ctx, "admin-sub", "管理者", "admin@example.com", []string{"management"}); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}

		err := s.UpdateRole(ctx, 1, model.RoleGeneral)
		if !errors.Is(err, ErrLastManagerUser) {
			t.Fatalf("err = %v, want ErrLastManagerUser", err)
		}
	})

	t.Run("管理者が複数いればgeneralへ変更できる", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		if _, err := s.Provision(ctx, "admin-1", "管理者1", "a1@example.com", []string{"management"}); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		if _, err := s.Provision(ctx, "admin-2", "管理者2", "a2@example.com", []string{"management"}); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}

		if err := s.UpdateRole(ctx, 1, model.RoleGeneral); err != nil {
			t.Fatalf("UpdateRole失敗: %v", err)
		}
		users, _ := repo.Get(ctx, 1)
		if users.Role != model.RoleGeneral {
			t.Errorf("Role = %v, want RoleGeneral", users.Role)
		}
	})

	t.Run("generalユーザーをmanagementへ昇格できる", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		if _, err := s.Provision(ctx, "sub-1", "太郎", "taro@example.com", nil); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		if err := s.UpdateRole(ctx, 1, model.RoleManagement); err != nil {
			t.Fatalf("UpdateRole失敗: %v", err)
		}
	})
}

func TestUserService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("name/email/password/roleを指定して作成できる", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)

		dto, err := s.Create(ctx, "花子", "hanako@example.com", "password123", model.RoleGeneral)
		if err != nil {
			t.Fatalf("Create失敗: %v", err)
		}
		if dto.Name != "花子" || dto.Email != "hanako@example.com" || dto.Role != "general" {
			t.Errorf("dto = %+v, 期待した値と不一致", dto)
		}
	})

	t.Run("name/email/passwordのいずれかが空ならErrValidation", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)

		cases := []struct {
			name, email, password string
		}{
			{"", "e@example.com", "pw"},
			{"名前", "", "pw"},
			{"名前", "e2@example.com", ""},
		}
		for _, c := range cases {
			if _, err := s.Create(ctx, c.name, c.email, c.password, model.RoleGeneral); !errors.Is(err, ErrValidation) {
				t.Errorf("Create(%q,%q,%q) err = %v, want ErrValidation", c.name, c.email, c.password, err)
			}
		}
	})

	// 【テスト監査で発見・修正】emailの前後空白がtrimされずに保存されていたため、
	// " user@example.com"(先頭空白付き)と"user@example.com"が別レコードとして
	// 作成できてしまっていた(実機のMySQL(utf8mb4_general_ci)ではUNIQUE制約のPAD SPACE
	// 挙動により末尾空白の差異だけは偶然吸収されるが、先頭空白は吸収されないことを実際に
	// 確認した)。この回帰テストはUserService.Createでのtrim自体を検証する
	// (fakeUserRepoは素朴な文字列比較のため、trimしなければ現在の実装でも再現できる)
	t.Run("emailの前後の空白は保存前に除去される", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)

		dto, err := s.Create(ctx, "花子", "  leading-space@example.com ", "password123", model.RoleGeneral)
		if err != nil {
			t.Fatalf("Create失敗: %v", err)
		}
		if dto.Email != "leading-space@example.com" {
			t.Errorf("Email = %q, 前後の空白が除去されていない", dto.Email)
		}

		// trimされているため、空白無しの同じアドレスは重複として検出されるはず
		_, err = s.Create(ctx, "次郎", "leading-space@example.com", "password456", model.RoleGeneral)
		if !errors.Is(err, ErrEmailTaken) {
			t.Errorf("2回目のCreate() err = %v, want ErrEmailTaken(trim後は同一アドレスのはず)", err)
		}
	})

	t.Run("email重複はErrEmailTaken", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		if _, err := s.Create(ctx, "花子", "dup@example.com", "password123", model.RoleGeneral); err != nil {
			t.Fatalf("1回目のCreate失敗: %v", err)
		}
		_, err := s.Create(ctx, "次郎", "dup@example.com", "password456", model.RoleGeneral)
		if !errors.Is(err, ErrEmailTaken) {
			t.Fatalf("err = %v, want ErrEmailTaken", err)
		}
	})
}

func TestUserService_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("最後の管理者は削除できない", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		if _, err := s.Provision(ctx, "admin-sub", "管理者", "admin@example.com", []string{"management"}); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}

		err := s.Delete(ctx, 1)
		if !errors.Is(err, ErrLastManagerUser) {
			t.Fatalf("err = %v, want ErrLastManagerUser", err)
		}
	})

	t.Run("管理者が複数いれば削除できる", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		if _, err := s.Provision(ctx, "admin-1", "管理者1", "a1@example.com", []string{"management"}); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		if _, err := s.Provision(ctx, "admin-2", "管理者2", "a2@example.com", []string{"management"}); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}

		if err := s.Delete(ctx, 1); err != nil {
			t.Fatalf("Delete失敗: %v", err)
		}
		if _, err := repo.Get(ctx, 1); err == nil {
			t.Errorf("削除したはずのユーザーがまだ取得できる")
		}
	})

	t.Run("generalユーザーは無条件で削除できる", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		if _, err := s.Provision(ctx, "sub-1", "太郎", "taro@example.com", nil); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		if err := s.Delete(ctx, 1); err != nil {
			t.Fatalf("Delete失敗: %v", err)
		}
	})

	t.Run("存在しないIDはErrNotFound", func(t *testing.T) {
		repo := newFakeUserRepo()
		s := NewUserService(repo)
		if err := s.Delete(ctx, 999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
