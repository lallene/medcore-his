package consultations

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/shared/pagination"
	"gorm.io/gorm"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func consultationAuthorID(c *gin.Context) (uint, bool) {
	userID, err := rbac.CurrentUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return 0, false
	}
	return userID, true
}

// GetReasons godoc
// @Summary Liste des motifs de consultation
// @Tags Consultations
// @Security BearerAuth
// @Produce json
// @Success 200 {array} ConsultationReason
// @Router /consultations/reasons [get]
func (h *Handler) GetReasons(c *gin.Context) {
	reasons, err := h.service.GetReasons()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, reasons)
}

// GetExams godoc
// @Summary Liste des examens médicaux
// @Tags Consultations
// @Security BearerAuth
// @Produce json
// @Success 200 {array} MedicalExam
// @Router /consultations/exams [get]
func (h *Handler) GetExams(c *gin.Context) {
	exams, err := h.service.GetExams()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, exams)
}

// CreateConsultation godoc
//
//	@Summary		Créer une consultation
//	@Description	Crée une consultation médicale avec motifs, constantes vitales, antécédents, examens physiques par organe, examens demandés, ordonnance structurée, traitements administrés sur place, repos maladie et hospitalisation.
//	@Tags			Consultations
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		CreateConsultationRequest	true	"Consultation à créer"
//	@Success		201		{object}	Consultation
//	@Failure		400		{object}	map[string]interface{}
//	@Failure		401		{object}	map[string]interface{}
//	@Failure		404		{object}	map[string]interface{}
//	@Failure		409		{object}	map[string]interface{}
//	@Failure		500		{object}	map[string]interface{}
//	@Router			/api/consultations [post]
func (h *Handler) CreateConsultation(c *gin.Context) {
	authorID, ok := consultationAuthorID(c)
	if !ok {
		return
	}
	var req CreateConsultationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	consultation, err := h.service.CreateConsultation(req, authorID)
	if err != nil {
		switch {
		case errors.Is(err, ErrPhysicalExamAreaNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})

		case errors.Is(err, ErrInactivePhysicalExamArea):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

		case errors.Is(err, ErrInvalidPresentationID):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

		case errors.Is(err, ErrInactivePresentation):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}

		return
	}

	c.JSON(http.StatusCreated, consultation)
}

func (h *Handler) ListConsultations(c *gin.Context) {
	paging := pagination.FromContext(c)
	filter := ConsultationListFilter{
		Page: paging.Page, Limit: paging.Limit,
		Status: c.Query("status"), Service: c.Query("service"), Search: c.Query("search"),
	}
	if rawPatientID := c.Query("patientId"); rawPatientID != "" {
		patientID, err := strconv.ParseUint(rawPatientID, 10, 64)
		if err != nil || patientID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant patient invalide"})
			return
		}
		value := uint(patientID)
		filter.PatientID = &value
	}
	result, err := h.service.ListConsultations(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": result.Data,
		"meta": gin.H{"page": result.Page, "limit": result.Limit, "total": result.Total, "totalPages": result.TotalPages},
	})
}

// GetConsultation godoc
// @Summary Détail d'une consultation médicale
// @Tags Consultations
// @Security BearerAuth
// @Produce json
// @Param id path int true "ID consultation"
// @Success 200 {object} Consultation
// @Router /consultations/{id} [get]
func (h *Handler) GetConsultation(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	consultation, err := h.service.GetConsultation(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "consultation introuvable"})
		return
	}

	c.JSON(http.StatusOK, consultation)
}

