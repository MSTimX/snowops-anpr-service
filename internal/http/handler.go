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
		public.GET("/anpr/hikvision", h.checkHikvisionEndpoint) // Для проверки доступности камерой
		public.GET("/plates", h.listPlates)
		public.GET("/events", h.listEvents)
		public.GET("/camera/status", h.checkCameraStatus)
	}

	// Protected endpoints
	protected := r.Group("/api/v1")
	protected.Use(authMiddleware)
	{
		protected.POST("/anpr/sync-vehicle", h.syncVehicleToWhitelist)
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
	h.log.Info().
		Str("method", c.Request.Method).
		Str("path", c.Request.URL.Path).
		Str("remote_addr", c.ClientIP()).
		Str("user_agent", c.Request.UserAgent()).
		Str("content_type", c.Request.Header.Get("Content-Type")).
		Msg("received Hikvision event request")

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

	h.log.Debug().
		Int("xml_size", len(xmlPayload)).
		Str("xml_preview", string(xmlPayload[:min(200, len(xmlPayload))])).
		Msg("extracted XML payload")

	hikEvent := &hikvisionEvent{}
	if err := xml.Unmarshal(xmlPayload, hikEvent); err != nil {
		h.log.Error().
			Err(err).
			Str("xml_content", string(xmlPayload)).
			Msg("failed to parse hikvision xml")
		c.JSON(http.StatusBadRequest, errorResponse("invalid xml payload"))
		return
	}

	h.log.Info().
		Str("event_type", hikEvent.EventType).
		Str("license_plate", hikEvent.ANPR.LicensePlate).
		Str("device_id", hikEvent.DeviceID).
		Str("channel_id", hikEvent.ChannelID).
		Str("date_time", hikEvent.DateTime).
		Msg("parsed Hikvision event")

	payload := hikEvent.ToEventPayload(xmlPayload)

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
			h.log.Warn().
				Err(err).
				Str("plate", payload.Plate).
				Str("camera_id", payload.CameraID).
				Msg("invalid input for Hikvision event")
			c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
			return
		}
		h.log.Error().
			Err(err).
			Str("plate", payload.Plate).
			Str("camera_id", payload.CameraID).
			Msg("failed to process hikvision event")
		c.JSON(http.StatusInternalServerError, errorResponse("internal error"))
		return
	}

	h.log.Info().
		Str("event_id", result.EventID.String()).
		Str("plate_id", result.PlateID.String()).
		Str("plate", result.Plate).
		Int("hits_count", len(result.Hits)).
		Msg("successfully processed and saved Hikvision event")

	c.JSON(http.StatusCreated, gin.H{
		"status":    "ok",
		"event_id":  result.EventID,
		"plate_id":  result.PlateID,
		"plate":     result.Plate,
		"hits":      result.Hits,
		"processed": true,
	})
}

// checkHikvisionEndpoint обрабатывает GET запросы от камеры для проверки доступности эндпоинта
func (h *Handler) checkHikvisionEndpoint(c *gin.Context) {
	h.log.Info().
		Str("method", c.Request.Method).
		Str("path", c.Request.URL.Path).
		Str("remote_addr", c.ClientIP()).
		Str("user_agent", c.Request.UserAgent()).
		Msg("received Hikvision endpoint check request")

	// Возвращаем 200 OK, чтобы камера знала, что эндпоинт доступен
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Hikvision ANPR endpoint is available",
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	XMLName          xml.Name `xml:"EventNotificationAlert"`
	EventType        string   `xml:"eventType" json:"event_type"`
	EventDescription string   `xml:"eventDescription" json:"event_description"`
	DateTime         string   `xml:"dateTime" json:"date_time"`
	ChannelID        string   `xml:"channelID" json:"channel_id"`
	DeviceID         string   `xml:"deviceID" json:"device_id"`
	DeviceName       string   `xml:"deviceName" json:"device_name"`
	IPAddress        string   `xml:"ipAddress" json:"ip_address"`
	PortNo           string   `xml:"portNo" json:"port_no"`
	ProtocolType     string   `xml:"protocolType" json:"protocol_type"`
	ANPR             struct {
		LicensePlate    string  `xml:"licensePlate" json:"license_plate"`
		ConfidenceLevel float64 `xml:"confidenceLevel" json:"confidence_level"`
		VehicleType     string  `xml:"vehicleType" json:"vehicle_type"`
		VehicleColor    string  `xml:"vehicleColor" json:"vehicle_color"`
		Color           string  `xml:"color" json:"color"`
		PlateColor      string  `xml:"plateColor" json:"plate_color"`
		Country         string  `xml:"country" json:"country"`
		Brand           string  `xml:"brand" json:"brand"`
		Direction       string  `xml:"direction" json:"direction"`
		LaneNo          string  `xml:"laneNo" json:"lane_no"`
		Speed           string  `xml:"speed" json:"speed"`
	} `xml:"ANPR" json:"anpr"`
	VehicleInfo struct {
		Type       string `xml:"vehicleType" json:"vehicle_type"`
		Color      string `xml:"vehicleColor" json:"vehicle_color"`
		Brand      string `xml:"brand" json:"brand"`
		Model      string `xml:"vehicleModel" json:"vehicle_model"`
		PlateColor string `xml:"plateColor" json:"plate_color"`
		Country    string `xml:"country" json:"country"`
		Speed      string `xml:"speed" json:"speed"`
	} `xml:"vehicleInfo" json:"vehicle_info"`
	PicInfo struct {
		StoragePath string   `xml:"ftpPath" json:"ftp_path"`
		FilePath    string   `xml:"filePath" json:"file_path"`
		FilePaths   []string `xml:"filePathList>filePath" json:"file_path_list"`
	} `xml:"picInfo" json:"pic_info"`
}

