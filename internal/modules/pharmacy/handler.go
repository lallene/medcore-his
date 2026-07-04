package pharmacy

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetFamilies godoc
//
//	@Summary	Liste des familles thérapeutiques
//	@Tags		Pharmacy - Référentiels
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	MedicationFamily
//	@Router		/pharmacy/families [get]
func (h *Handler) GetFamilies(c *gin.Context) {
	families, err := h.service.GetFamilies()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, families)
}

// CreateFamily godoc
//
//	@Summary	Créer une famille thérapeutique
//	@Tags		Pharmacy - Référentiels
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		request	body		CreateMedicationFamilyRequest	true	"Famille thérapeutique"
//	@Success	201		{object}	MedicationFamily
//	@Router		/pharmacy/families [post]
func (h *Handler) CreateFamily(c *gin.Context) {
	var req CreateMedicationFamilyRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	family, err := h.service.CreateFamily(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, family)
}

// UpdateFamily godoc
//
//	@Summary	Modifier une famille thérapeutique
//	@Tags		Pharmacy - Référentiels
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int								true	"ID famille"
//	@Param		request	body		UpdateMedicationFamilyRequest	true	"Famille thérapeutique"
//	@Success	200		{object}	MedicationFamily
//	@Router		/pharmacy/families/{id} [put]
func (h *Handler) UpdateFamily(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant invalide",
		})
		return
	}

	var req UpdateMedicationFamilyRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	family, err := h.service.UpdateFamily(uint(id), req)
	if err != nil {
		if errors.Is(err, ErrFamilyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, family)
}

// DeleteFamily godoc
//
//	@Summary	Désactiver une famille thérapeutique
//	@Tags		Pharmacy - Référentiels
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"ID famille"
//	@Success	200	{object}	map[string]string
//	@Router		/pharmacy/families/{id} [delete]
func (h *Handler) DeleteFamily(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant invalide",
		})
		return
	}

	if err := h.service.DeleteFamily(uint(id)); err != nil {
		if errors.Is(err, ErrFamilyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "famille thérapeutique désactivée",
	})
}

// GetMedications godoc
//
//	@Summary	Liste des médicaments
//	@Tags		Pharmacy - Médicaments
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	Medication
//	@Router		/pharmacy/medications [get]
func (h *Handler) GetMedications(c *gin.Context) {
	medications, err := h.service.GetMedications()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, medications)
}

// CreateMedication godoc
//
//	@Summary	Créer un médicament
//	@Tags		Pharmacy - Médicaments
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		request	body		CreateMedicationRequest	true	"Médicament"
//	@Success	201		{object}	Medication
//	@Router		/pharmacy/medications [post]
func (h *Handler) CreateMedication(c *gin.Context) {
	var req CreateMedicationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	medication, err := h.service.CreateMedication(req)
	if err != nil {
		if errors.Is(err, ErrFamilyNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, medication)
}

// UpdateMedication godoc
//
//	@Summary	Modifier un médicament
//	@Tags		Pharmacy - Médicaments
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"ID médicament"
//	@Param		request	body		UpdateMedicationRequest	true	"Médicament"
//	@Success	200		{object}	Medication
//	@Router		/pharmacy/medications/{id} [put]
func (h *Handler) UpdateMedication(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant invalide"})
		return
	}

	var req UpdateMedicationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	medication, err := h.service.UpdateMedication(uint(id), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrMedicationNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, ErrFamilyNotFound):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, medication)
}

// DeleteMedication godoc
//
//	@Summary	Désactiver un médicament
//	@Tags		Pharmacy - Médicaments
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"ID médicament"
//	@Success	200	{object}	map[string]string
//	@Router		/pharmacy/medications/{id} [delete]
func (h *Handler) DeleteMedication(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant invalide"})
		return
	}

	if err := h.service.DeleteMedication(uint(id)); err != nil {
		if errors.Is(err, ErrMedicationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "médicament désactivé"})
}

// GetPresentations godoc
//
//	@Summary	Liste des présentations pharmaceutiques
//	@Tags		Pharmacy - Présentations
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	MedicationPresentation
//	@Router		/pharmacy/presentations [get]
func (h *Handler) GetPresentations(c *gin.Context) {
	presentations, err := h.service.GetPresentations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, presentations)
}

