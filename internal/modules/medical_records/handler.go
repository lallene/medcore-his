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

// GetOrCreateByPatientID godoc
// @Summary      Récupérer ou créer le dossier médical d'un patient
// @Description  Retourne le dossier médical du patient. S'il n'existe pas encore, il est créé automatiquement.
// @Tags         Medical Records
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "ID du patient"
// @Success      200  {object}  MedicalRecord
// @Failure      400  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /patients/{id}/medical-record [get]
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

// GetOverview godoc
// @Summary      Obtenir la vue d'ensemble d'un dossier médical
// @Description  Retourne les informations générales du dossier, les alertes, allergies, antécédents et dernières constantes.
// @Tags         Medical Records
// @Produce      json
// @Security     BearerAuth
// @Param        recordId   path      int  true  "ID du dossier médical"
// @Success      200        {object}  MedicalRecordOverviewResponse
// @Failure      400        {object}  map[string]interface{}
// @Failure      401        {object}  map[string]interface{}
// @Failure      404        {object}  map[string]interface{}
// @Router       /medical-records/{recordId}/overview [get]
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

// AddAlert godoc
// @Summary      Ajouter une alerte médicale
// @Description  Ajoute une alerte active au dossier médical du patient.
// @Tags         Medical Records
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        recordId  path      int                 true  "ID du dossier médical"
// @Param        request   body      CreateAlertRequest  true  "Alerte médicale"
// @Success      201       {object}  MedicalAlert
// @Failure      400       {object}  map[string]interface{}
// @Failure      401       {object}  map[string]interface{}
// @Failure      500       {object}  map[string]interface{}
// @Router       /medical-records/{recordId}/alerts [post]
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

// AddAllergy godoc
// @Summary      Ajouter une allergie
// @Description  Ajoute une allergie au dossier médical et crée automatiquement un événement dans la chronologie.
// @Tags         Medical Records
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        recordId  path      int                   true  "ID du dossier médical"
// @Param        request   body      CreateAllergyRequest  true  "Informations sur l'allergie"
// @Success      201       {object}  Allergy
// @Failure      400       {object}  map[string]interface{}
// @Failure      401       {object}  map[string]interface{}
// @Failure      500       {object}  map[string]interface{}
// @Router       /medical-records/{recordId}/allergies [post]
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

// AddMedicalHistory godoc
// @Summary      Ajouter un antécédent médical
// @Description  Ajoute un antécédent au dossier médical et crée automatiquement un événement dans la chronologie.
// @Tags         Medical Records
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        recordId  path      int                          true  "ID du dossier médical"
// @Param        request   body      CreateMedicalHistoryRequest  true  "Antécédent médical"
// @Success      201       {object}  MedicalHistory
// @Failure      400       {object}  map[string]interface{}
// @Failure      401       {object}  map[string]interface{}
// @Failure      500       {object}  map[string]interface{}
// @Router       /medical-records/{recordId}/histories [post]
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

// AddVitalSign godoc
// @Summary      Enregistrer les constantes vitales
// @Description  Enregistre les constantes du patient, calcule automatiquement l'IMC et ajoute un événement dans la chronologie.
// @Tags         Medical Records
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        recordId  path      int                     true  "ID du dossier médical"
// @Param        request   body      CreateVitalSignRequest  true  "Constantes vitales"
// @Success      201       {object}  VitalSign
// @Failure      400       {object}  map[string]interface{}
// @Failure      401       {object}  map[string]interface{}
// @Failure      500       {object}  map[string]interface{}
// @Router       /medical-records/{recordId}/vital-signs [post]
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

// ListVitalSigns godoc
// @Summary      Lister les constantes vitales
// @Description  Retourne l'historique des constantes vitales du patient, de la plus récente à la plus ancienne.
// @Tags         Medical Records
// @Produce      json
// @Security     BearerAuth
// @Param        recordId  path      int  true  "ID du dossier médical"
// @Success      200       {array}   VitalSign
// @Failure      400       {object}  map[string]interface{}
// @Failure      401       {object}  map[string]interface{}
// @Failure      500       {object}  map[string]interface{}
// @Router       /medical-records/{recordId}/vital-signs [get]
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

// ListTimelineEvents godoc
// @Summary      Obtenir la chronologie médicale
// @Description  Retourne tous les événements médicaux du patient dans l'ordre chronologique inverse.
// @Tags         Medical Records
// @Produce      json
// @Security     BearerAuth
// @Param        recordId  path      int  true  "ID du dossier médical"
// @Success      200       {array}   MedicalTimelineEvent
// @Failure      400       {object}  map[string]interface{}
// @Failure      401       {object}  map[string]interface{}
// @Failure      500       {object}  map[string]interface{}
// @Router       /medical-records/{recordId}/timeline [get]
func (h *Handler) ListTimelineEvents(c *gin.Context) {
	recordID, err := strconv.ParseUint(c.Param("recordId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "recordId invalide",
		})
		return
	}

	events, err := h.service.ListTimelineEvents(uint(recordID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, events)
}

// GetPatientMedicalSummary godoc
// @Summary      Obtenir le résumé médical complet d'un patient
// @Description  Retourne le dossier médical, les alertes, les allergies, les antécédents, les dernières constantes et la chronologie récente.
// @Tags         Medical Records
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "ID du patient"
// @Success      200  {object}  PatientMedicalSummaryResponse
// @Failure      400  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /patients/{id}/medical-summary [get]
func (h *Handler) GetPatientMedicalSummary(c *gin.Context) {
	patientID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id patient invalide"})
		return
	}

	summary, err := h.service.GetPatientMedicalSummary(uint(patientID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, summary)
}

func (h *Handler) GetCommonMedicalRecord(c *gin.Context) {
	patientID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant patient invalide",
		})
		return
	}

	record, err := h.service.GetCommonMedicalRecord(uint(patientID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, record)
}

func (h *Handler) UpdateCommonMedicalRecord(c *gin.Context) {
	patientID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant patient invalide",
		})
		return
	}

	var req UpdateCommonMedicalRecordRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	record, err := h.service.UpdateCommonMedicalRecord(
		uint(patientID),
		req,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, record)
}
