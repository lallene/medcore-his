package medical_records

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetOrCreateByPatientID(c *gin.Context) {
	patientID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id patient invalide"})
		return
	}

	record, err := h.service.GetOrCreateMedicalRecord(uint(patientID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, record)
}

func (h *Handler) GetOverview(c *gin.Context) {
	recordID, err := strconv.ParseUint(c.Param("recordId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recordId invalide"})
		return
	}

	overview, err := h.service.GetOverview(uint(recordID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dossier médical introuvable"})
		return
	}

	c.JSON(http.StatusOK, overview)
}

func (h *Handler) AddAlert(c *gin.Context) {
	recordID, err := strconv.ParseUint(c.Param("recordId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recordId invalide"})
		return
	}

	var req CreateAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	alert, err := h.service.AddAlert(uint(recordID), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, alert)
}

func (h *Handler) AddAllergy(c *gin.Context) {
	recordID, err := strconv.ParseUint(c.Param("recordId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recordId invalide"})
		return
	}

	var req CreateAllergyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	allergy, err := h.service.AddAllergy(uint(recordID), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, allergy)
}

func (h *Handler) AddMedicalHistory(c *gin.Context) {
	recordID, err := strconv.ParseUint(c.Param("recordId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recordId invalide"})
		return
	}

	var req CreateMedicalHistoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	history, err := h.service.AddMedicalHistory(uint(recordID), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, history)
}

func (h *Handler) AddVitalSign(c *gin.Context) {
	recordID, err := strconv.ParseUint(c.Param("recordId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recordId invalide"})
		return
	}

	var req CreateVitalSignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	vital, err := h.service.AddVitalSign(uint(recordID), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, vital)
}

func (h *Handler) ListVitalSigns(c *gin.Context) {
	recordID, err := strconv.ParseUint(c.Param("recordId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recordId invalide"})
		return
	}

	vitals, err := h.service.ListVitalSigns(uint(recordID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, vitals)
}
