package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/usecase"
)

type scoringUsecaseFake struct {
	usecase.ScoringUsecase
	input entity.ScoreRuleInput
}

func (f *scoringUsecaseFake) CreateRule(_ context.Context, _ *entity.User, in entity.ScoreRuleInput) (*entity.ScoreRule, error) {
	f.input = in
	return &entity.ScoreRule{ID: "r", Kind: in.Kind}, nil
}

func TestCreateMaturityRuleMapsDimensions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &scoringUsecaseFake{}
	h := NewScoringHandler(fake)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/maturity-rules", bytes.NewBufferString(`{
		"vendor_id": "10000000-0000-4000-8000-000000000001", "period_type": "QUARTER",
		"dimensions": [{"code": "ON_TIME_RATE", "weight": 100}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateRule(entity.RuleKindMaturity)(c)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if fake.input.Kind != "MATURITY" || len(fake.input.Items) != 1 || fake.input.Items[0].Code != "ON_TIME_RATE" {
		t.Fatalf("input = %+v", fake.input)
	}
}
