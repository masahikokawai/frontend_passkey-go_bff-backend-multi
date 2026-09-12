package v1

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// parseUintParam はGinのパスパラメータをuint64へ変換する共通ヘルパー
//
// Rails対比: Railsは `params[:id]` を文字列のまま受け取りActiveRecordが暗黙に
// 型変換してくれるが、Goでは型が静的なため、この変換を明示的なコードとして書く必要がある
func parseUintParam(c *gin.Context, name string) (uint64, error) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("パラメータ %s の変換に失敗: %w", name, err)
	}
	return v, nil
}

func parseUintQuery(c *gin.Context, name string) (uint64, bool) {
	raw := c.Query(name)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseUintListQuery(c *gin.Context, name string) []uint64 {
	raw := c.Query(name)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]uint64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseUint(strings.TrimSpace(p), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

func parseRole(s string) (model.Role, error) {
	return model.RoleFromString(s)
}
