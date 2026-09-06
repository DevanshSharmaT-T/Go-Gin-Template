// File: internal/modules/health/api/health_handler.go

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/health/service"
)

// HealthHandler serves the probes.
type HealthHandler struct {
	health *service.HealthService
}

// NewHealthHandler builds the handler.
func NewHealthHandler(health *service.HealthService) *HealthHandler {
	return &HealthHandler{health: health}
}

// Live answers the liveness probe. It always returns 200 while the process can
// serve a request at all; see HealthService.Live for why it checks nothing.
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, service.ToResponse(h.health.Live()))
}

// Ready answers the readiness probe.
//
// **The status code is what an orchestrator reads**, not the body: 200 means
// "send me traffic", 503 means "not right now". The body is for a person
// looking at why.
func (h *HealthHandler) Ready(c *gin.Context) {
	var report *service.Report = h.health.Ready(c.Request.Context())

	var status int = http.StatusOK
	if !report.Healthy() {
		status = http.StatusServiceUnavailable
	}

	c.JSON(status, service.ToResponse(report))
}
