package http

import (
	"encoding/xml"
	"errors"
	"io"
	"mime/multipart"
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
	anprService *service.ANPRService
	config      *config.Config
	log         zerolog.Logger
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
		public.POST("/anpr/hikvision", h.createHikvisionEvent)
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

func (h *Handler) createHikvisionEvent(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
		h.log.Error().Err(err).Msg("failed to parse multipart request")
		c.JSON(http.StatusBadRequest, errorResponse("invalid multipart payload"))
		return
	}

	xmlPayload, err := extractXMLPayload(c.Request.MultipartForm)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to extract xml payload")
		c.JSON(http.StatusBadRequest, errorResponse("xml payload not found"))
		return
	}

	hikEvent := &hikvisionEvent{}
	if err := xml.Unmarshal(xmlPayload, hikEvent); err != nil {
		h.log.Error().Err(err).Msg("failed to parse hikvision xml")
		c.JSON(http.StatusBadRequest, errorResponse("invalid xml payload"))
		return
	}

	payload := hikEvent.ToEventPayload()

	if payload.CameraID == "" {
		cameraID := c.Query("camera_id")
		if cameraID == "" {
			cameraID = h.config.Camera.HTTPHost
		}
		payload.CameraID = cameraID
	}
	if payload.CameraModel == "" {
		payload.CameraModel = h.config.Camera.Model
	}
	if payload.EventTime.IsZero() {
		payload.EventTime = time.Now()
	}
	if payload.RawPayload == nil {
		payload.RawPayload = map[string]interface{}{
			"xml": string(xmlPayload),
		}
	}

	result, err := h.anprService.ProcessIncomingEvent(c.Request.Context(), payload, h.config.Camera.Model)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
			return
		}
		h.log.Error().Err(err).Msg("failed to process hikvision event")
		c.JSON(http.StatusInternalServerError, errorResponse("internal error"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":    "ok",
		"event_id":  result.EventID,
		"plate_id":  result.PlateID,
		"plate":     result.Plate,
		"hits":      result.Hits,
		"processed": true,
	})
}

func extractXMLPayload(form *multipart.Form) ([]byte, error) {
	if form == nil {
		return nil, errors.New("empty form")
	}

	for _, files := range form.File {
		for _, fh := range files {
			if isXMLFile(fh) {
				file, err := fh.Open()
				if err != nil {
					return nil, err
				}
				defer file.Close()
				return io.ReadAll(file)
			}
		}
	}

	for key, values := range form.Value {
		if strings.Contains(strings.ToLower(key), "xml") && len(values) > 0 {
			return []byte(values[0]), nil
		}
	}

	return nil, errors.New("xml file not found")
}

func isXMLFile(fh *multipart.FileHeader) bool {
	filename := strings.ToLower(fh.Filename)
	if strings.HasSuffix(filename, ".xml") {
		return true
	}
	contentType := strings.ToLower(fh.Header.Get("Content-Type"))
	return strings.Contains(contentType, "xml")
}

type hikvisionEvent struct {
	XMLName   xml.Name `xml:"EventNotificationAlert"`
	EventType string   `xml:"eventType"`
	DateTime  string   `xml:"dateTime"`
	ChannelID string   `xml:"channelID"`
	DeviceID  string   `xml:"deviceID"`
	ANPR      struct {
		LicensePlate    string  `xml:"licensePlate"`
		ConfidenceLevel float64 `xml:"confidenceLevel"`
		VehicleType     string  `xml:"vehicleType"`
		Color           string  `xml:"color"`
		Direction       string  `xml:"direction"`
		LaneNo          string  `xml:"laneNo"`
	} `xml:"ANPR"`
	PicInfo struct {
		StoragePath string `xml:"ftpPath"`
	} `xml:"picInfo"`
}

func (e *hikvisionEvent) ToEventPayload() anpr.EventPayload {
	eventTime := parseHikvisionTime(e.DateTime)
	lane := parseLane(e.ANPR.LaneNo)

	return anpr.EventPayload{
		CameraID:    firstNonEmpty(e.ChannelID, e.DeviceID),
		CameraModel: "",
		Plate:       strings.TrimSpace(e.ANPR.LicensePlate),
		Confidence:  e.ANPR.ConfidenceLevel,
		Direction:   e.ANPR.Direction,
		Lane:        lane,
		EventTime:   eventTime,
		Vehicle: anpr.VehicleInfo{
			Color: e.ANPR.Color,
			Type:  e.ANPR.VehicleType,
		},
		SnapshotURL: e.PicInfo.StoragePath,
		RawPayload: map[string]interface{}{
			"event_type": e.EventType,
		},
	}
}

func parseHikvisionTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}

	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts
		}
	}

	return time.Time{}
}

func parseLane(value string) int {
	if value == "" {
		return 0
	}
	lane, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return lane
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
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
