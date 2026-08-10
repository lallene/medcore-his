package hospitalizations

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"github.com/lallene/medcore-his/backend/internal/core/validator"
	"github.com/lallene/medcore-his/backend/internal/shared/pagination"
)

type BedHandler struct{ service *Service }

func NewBedHandler(service *Service) *BedHandler { return &BedHandler{service: service} }
func (h *BedHandler) ListRooms(c *gin.Context) {
	items, err := h.service.ListRooms()
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Chambres chargées", items)
}
func (h *BedHandler) FindRoom(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	item, err := h.service.FindRoom(id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Chambre chargée", item)
}
func (h *BedHandler) CreateRoom(c *gin.Context) {
	author, ok := authorID(c)
	if !ok {
		return
	}
	var req CreateRoomRequest
	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	item, err := h.service.CreateRoom(req, author)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Chambre créée", item)
}
func (h *BedHandler) UpdateRoom(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	author, ok := authorID(c)
	if !ok {
		return
	}
	var req UpdateRoomRequest
	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	item, err := h.service.UpdateRoom(id, req, author)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Chambre mise à jour", item)
}
func (h *BedHandler) ListBeds(c *gin.Context) {
	p := pagination.FromContext(c)
	f := BedFilter{Page: p.Page, Limit: p.Limit, Department: c.Query("department"), Status: strings.ToUpper(c.Query("status"))}
	if !optionalUint(c, "roomId", &f.RoomID) || !optionalBool(c, "active", &f.Active) || !optionalBool(c, "available", &f.Available) {
		return
	}
	result, err := h.service.ListBeds(f)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.SuccessWithMeta(c, "Lits chargés", result.Data, pagination.Meta{Page: result.Page, Limit: result.Limit, Total: result.Total, TotalPages: result.TotalPages})
}
func optionalUint(c *gin.Context, key string, target **uint) bool {
	raw := c.Query(key)
	if raw == "" {
		return true
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v == 0 {
		response.Error(c, coreerrors.BadRequest(key+" invalide"))
		return false
	}
	x := uint(v)
	*target = &x
	return true
}
func optionalBool(c *gin.Context, key string, target **bool) bool {
	raw := c.Query(key)
	if raw == "" {
		return true
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		response.Error(c, coreerrors.BadRequest(key+" invalide"))
		return false
	}
	*target = &v
	return true
}
func (h *BedHandler) FindBed(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	item, err := h.service.FindBed(id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Lit chargé", item)
}
func (h *BedHandler) CreateBed(c *gin.Context) {
	author, ok := authorID(c)
	if !ok {
		return
	}
	var req CreateBedRequest
	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	item, err := h.service.CreateBed(req, author)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Lit créé", item)
}
func (h *BedHandler) UpdateBed(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	author, ok := authorID(c)
	if !ok {
		return
	}
	var req UpdateBedRequest
	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	item, err := h.service.UpdateBed(id, req, author)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Lit mis à jour", item)
}
func (h *BedHandler) Assign(c *gin.Context)   { h.withBedRequest(c, false) }
func (h *BedHandler) Transfer(c *gin.Context) { h.withBedRequest(c, true) }
func (h *BedHandler) withBedRequest(c *gin.Context, transfer bool) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	author, ok := authorID(c)
	if !ok {
		return
	}
	var req AssignBedRequest
	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	var item *BedAssignment
	var err error
	if transfer {
		item, err = h.service.TransferBed(id, req.BedID, author)
	} else {
		item, err = h.service.AssignBed(id, req.BedID, author)
	}
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Affectation enregistrée", item)
}
func (h *BedHandler) ListAssignments(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListAssignments(id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Historique des lits chargé", items)
}
func (h *BedHandler) Release(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	author, ok := authorID(c)
	if !ok {
		return
	}
	item, err := h.service.ReleaseBed(id, author)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Lit libéré", item)
}