// CreatePresentation godoc
//
//	@Summary	Créer une présentation pharmaceutique
//	@Tags		Pharmacy - Présentations
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		request	body		CreateMedicationPresentationRequest	true	"Présentation pharmaceutique"
//	@Success	201		{object}	MedicationPresentation
//	@Router		/pharmacy/presentations [post]
func (h *Handler) CreatePresentation(c *gin.Context) {
	var req CreateMedicationPresentationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	presentation, err := h.service.CreatePresentation(req)
	if err != nil {
		if errors.Is(err, ErrMedicationNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, presentation)
}

// UpdatePresentation godoc
//
//	@Summary	Modifier une présentation pharmaceutique
//	@Tags		Pharmacy - Présentations
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int									true	"ID présentation"
//	@Param		request	body		UpdateMedicationPresentationRequest	true	"Présentation pharmaceutique"
//	@Success	200		{object}	MedicationPresentation
//	@Router		/pharmacy/presentations/{id} [put]
func (h *Handler) UpdatePresentation(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant invalide"})
		return
	}

	var req UpdateMedicationPresentationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	presentation, err := h.service.UpdatePresentation(uint(id), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrPresentationNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, ErrMedicationNotFound):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, presentation)
}

// DeletePresentation godoc
//
//	@Summary	Désactiver une présentation pharmaceutique
//	@Tags		Pharmacy - Présentations
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"ID présentation"
//	@Success	200	{object}	map[string]string
//	@Router		/pharmacy/presentations/{id} [delete]
func (h *Handler) DeletePresentation(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant invalide"})
		return
	}

	if err := h.service.DeletePresentation(uint(id)); err != nil {
		if errors.Is(err, ErrPresentationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "présentation pharmaceutique désactivée"})
}

// GetStocks godoc
//
//	@Summary	Liste des stocks pharmacie
//	@Tags		Pharmacy - Stock
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	PharmacyStockResponse
//	@Router		/pharmacy/stocks [get]
func (h *Handler) GetStocks(c *gin.Context) {
	stocks, err := h.service.GetStocks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stocks)
}

// CreateStock godoc
//
//	@Summary	Créer un stock pour une présentation
//	@Tags		Pharmacy - Stock
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		request	body		CreatePharmacyStockRequest	true	"Stock"
//	@Success	201		{object}	PharmacyStockResponse
//	@Router		/pharmacy/stocks [post]
func (h *Handler) CreateStock(c *gin.Context) {
	var req CreatePharmacyStockRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stock, err := h.service.CreateStock(req)
	if err != nil {
		switch {
		case errors.Is(err, ErrPresentationNotFound):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrStockAlreadyExists):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusCreated, stock)
}

// UpdateStock godoc
//
//	@Summary	Modifier un stock pharmacie
//	@Tags		Pharmacy - Stock
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int							true	"ID stock"
//	@Param		request	body		UpdatePharmacyStockRequest	true	"Stock"
//	@Success	200		{object}	PharmacyStockResponse
//	@Router		/pharmacy/stocks/{id} [put]
func (h *Handler) UpdateStock(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant invalide"})
		return
	}

	var req UpdatePharmacyStockRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stock, err := h.service.UpdateStock(uint(id), req)
	if err != nil {
		if errors.Is(err, ErrStockNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stock)
}

// GetBatches godoc
//
//	@Summary	Liste des lots pharmacie
//	@Tags		Pharmacy - Lots
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	PharmacyBatchResponse
//	@Router		/pharmacy/batches [get]
func (h *Handler) GetBatches(c *gin.Context) {
	batches, err := h.service.GetBatches()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, batches)
}

// CreateBatch godoc
//
//	@Summary		Créer un lot pharmacie
//	@Description	La création d'un lot augmente le stock et crée un mouvement BATCH_ENTRY.
//	@Tags			Pharmacy - Lots
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreatePharmacyBatchRequest	true	"Lot pharmacie"
//	@Success		201		{object}	PharmacyBatchResponse
//	@Router			/pharmacy/batches [post]
func (h *Handler) CreateBatch(c *gin.Context) {
	var req CreatePharmacyBatchRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	batch, err := h.service.CreateBatch(req)
	if err != nil {
		switch {
		case errors.Is(err, ErrPresentationNotFound):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrInvalidExpirationDate):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusCreated, batch)
}