func (e *hikvisionEvent) ToEventPayload(rawXML []byte) anpr.EventPayload {
	eventTime := parseHikvisionTime(e.DateTime)
	lane := parseLane(e.ANPR.LaneNo)

	vehicleColor := firstNonEmpty(e.ANPR.VehicleColor, e.ANPR.Color, e.VehicleInfo.Color)
	vehicleType := firstNonEmpty(e.ANPR.VehicleType, e.VehicleInfo.Type)
	vehiclePlateColor := firstNonEmpty(e.ANPR.PlateColor, e.VehicleInfo.PlateColor)
	vehicleCountry := firstNonEmpty(e.ANPR.Country, e.VehicleInfo.Country)
	vehicleBrand := firstNonEmpty(e.VehicleInfo.Brand, e.ANPR.Brand)
	vehicleModel := e.VehicleInfo.Model
	speedPtr := parseOptionalFloat(firstNonEmpty(e.VehicleInfo.Speed, e.ANPR.Speed))

	cameraModel := firstNonEmpty(e.DeviceName, e.DeviceID)
	snapshotURL := firstNonEmpty(e.PicInfo.StoragePath, e.PicInfo.FilePath)
	if snapshotURL == "" && len(e.PicInfo.FilePaths) > 0 {
		snapshotURL = e.PicInfo.FilePaths[0]
	}

	rawPayload := map[string]interface{}{
		"event_type":        e.EventType,
		"event_description": e.EventDescription,
		"device_id":         e.DeviceID,
		"device_name":       e.DeviceName,
		"channel_id":        e.ChannelID,
		"ip_address":        e.IPAddress,
		"port_no":           e.PortNo,
		"protocol_type":     e.ProtocolType,
		"anpr":              e.ANPR,
		"vehicle_info":      e.VehicleInfo,
	}
	if len(rawXML) > 0 {
		rawPayload["xml"] = string(rawXML)
	}

	return anpr.EventPayload{
		CameraID:    firstNonEmpty(e.ChannelID, e.DeviceID),
		CameraModel: cameraModel,
		Plate:       strings.TrimSpace(e.ANPR.LicensePlate),
		Confidence:  e.ANPR.ConfidenceLevel,
		Direction:   e.ANPR.Direction,
		Lane:        lane,
		EventTime:   eventTime,
		Vehicle: anpr.VehicleInfo{
			Color:      vehicleColor,
			Type:       vehicleType,
			Brand:      vehicleBrand,
			Model:      vehicleModel,
			Country:    vehicleCountry,
			PlateColor: vehiclePlateColor,
			Speed:      speedPtr,
		},
		SnapshotURL: snapshotURL,
		RawPayload:  rawPayload,
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

func parseOptionalFloat(value string) *float64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	if f, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
		return &f
	}
	return nil
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

func (h *Handler) syncVehicleToWhitelist(c *gin.Context) {
	var req struct {
		PlateNumber string `json:"plate_number" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
		return
	}

	plateID, err := h.anprService.SyncVehicleToWhitelist(c.Request.Context(), req.PlateNumber)
	if err != nil {
		h.log.Error().Err(err).Str("plate_number", req.PlateNumber).Msg("failed to sync vehicle to whitelist")
		c.JSON(http.StatusInternalServerError, errorResponse("failed to sync vehicle to whitelist"))
		return
	}

	h.log.Info().
		Str("plate_number", req.PlateNumber).
		Str("plate_id", plateID.String()).
		Msg("vehicle synced to whitelist")

	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"plate_id":     plateID.String(),
		"plate_number": req.PlateNumber,
		"message":      "vehicle added to whitelist",
	})
}

func errorResponse(message string) gin.H {
	return gin.H{
		"error": message,
	}
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func (h *Handler) checkCameraStatus(c *gin.Context) {
	httpHost := h.config.Camera.HTTPHost
	rtspURL := h.config.Camera.RTSPURL
	cameraModel := h.config.Camera.Model

	status := gin.H{
		"camera_model": cameraModel,
		"http_host":    httpHost,
		"rtsp_url":     maskPassword(rtspURL),
		"configured":   httpHost != "" && rtspURL != "",
	}

	// Проверяем доступность HTTP интерфейса камеры
	if httpHost != "" {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(httpHost)
		if err != nil {
			status["http_accessible"] = false
			status["http_error"] = err.Error()
		} else {
			resp.Body.Close()
			status["http_accessible"] = resp.StatusCode < 500
			status["http_status"] = resp.StatusCode
		}
	} else {
		status["http_accessible"] = false
		status["http_error"] = "HTTP host not configured"
	}

	// RTSP URL проверяем только на наличие (для проверки подключения нужен специальный клиент)
	status["rtsp_configured"] = rtspURL != ""

	h.log.Info().
		Str("http_host", httpHost).
		Bool("http_accessible", status["http_accessible"].(bool)).
		Msg("camera status checked")

	c.JSON(http.StatusOK, gin.H{
		"status": status,
	})
}

func maskPassword(url string) string {
	// Маскируем пароль в URL для безопасности
	if strings.Contains(url, "@") {
		parts := strings.Split(url, "@")
		if len(parts) == 2 {
			authPart := parts[0]
			if strings.Contains(authPart, "://") {
				protocol := strings.Split(authPart, "://")[0]
				credentials := strings.Split(authPart, "://")[1]
				if strings.Contains(credentials, ":") {
					username := strings.Split(credentials, ":")[0]
					return protocol + "://" + username + ":****@" + parts[1]
				}
			}
		}
	}
	return url
}
