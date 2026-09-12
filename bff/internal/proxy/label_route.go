package proxy

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
)

// LabelClientV1 は backend の /internal/v1/labels(REST限定、gRPC化の対象外)を呼ぶ
// CONTRACT.mdセクション3: ラベルはTaskのようなv1/v2切り替えの対象にしない
type LabelClientV1 struct {
	http *resty.Client
}

func NewLabelClientV1(baseURL string) *LabelClientV1 {
	return &LabelClientV1{http: resty.New().SetBaseURL(baseURL)}
}

func (c *LabelClientV1) List(ctx context.Context, accessToken string) ([]Label, error) {
	var out struct {
		Labels []Label `json:"labels"`
	}
	resp, err := c.http.R().SetContext(ctx).SetAuthToken(accessToken).SetResult(&out).Get("/internal/v1/labels")
	if err := classifyStatus(resp, err); err != nil {
		return nil, err
	}
	return out.Labels, nil
}

// labelRequestBody はbackendの内部v1 API(POST/PATCH /internal/v1/labels)が
// 期待するボディ形状(`internal/handler/v1/label.go`のlabelRequestBodyと一致させる)
type labelRequestBody struct {
	Name string `json:"name"`
}

func (c *LabelClientV1) Create(ctx context.Context, accessToken string, name string) (Label, error) {
	var out Label
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetBody(labelRequestBody{Name: name}).
		SetResult(&out).
		Post("/internal/v1/labels")
	if err := classifyStatus(resp, err); err != nil {
		return Label{}, err
	}
	return out, nil
}

func (c *LabelClientV1) Update(ctx context.Context, accessToken string, id uint64, name string) (Label, error) {
	var out Label
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetBody(labelRequestBody{Name: name}).
		SetResult(&out).
		Patch("/internal/v1/labels/" + strconv.FormatUint(id, 10))
	if err := classifyStatus(resp, err); err != nil {
		return Label{}, err
	}
	return out, nil
}

func (c *LabelClientV1) Delete(ctx context.Context, accessToken string, id uint64) error {
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		Delete("/internal/v1/labels/" + strconv.FormatUint(id, 10))
	return classifyStatus(resp, err)
}

// LabelRoutes は /api/labels* のハンドラ
type LabelRoutes struct {
	Client    *LabelClientV1
	Refresher *auth.Refresher
}

// do はTaskRoutes.doと同じパターン(auth.Refresherを介した401時のリアクティブリフレッシュ+1回だけの再試行)
// Task側とロジックを共有してもよいが、依存を増やさずLabel側だけで完結させるためここに複製している
func (l *LabelRoutes) do(c *gin.Context, sessionID string, fn func(accessToken string) error) bool {
	err := l.Refresher.Do(c.Request.Context(), sessionID, fn)
	if err == nil {
		return true
	}
	if errors.Is(err, auth.ErrUpstreamUnauthorized) || errors.Is(err, auth.ErrSessionNotFound) || errors.Is(err, auth.ErrUserNotProvisioned) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return false
	}
	c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	return false
}

func (l *LabelRoutes) List(c *gin.Context) {
	_, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	var labels []Label
	ok = l.do(c, sessionID, func(accessToken string) error {
		var err error
		labels, err = l.Client.List(c.Request.Context(), accessToken)
		return err
	})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"labels": labels})
}

type labelInput struct {
	Name string `json:"name" binding:"required"`
}

func (l *LabelRoutes) Create(c *gin.Context) {
	_, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var input labelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var label Label
	ok = l.do(c, sessionID, func(accessToken string) error {
		var err error
		label, err = l.Client.Create(c.Request.Context(), accessToken, input.Name)
		return err
	})
	if !ok {
		return
	}
	c.JSON(http.StatusCreated, label)
}

func (l *LabelRoutes) Update(c *gin.Context) {
	_, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var input labelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var label Label
	ok = l.do(c, sessionID, func(accessToken string) error {
		var err error
		label, err = l.Client.Update(c.Request.Context(), accessToken, id, input.Name)
		return err
	})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, label)
}

func (l *LabelRoutes) Delete(c *gin.Context) {
	_, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	ok = l.do(c, sessionID, func(accessToken string) error {
		return l.Client.Delete(c.Request.Context(), accessToken, id)
	})
	if !ok {
		return
	}
	c.Status(http.StatusNoContent)
}