// GetPatientConsultations godoc
// @Summary Historique des consultations d'un patient
// @Description Retourne toutes les consultations médicales du patient avec motifs, constantes, examens et prescriptions.
// @Tags Patient 360
// @Security BearerAuth
// @Produce json
// @Param id path int true "ID du patient"
// @Success 200 {array} Consultation
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /patients/{id}/consultations [get]
func (h *Handler) GetPatientConsultations(c *gin.Context) {
	patientID, _ := strconv.Atoi(c.Param("id"))

	consultations, err := h.service.GetPatientConsultations(uint(patientID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, consultations)
}

// CreateReason godoc
// @Summary Créer un motif de consultation
// @Tags Consultations - Référentiels
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body CreateReferenceRequest true "Motif"
// @Success 201 {object} ConsultationReason
// @Router /consultations/reasons [post]
func (h *Handler) CreateReason(c *gin.Context) {
	var req CreateReferenceRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	reason, err := h.service.CreateReason(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, reason)
}

// UpdateReason godoc
// @Summary Modifier un motif de consultation
// @Tags Consultations - Référentiels
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "ID motif"
// @Param request body UpdateReferenceRequest true "Motif"
// @Success 200 {object} map[string]string
// @Router /consultations/reasons/{id} [put]
func (h *Handler) UpdateReason(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var req UpdateReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.service.UpdateReason(uint(id), req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Motif modifié avec succès"})
}

// DeleteReason godoc
// @Summary Désactiver un motif de consultation
// @Tags Consultations - Référentiels
// @Security BearerAuth
// @Produce json
// @Param id path int true "ID motif"
// @Success 200 {object} map[string]string
// @Router /consultations/reasons/{id} [delete]
func (h *Handler) DeleteReason(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	if err := h.service.DeleteReason(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Motif désactivé avec succès"})
}

// CreateExam godoc
// @Summary Créer un examen médical
// @Tags Consultations - Référentiels
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body CreateReferenceRequest true "Examen"
// @Success 201 {object} MedicalExam
// @Router /consultations/exams [post]
func (h *Handler) CreateExam(c *gin.Context) {
	var req CreateReferenceRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	exam, err := h.service.CreateExam(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, exam)
}

// UpdateExam godoc
// @Summary Modifier un examen médical
// @Description Modifie les informations d'un examen médical du référentiel. Permission requise : consultations.references.manage.
// @Tags Consultations - Référentiels
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "ID de l'examen"
// @Param request body UpdateReferenceRequest true "Données de l'examen"
// @Success 200 {object} MedicalExam
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /consultations/exams/{id} [put]
func (h *Handler) UpdateExam(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var req UpdateReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.service.UpdateExam(uint(id), req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Examen modifié avec succès"})
}

// DeleteExam godoc
// @Summary Désactiver un examen médical
// @Description Désactive un examen médical du référentiel. Permission requise : consultations.references.manage.
// @Tags Consultations - Référentiels
// @Security BearerAuth
// @Produce json
// @Param id path int true "ID de l'examen"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /consultations/exams/{id} [delete]
func (h *Handler) DeleteExam(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	if err := h.service.DeleteExam(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Examen désactivé avec succès"})
}

// UpdateStatus godoc
// @Summary Changer le statut d'une consultation
// @Tags Consultations
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "ID consultation"
// @Param request body UpdateConsultationStatusRequest true "Statut"
// @Success 200 {object} Consultation
// @Router /consultations/{id}/status [patch]
func (h *Handler) UpdateStatus(c *gin.Context) {
	authorID, ok := consultationAuthorID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant de consultation invalide",
		})
		return
	}

	var req UpdateConsultationStatusRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	consultation, err := h.service.UpdateStatus(
		uint(id),
		req,
		authorID,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrConsultationNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"error": err.Error(),
			})

		case errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusConflict, gin.H{
				"error": err.Error(),
			})
		case errors.Is(err, ErrCancellationReasonRequired):
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})

		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
		}
		return
	}

	c.JSON(http.StatusOK, consultation)
}

