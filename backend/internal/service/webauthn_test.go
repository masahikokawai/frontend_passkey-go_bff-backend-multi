package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// fakeWebauthnRepo はwebauthnRepositoryのインメモリフェイク実装
type fakeWebauthnRepo struct {
	byCredentialID map[string]*model.WebauthnCredential
	nextID         uint64
}

func newFakeWebauthnRepo() *fakeWebauthnRepo {
	return &fakeWebauthnRepo{byCredentialID: map[string]*model.WebauthnCredential{}}
}

func (f *fakeWebauthnRepo) Create(ctx context.Context, cred *model.WebauthnCredential) error {
	// 【テスト監査で追記】
	// 実際の MySQL の UNIQUE 制約違反エラー文言を模擬する
	// isDuplicateCredentialIDError(文字列マッチ) が正しく機能することを、このfakeを使ったユニットテストでも検証できるようにするため
	if _, exists := f.byCredentialID[string(cred.CredentialID)]; exists {
		return errors.New("Error 1062: Duplicate entry 'x' for key 'webauthn_credentials.index_webauthn_credentials_on_credential_id'")
	}
	f.nextID++
	cred.ID = f.nextID
	f.byCredentialID[string(cred.CredentialID)] = cred
	return nil
}

func (f *fakeWebauthnRepo) FindByCredentialID(ctx context.Context, credentialID []byte) (*model.WebauthnCredential, error) {
	cred, ok := f.byCredentialID[string(credentialID)]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return cred, nil
}

func (f *fakeWebauthnRepo) UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint64) error {
	cred, ok := f.byCredentialID[string(credentialID)]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	cred.SignCount = signCount
	return nil
}

func TestWebauthnService_Register(t *testing.T) {
	repo := newFakeWebauthnRepo()
	s := NewWebauthnService(repo)

	id, err := s.Register(context.Background(), RegisterWebauthnCredentialInput{
		UserID:       1,
		CredentialID: []byte("cred-1"),
		PublicKey:    []byte("pubkey-1"),
		SignCount:    0,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if id == 0 {
		t.Error("Register()が発行したIDが0のまま")
	}
}

// 【テスト監査で追加】同じcredential_idを2回登録しようとした場合、
// MySQLのUNIQUE制約違反(migrations/000013)がisDuplicateCredentialIDErrorで
// ErrValidation(422)へ変換され、生のDBエラー(500相当)がそのまま漏れないことを確認する
// (リプレイ・二重送信・テストコードでの再登録で実際に踏みうるケース)
func TestWebauthnService_Register_同じcredential_idを2回登録するとErrValidation(t *testing.T) {
	repo := newFakeWebauthnRepo()
	s := NewWebauthnService(repo)
	in := RegisterWebauthnCredentialInput{UserID: 1, CredentialID: []byte("dup-cred"), PublicKey: []byte("pk")}

	if _, err := s.Register(context.Background(), in); err != nil {
		t.Fatalf("1回目のRegister() error = %v", err)
	}
	_, err := s.Register(context.Background(), in)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("2回目のRegister() error = %v, ErrValidationを期待", err)
	}
}

func TestWebauthnService_Register_バリデーション(t *testing.T) {
	tests := []struct {
		name string
		in   RegisterWebauthnCredentialInput
	}{
		{name: "credential_id無し", in: RegisterWebauthnCredentialInput{UserID: 1, PublicKey: []byte("pk")}},
		{name: "public_key無し", in: RegisterWebauthnCredentialInput{UserID: 1, CredentialID: []byte("cid")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewWebauthnService(newFakeWebauthnRepo())
			_, err := s.Register(context.Background(), tt.in)
			if !errors.Is(err, ErrValidation) {
				t.Errorf("Register() error = %v, ErrValidationを期待", err)
			}
		})
	}
}

func TestWebauthnService_FindByCredentialID(t *testing.T) {
	repo := newFakeWebauthnRepo()
	s := NewWebauthnService(repo)
	_, _ = s.Register(context.Background(), RegisterWebauthnCredentialInput{
		UserID: 42, CredentialID: []byte("cred-x"), PublicKey: []byte("pk-x"), SignCount: 5,
	})

	got, err := s.FindByCredentialID(context.Background(), []byte("cred-x"))
	if err != nil {
		t.Fatalf("FindByCredentialID() error = %v", err)
	}
	want := WebauthnCredentialDTO{ID: got.ID, UserID: 42, CredentialID: []byte("cred-x"), PublicKey: []byte("pk-x"), SignCount: 5}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(WebauthnCredentialDTO{}, "ID")); diff != "" {
		t.Errorf("FindByCredentialID() 差分 (-want +got):\n%s", diff)
	}
}

// 【実機バグ(CONTRACT.mdセクション22.6)の回帰テスト】
// Register時に渡したBackupEligible/BackupStateが、FindByCredentialIDでの
// 読み出し時にそのまま(falseへ潰れず)返ってくることを確認する
// このラウンドトリップが崩れると、bff側でgo-webauthnの「Backup Eligible flag
// inconsistency」による全ログイン失敗が再発する
func TestWebauthnService_Register_BackupEligibleとBackupStateが往復する(t *testing.T) {
	repo := newFakeWebauthnRepo()
	s := NewWebauthnService(repo)
	_, err := s.Register(context.Background(), RegisterWebauthnCredentialInput{
		UserID: 1, CredentialID: []byte("cred-be"), PublicKey: []byte("pk-be"),
		BackupEligible: true, BackupState: true,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, err := s.FindByCredentialID(context.Background(), []byte("cred-be"))
	if err != nil {
		t.Fatalf("FindByCredentialID() error = %v", err)
	}
	if !got.BackupEligible || !got.BackupState {
		t.Errorf("BackupEligible=%v BackupState=%v, want true/true", got.BackupEligible, got.BackupState)
	}
}

func TestWebauthnService_FindByCredentialID_見つからない(t *testing.T) {
	s := NewWebauthnService(newFakeWebauthnRepo())
	_, err := s.FindByCredentialID(context.Background(), []byte("not-exist"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindByCredentialID() error = %v, ErrNotFoundを期待", err)
	}
}

func TestWebauthnService_UpdateSignCount(t *testing.T) {
	repo := newFakeWebauthnRepo()
	s := NewWebauthnService(repo)
	_, _ = s.Register(context.Background(), RegisterWebauthnCredentialInput{
		UserID: 1, CredentialID: []byte("cred-y"), PublicKey: []byte("pk-y"), SignCount: 1,
	})

	if err := s.UpdateSignCount(context.Background(), []byte("cred-y"), 2); err != nil {
		t.Fatalf("UpdateSignCount() error = %v", err)
	}
	got, err := s.FindByCredentialID(context.Background(), []byte("cred-y"))
	if err != nil {
		t.Fatalf("FindByCredentialID() error = %v", err)
	}
	if got.SignCount != 2 {
		t.Errorf("SignCount = %d, want 2", got.SignCount)
	}
}

func TestWebauthnService_UpdateSignCount_見つからない(t *testing.T) {
	s := NewWebauthnService(newFakeWebauthnRepo())
	err := s.UpdateSignCount(context.Background(), []byte("not-exist"), 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSignCount() error = %v, ErrNotFoundを期待", err)
	}
}
