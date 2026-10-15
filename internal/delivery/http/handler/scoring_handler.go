package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/scoring"
	"peelen-ms-be/internal/usecase"
)

type ScoringHandler struct {
	usecase usecase.ScoringUsecase
}

func NewScoringHandler(u usecase.ScoringUsecase) *ScoringHandler {
	return &ScoringHandler{usecase: u}
}

type ruleRequest struct {
	VendorID   string              `json:"vendor_id"`
	SiteID     *string             `json:"site_id"`
	PeriodType string              `json:"period_type"`
	Indicators []entity.RuleItem   `json:"indicators"`
	Dimensions []entity.RuleItem   `json:"dimensions"`
	Levels     []scoring.LevelRule `json:"levels"`
}

func (h *ScoringHandler) Catalog(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var list []scoring.Indicator
		for _, indicator := range h.usecase.Catalog() {
			if kind == entity.RuleKindSLA && indicator.SLA || kind == entity.RuleKindMaturity && indicator.Maturity {
				list = append(list, indicator)
			}
		}
		body := gin.H{"data": list}
		if kind == entity.RuleKindMaturity {
			body["default_levels"] = scoring.DefaultLevels
		}
		c.JSON(http.StatusOK, body)
	}
}

func (h *ScoringHandler) CreateRule(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ruleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
			return
		}
		items := req.Indicators
		if kind == entity.RuleKindMaturity {
			items = req.Dimensions
		}
		rule, err := h.usecase.CreateRule(c.Request.Context(), currentUser(c), entity.ScoreRuleInput{
			Kind: kind, VendorID: req.VendorID, SiteID: req.SiteID, PeriodType: req.PeriodType, Items: items, Levels: req.Levels,
		})
		if err != nil {
			handleWorkflowError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": rule})
	}
}

func (h *ScoringHandler) ListRules(kind string, history bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := entity.RuleFilter{Kind: kind, VendorID: c.Query("vendor_id"), ActiveOnly: !history}
		if site := c.Query("site_id"); site != "" {
			f.SiteID = &site
		}
		rules, err := h.usecase.ListRules(c.Request.Context(), currentUser(c), f)
		if err != nil {
			handleWorkflowError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rules})
	}
}

func (h *ScoringHandler) GetRule(c *gin.Context) {
	rule, err := h.usecase.GetRule(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rule})
}

func (h *ScoringHandler) DeleteRule(c *gin.Context) {
	if err := h.usecase.DeleteRule(c.Request.Context(), currentUser(c), c.Param("id")); err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ScoringHandler) Live(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		results, err := h.usecase.LiveResults(c.Request.Context(), currentUser(c), entity.LiveFilter{
			Kind: kind, Scope: c.DefaultQuery("scope", entity.ScopeVendor),
			VendorID: c.Query("vendor_id"), SiteID: c.Query("site_id"), WorkerID: c.Query("worker_id"),
		})
		if err != nil {
			handleWorkflowError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": results})
	}
}

func (h *ScoringHandler) History(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := entity.ResultFilter{Kind: kind, Scope: c.DefaultQuery("scope", entity.ScopeVendor),
			VendorID: c.Query("vendor_id"), SiteID: c.Query("site_id"), WorkerID: c.Query("worker_id")}
		var err error
		for key, dest := range map[string]*time.Time{"from": &f.From, "to": &f.To} {
			if value := c.Query(key); value != "" {
				if *dest, err = time.Parse("2006-01-02", value); err != nil {
					writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", key+" must be YYYY-MM-DD")
					return
				}
			}
		}
		f.Page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
		f.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
		results, total, err := h.usecase.ListResults(c.Request.Context(), currentUser(c), f)
		if err != nil {
			handleWorkflowError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": results, "page": f.Page, "limit": f.Limit, "total": total})
	}
}
