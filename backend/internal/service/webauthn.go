package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// webauthnRepository はWebauthnServiceが必要とする最小のリポジトリ操作
// *repository.WebauthnCredentialがこれらを実装する(本番の配線は変更不要、
// テストではfakeに差し替えられる)
type webauthnRepository interface {
	Create(ctx context.Context, cred *model.WebauthnCredential) error
	FindByCredentialID(ctx context.Context, credentialID []byte) (*model.WebauthnCredential, error)
	UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint64) error
}

// WebauthnService はパスキー(CONTRACT.mdセクション22)のユースケース
// go-webauthnライブラリ自体はbff側が使うため、backendはあくまで
// credential(公開鍵・sign_count等)の保存/参照層に徹する
type WebauthnService struct {
	repo webauthnRepository
}

func NewWebauthnService(repo webauthnRepository) *WebauthnService {
	return &WebauthnService{repo: repo}
}

// WebauthnCredentialDTO はハンドラ層へ渡す表現
type WebauthnCredentialDTO struct {
	ID             uint64
	UserID         uint64
	CredentialID   []byte
	PublicKey      []byte
	SignCount      uint64
	BackupEligible bool
	BackupState    bool
}

func toWebauthnCredentialDTO(c model.WebauthnCredential) WebauthnCredentialDTO {
	return WebauthnCredentialDTO{
		ID:             c.ID,
		UserID:         c.UserID,
		CredentialID:   c.CredentialID,
		PublicKey:      c.PublicKey,
		SignCount:      c.SignCount,
		BackupEligible: c.BackupEligible,
		BackupState:    c.BackupState,
	}
}

// RegisterWebauthnCredentialInput はパスキー登録時の入力
type RegisterWebauthnCredentialInput struct {
	UserID         uint64
	CredentialID   []byte
	PublicKey      []byte
	SignCount      uint64
	BackupEligible bool
	BackupState    bool
	Transports     *string
	Name           *string
}

// Register はログイン中ユーザーが新しいパスキーを登録する(bffが検証済みのattestationから
// 抽出した値をそのまま保存するだけで、署名検証自体はbff側のgo-webauthnが担う)
func (s *WebauthnService) Register(ctx context.Context, in RegisterWebauthnCredentialInput) (uint64, error) {
	if len(in.CredentialID) == 0 || len(in.PublicKey) == 0 {
		return 0, fmt.Errorf("%w: credential_id/public_keyは必須です", ErrValidation)
	}
	cred := &model.WebauthnCredential{
		UserID:         in.UserID,
		CredentialID:   in.CredentialID,
		PublicKey:      in.PublicKey,
		SignCount:      in.SignCount,
		BackupEligible: in.BackupEligible,
		BackupState:    in.BackupState,
		Transports:     in.Transports,
		Name:           in.Name,
	}
	if err := s.repo.Create(ctx, cred); err != nil {
		// 【テスト監査で発覚・追記】credential_idはUNIQUE制約(migrations/000013)を
		// 持つが、この判定が無い状態だとMySQLの重複エラーがそのままrenderServiceErrorの
		// defaultケース(500 internal_server_error)に落ちてしまい、クライアントにとって原因が分からないエラーになっていた
		//
		// credential_idは暗号論的乱数
		// (十分に長いランダム値)のため実運用で衝突することはまず無いが、リプレイ・
		// 二重送信やテストコードでの再登録では容易に再現するため、他の一意制約違反
		// (isDuplicateEmailError、internal/service/user.go)と同じ判定方法で
		// ErrValidation(422)へ変換し、原因が分かるエラーメッセージを返す
		if isDuplicateCredentialIDError(err) {
			return 0, fmt.Errorf("%w: このcredential_idは既に登録されています", ErrValidation)
		}
		return 0, err
	}
	return cred.ID, nil
}

// isDuplicateCredentialIDError はMySQLのUNIQUE制約違反
// (webauthn_credentials.credential_id、migrations/000013)を検出する
// internal/service/user.goのisDuplicateEmailErrorと同じ判定方法(文字列マッチ)
func isDuplicateCredentialIDError(err error) bool {
	return strings.Contains(err.Error(), "Duplicate entry") && strings.Contains(err.Error(), "credential_id")
}

// FindByCredentialID はdiscoverable credentialでのログイン試行時、bffがcredential_idから
// user_id・公開鍵・sign_countを引くために使う(まだ未認証の呼び出し)
func (s *WebauthnService) FindByCredentialID(ctx context.Context, credentialID []byte) (WebauthnCredentialDTO, error) {
	cred, err := s.repo.FindByCredentialID(ctx, credentialID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return WebauthnCredentialDTO{}, ErrNotFound
		}
		return WebauthnCredentialDTO{}, fmt.Errorf("パスキーの検索に失敗しました: %w", err)
	}
	return toWebauthnCredentialDTO(*cred), nil
}

// UpdateSignCount はログイン成功後にsign_countを更新する(リプレイ攻撃対策)
func (s *WebauthnService) UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint64) error {
	if err := s.repo.UpdateSignCount(ctx, credentialID, signCount); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("sign_countの更新に失敗しました: %w", err)
	}
	return nil
}
