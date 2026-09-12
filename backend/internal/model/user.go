package model

import "time"

// User は users テーブルの写し。Rails: app/models/user.rb
//
// 認証情報(Keycloak sub・ローカルパスワード)は持たない
// 「アプリ固有のプロフィール+権限」のみを表す(CONTRACT.md セクション10・16.2)
// 【セクション16.2で変更】以前は KeycloakSub をこのモデル自身が持っていたが、
// ローカル(非Keycloak)認証を追加するにあたり、RailsのUserCredentialに倣って
// UserKeycloak/UserPasswordへ分離した(3方式のログイン(Keycloak/ローカルHMAC/
// ローカルRSA)を1つのusersテーブルのカラムで表現しようとすると、方式が増える
// たびにusersにNULL許容カラムが増えていく設計になってしまうため)
type User struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	Email     string    `gorm:"column:email"`
	Name      string    `gorm:"column:name"`
	Role      Role      `gorm:"column:role"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`

	// has_many :tasks, dependent: :destroy
	Tasks []Task `gorm:"foreignKey:UserID"`
}

func (User) TableName() string {
	return "users"
}

// RoleManagement はRailsの `role_management?` に対応する
func (u User) RoleManagement() bool {
	return u.Role == RoleManagement
}