// UpdateConsultation godoc
//
//	@Summary		Modifier une consultation
//	@Description	Met à jour une consultation non terminée. Les antécédents peuvent être remplacés. Les examens physiques et traitements administrés sont remplacés uniquement lorsqu'ils sont présents dans la requête ; un tableau vide supprime les éléments existants.
//	@Tags			Consultations
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		int							true	"ID de la consultation"
//	@Param			request	body		UpdateConsultationRequest	true	"Données à modifier"
//	@Success		200		{object}	Consultation
//	@Failure		400		{object}	map[string]interface{}
//	@Failure		401		{object}	map[string]interface{}
//	@Failure		404		{object}	map[string]interface{}
//	@Failure		409		{object}	map[string]interface{}
//	@Failure		500		{object}	map[string]interface{}
//	@Router			/api/consultations/{id} [put]
func (h *Handler) UpdateConsultation(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant de consultation invalide",
		})
		return
	}

	var req UpdateConsultationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	consultation, err := h.service.UpdateConsultation(
		uint(id),
		req,
	)

	if err != nil {
		switch {

		case errors.Is(err, ErrConsultationNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"error": err.Error(),
			})

		case errors.Is(err, ErrConsultationLocked):
			c.JSON(http.StatusConflict, gin.H{
				"error": err.Error(),
			})

		case errors.Is(err, ErrInvalidSickLeave),
			errors.Is(err, ErrInvalidReasonIDs),
			errors.Is(err, ErrInvalidExamIDs):

			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
		case errors.Is(err, ErrInvalidPresentationID):
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return

		case errors.Is(err, ErrInactivePresentation):
			c.JSON(http.StatusConflict, gin.H{
				"error": err.Error(),
			})
			return

		case errors.Is(err, ErrPhysicalExamAreaNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return

		case errors.Is(err, ErrInactivePhysicalExamArea):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return

		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
		}

		return
	}

	c.JSON(http.StatusOK, consultation)
}

// GenerateSickLeavePDF godoc
// @Summary Générer la fiche de repos maladie
// @Description Génère la fiche PDF de repos maladie prescrite lors d'une consultation.
// @Tags Documents médicaux
// @Security BearerAuth
// @Produce application/pdf
// @Param id path int true "ID de la consultation"
// @Success 200 {file} file
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /consultations/{id}/sick-leave/pdf [get]
func (h *Handler) GenerateSickLeavePDF(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	consultation, err := h.service.GetConsultation(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "consultation introuvable"})
		return
	}

	content, err := GenerateSickLeavePDF(consultation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("repos-maladie-consultation-%d.pdf", consultation.ID)

	c.Header("Content-Disposition", "inline; filename="+filename)
	c.Data(http.StatusOK, "application/pdf", content)
}

// GenerateExamRequestPDF godoc
// @Summary Générer la demande d'examens
// @Tags Documents médicaux
// @Security BearerAuth
// @Produce application/pdf
// @Param id path int true "ID consultation"
// @Success 200 {file} file
// @Router /consultations/{id}/exam-request/pdf [get]
func (h *Handler) GenerateExamRequestPDF(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	consultation, err := h.service.GetConsultation(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "consultation introuvable"})
		return
	}

	content, err := GenerateExamRequestPDF(consultation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("demande-examens-consultation-%d.pdf", consultation.ID)

	c.Header("Content-Disposition", "inline; filename="+filename)
	c.Data(http.StatusOK, "application/pdf", content)
}

// GeneratePrescriptionPDF godoc
// @Summary Générer l'ordonnance
// @Tags Documents médicaux
// @Security BearerAuth
// @Produce application/pdf
// @Param id path int true "ID consultation"
// @Success 200 {file} file
// @Router /consultations/{id}/prescription/pdf [get]
func (h *Handler) GeneratePrescriptionPDF(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant de consultation invalide",
		})
		return
	}

	consultation, err := h.service.GetConsultation(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "consultation introuvable",
		})
		return
	}

	content, err := GeneratePrescriptionPDF(consultation)
	if err != nil {
		logger.Error(
			"Erreur génération ordonnance PDF",
			"consultation_id", consultation.ID,
			"error", err,
		)

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	filename := fmt.Sprintf(
		"ordonnance-consultation-%d.pdf",
		consultation.ID,
	)

	c.Header(
		"Content-Disposition",
		"inline; filename="+filename,
	)

	c.Data(
		http.StatusOK,
		"application/pdf",
		content,
	)
}