// GetStockMovements godoc
//
//	@Summary	Historique des mouvements de stock
//	@Tags		Pharmacy - Mouvements
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	StockMovement
//	@Router		/pharmacy/stock-movements [get]
func (h *Handler) GetStockMovements(c *gin.Context) {
	movements, err := h.service.GetStockMovements()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, movements)
}

// GetPresentationStockMovements godoc
//
//	@Summary	Historique des mouvements d'une présentation
//	@Tags		Pharmacy - Mouvements
//	@Security	BearerAuth
//	@Produce	json
//	@Param		presentationId	path	int	true	"ID présentation"
//	@Success	200				{array}	StockMovement
//	@Router		/pharmacy/presentations/{presentationId}/stock-movements [get]
func (h *Handler) GetPresentationStockMovements(c *gin.Context) {
	presentationID, err := strconv.ParseUint(
		c.Param("presentationId"),
		10,
		64,
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant invalide",
		})
		return
	}

	movements, err := h.service.GetPresentationStockMovements(
		uint(presentationID),
	)

	if err != nil {
		if errors.Is(err, ErrPresentationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, movements)
}

// GetDispensations godoc
//
//	@Summary	Liste des délivrances pharmacie
//	@Tags		Pharmacy - Délivrance
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	PharmacyDispensation
//	@Router		/pharmacy/dispensations [get]
func (h *Handler) GetDispensations(c *gin.Context) {
	dispensations, err := h.service.GetDispensations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, dispensations)
}

// CreateDispensation godoc
//
//	@Summary		Créer une délivrance pharmacie
//	@Description	Seule la délivrance pharmacie diminue le stock. La consultation et la prescription ne diminuent jamais le stock.
//	@Tags			Pharmacy - Délivrance
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreateDispensationRequest	true	"Délivrance"
//	@Success		201		{object}	PharmacyDispensation
//	@Router			/pharmacy/dispensations [post]
func (h *Handler) CreateDispensation(c *gin.Context) {
	var req CreateDispensationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	rawUserID, exists := c.Get(rbac.ContextUserID)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "utilisateur non authentifié",
		})
		return
	}

	userID, ok := rawUserID.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "utilisateur invalide",
		})
		return
	}

	dispensation, err := h.service.CreateDispensation(req, userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrPresentationNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})

		case errors.Is(err, ErrStockNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})

		case errors.Is(err, ErrStockNotManaged):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

		case errors.Is(err, ErrInsufficientStock):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

		case errors.Is(err, ErrPrescriptionNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})

		case errors.Is(err, ErrPrescriptionPatientMismatch):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

		case errors.Is(err, ErrPrescriptionPresentationMismatch):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

		case errors.Is(err, ErrPrescriptionQuantityExceeded):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}

		return
	}

	c.JSON(http.StatusCreated, dispensation)
}

// GetPrescriptionDispensationStatus godoc
//
//	@Summary	Statut de délivrance d'une prescription
//	@Tags		Pharmacy - Ordonnances
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"ID prescription"
//	@Success	200	{object}	PrescriptionDispensationStatusResponse
//	@Router		/pharmacy/prescriptions/{id}/dispensation-status [get]
func (h *Handler) GetPrescriptionDispensationStatus(c *gin.Context) {
	prescriptionID, err := strconv.ParseUint(
		c.Param("id"),
		10,
		64,
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "identifiant de prescription invalide",
		})
		return
	}

	status, err := h.service.GetPrescriptionDispensationStatus(
		uint(prescriptionID),
	)
	if err != nil {
		if errors.Is(err, ErrPrescriptionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, status)
}

// GetPrescriptionQueue godoc
//
//	@Summary		File pharmacie des prescriptions
//	@Description	Retourne les prescriptions à délivrer avec filtre optionnel status=PENDING, PARTIAL ou COMPLETED.
//	@Tags			Pharmacy - Ordonnances
//	@Security		BearerAuth
//	@Produce		json
//	@Param			status	query	string	false	"Filtre statut"	Enums(PENDING, PARTIAL, COMPLETED)
//	@Success		200		{array}	PharmacyPrescriptionQueueItem
//	@Router			/pharmacy/prescriptions/pending [get]
func (h *Handler) GetPrescriptionQueue(c *gin.Context) {
	status := strings.ToUpper(strings.TrimSpace(c.Query("status")))

	items, err := h.service.GetPrescriptionQueue(status)
	if err != nil {
		if errors.Is(err, ErrInvalidPrescriptionStatus) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, items)
}
