package http

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"anpr-service/internal/config"
	"anpr-service/internal/domain/anpr"
	"anpr-service/internal/service"
)

type Handler struct {
	anprService      *service.ANPRService
	config           *config.Config
	log              zerolog.Logger
}

func NewHandler(
	anprService *service.ANPRService,
	cfg *config.Config,
	log zerolog.Logger,
) *Handler {
	return &Handler{
		anprService: anprService,
		config:      cfg,
		log:         log,
	}
}

func (h *Handler) Register(r *gin.Engine, authMiddleware gin.HandlerFunc) {
	// Public endpoints
	public := r.Group("/api/v1")
	{
		public.POST("/anpr/events", h.createANPREvent)
		public.GET("/plates", h.listPlates)
		public.GET("/events", h.listEvents)
	}

	// Protected endpoints (if needed in future)
	protected := r.Group("/api/v1")
	protected.Use(authMiddleware)
	{
		// Add protected endpoints here
	}
}

func (h *Handler) createANPREvent(c *gin.Context) {
	var payload anpr.EventPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
		return
	}

	if payload.EventTime.IsZero() {
		payload.EventTime = time.Now()
	}

	result, err := h.anprService.ProcessIncomingEvent(c.Request.Context(), payload, h.config.Camera.Model)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
			return
		}
		h.log.Error().Err(err).Msg("failed to process ANPR event")
		c.JSON(http.StatusInternalServerError, errorResponse("internal error"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":   "ok",
		"event_id": result.EventID,
		"plate_id": result.PlateID,
		"plate":    result.Plate,
		"hits":     result.Hits,
	})
}

func (h *Handler) listPlates(c *gin.Context) {
	plateQuery := strings.TrimSpace(c.Query("plate"))
	if plateQuery == "" {
		c.JSON(http.StatusBadRequest, errorResponse("plate parameter is required"))
		return
	}

	plates, err := h.anprService.FindPlates(c.Request.Context(), plateQuery)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
			return
		}
		h.log.Error().Err(err).Msg("failed to find plates")
		c.JSON(http.StatusInternalServerError, errorResponse("internal error"))
		return
	}

	c.JSON(http.StatusOK, successResponse(plates))
}

func (h *Handler) listEvents(c *gin.Context) {
	var plateQuery *string
	if plate := strings.TrimSpace(c.Query("plate")); plate != "" {
		plateQuery = &plate
	}

	var from, to *string
	if f := strings.TrimSpace(c.Query("from")); f != "" {
		from = &f
	}
	if t := strings.TrimSpace(c.Query("to")); t != "" {
		to = &t
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := parseInt(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	offset := 0
	if o := c.Query("offset"); o != "" {
		if parsed, err := parseInt(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	events, err := h.anprService.FindEvents(c.Request.Context(), plateQuery, from, to, limit, offset)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
			return
		}
		h.log.Error().Err(err).Msg("failed to find events")
		c.JSON(http.StatusInternalServerError, errorResponse("internal error"))
		return
	}

	c.JSON(http.StatusOK, successResponse(events))
}

func (h *Handler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
	case errors.Is(err, service.ErrNotFound):
		c.JSON(http.StatusNotFound, errorResponse(err.Error()))
	default:
		h.log.Error().Err(err).Msg("handler error")
		c.JSON(http.StatusInternalServerError, errorResponse("internal error"))
	}
}

func successResponse(data interface{}) gin.H {
	return gin.H{
		"data": data,
	}
}

func errorResponse(message string) gin.H {
	return gin.H{
		"error": message,
	}
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