// GenerateConsultationReportPDF godoc
// @Summary Générer le compte rendu de consultation
// @Tags Documents médicaux
// @Security BearerAuth
// @Produce application/pdf
// @Param id path int true "ID consultation"
// @Success 200 {file} file
// @Router /consultations/{id}/report/pdf [get]
func (h *Handler) GenerateConsultationReportPDF(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant de consultation invalide",
		})
		return
	}

	consultation, err := h.service.GetConsultation(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "consultation introuvable",
		})
		return
	}

	content, err := GenerateConsultationReportPDF(consultation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	filename := fmt.Sprintf(
		"compte-rendu-consultation-%d.pdf",
		consultation.ID,
	)

	c.Header(
		"Content-Disposition",
		"inline; filename="+filename,
	)

	c.Data(
		http.StatusOK,
		"application/pdf",
		content,
	)
}

// GenerateHospitalizationPDF godoc
// @Summary Générer la fiche d'hospitalisation
// @Tags Documents médicaux
// @Security BearerAuth
// @Produce application/pdf
// @Param id path int true "ID consultation"
// @Success 200 {file} file
// @Router /consultations/{id}/hospitalization/pdf [get]
func (h *Handler) GenerateHospitalizationPDF(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant de consultation invalide"})
		return
	}

	consultation, err := h.service.GetConsultation(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "consultation introuvable"})
		return
	}

	content, err := GenerateHospitalizationPDF(consultation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("fiche-hospitalisation-consultation-%d.pdf", consultation.ID)

	c.Header("Content-Disposition", "inline; filename="+filename)
	c.Data(http.StatusOK, "application/pdf", content)
}

// GetPatient360 godoc
// @Summary Vue Patient 360
// @Tags Patient 360
// @Security BearerAuth
// @Produce json
// @Param id path int true "ID patient"
// @Success 200 {object} Patient360Response
// @Router /patients/{id}/360 [get]
func (h *Handler) GetPatient360(c *gin.Context) {
	patientID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant patient invalide",
		})
		return
	}

	result, err := h.service.GetPatient360(uint(patientID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetPhysicalExamAreas godoc
//
//	@Summary		Liste des zones d'examen physique
//	@Description	Retourne le référentiel des systèmes, appareils, organes et zones utilisables dans l'examen physique.
//	@Tags			Consultations - Référentiels
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		PhysicalExamArea
//	@Failure		500	{object}	map[string]interface{}
//	@Router			/api/consultations/physical-exam-areas [get]
func (h *Handler) GetPhysicalExamAreas(c *gin.Context) {
	areas, err := h.service.GetPhysicalExamAreas()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, areas)
}

func (h *Handler) GetSOAP(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant invalide"})
		return
	}

	soap, err := h.service.GetSOAP(uint(id))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "SOAP non renseigné"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, soap)
}

func (h *Handler) UpsertSOAP(c *gin.Context) {
	authorID, ok := consultationAuthorID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant invalide"})
		return
	}

	var req UpsertConsultationSOAPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	soap, err := h.service.UpsertSOAP(uint(id), req, authorID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, soap)
}

func (h *Handler) GetSpecialtyData(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant de consultation invalide",
		})
		return
	}

	data, err := h.service.GetSpecialtyData(uint(id))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "données de spécialité non renseignées",
		})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	var parsedData map[string]any

	if data.Data != "" {
		_ = json.Unmarshal([]byte(data.Data), &parsedData)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":              data.ID,
		"consultation_id": data.ConsultationID,
		"specialty_code":  data.SpecialtyCode,
		"data":            parsedData,
		"created_by":      data.CreatedBy,
		"updated_by":      data.UpdatedBy,
		"created_at":      data.CreatedAt,
		"updated_at":      data.UpdatedAt,
	})
}

func (h *Handler) UpsertSpecialtyData(c *gin.Context) {
	authorID, ok := consultationAuthorID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant de consultation invalide",
		})
		return
	}

	var req UpsertConsultationSpecialtyRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	data, err := h.service.UpsertSpecialtyData(uint(id), req, authorID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	var parsedData map[string]any

	if data.Data != "" {
		_ = json.Unmarshal([]byte(data.Data), &parsedData)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":              data.ID,
		"consultation_id": data.ConsultationID,
		"specialty_code":  data.SpecialtyCode,
		"data":            parsedData,
		"created_by":      data.CreatedBy,
		"updated_by":      data.UpdatedBy,
		"created_at":      data.CreatedAt,
		"updated_at":      data.UpdatedAt,
	})
}
