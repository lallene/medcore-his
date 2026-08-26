package ticketing

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"gorm.io/gorm"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }
func fail(c *gin.Context, e error) {
	var a *coreerrors.AppError
	if errors.As(e, &a) {
		response.Error(c, a)
	} else {
		response.Error(c, coreerrors.Internal(e.Error()))
	}
}
func id(c *gin.Context) (uint, bool) {
	n, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || n == 0 {
		fail(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(n), true
}
func access(c *gin.Context) (Access, bool) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		fail(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return Access{}, false
	}
	a := Access{UserID: u, Permissions: map[string]bool{}}
	if p, ok := c.Get(rbac.ContextPermissions); ok {
		if values, ok := p.([]string); ok {
			for _, v := range values {
				a.Permissions[v] = true
			}
		}
	}
	return a, true
}
func (h *Handler) enrich(a *Access) {
	var sid *uint
	h.service.db.Raw("SELECT primary_service_id FROM staff_profiles WHERE user_id=? AND active", a.UserID).Scan(&sid)
	a.ServiceID = sid
}
func (h *Handler) Create(c *gin.Context) {
	var r CreateRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Ticket invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	x, e := h.service.Create(r, a.UserID)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}
func filter(c *gin.Context) Filter {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	l, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	return Filter{Search: c.Query("search"), Status: c.Query("status"), Priority: c.Query("priority"), Type: c.Query("type"), Category: c.Query("category"), Service: c.Query("service"), Assigned: c.Query("assigned"), Requester: c.Query("requester"), SLABreached: c.Query("slaBreached"), Page: p, Limit: l}
}
func (h *Handler) List(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.List(filter(c), a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Get(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Detail(n, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Update(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r UpdateRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Modification invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Update(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Comment(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r CommentRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Commentaire invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.AddComment(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) Comments(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Detail(n, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x.Comments)
}
func (h *Handler) Assign(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r AssignRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Assignation invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Assign(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Workflow(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r WorkflowRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Transition invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Workflow(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) KPIs(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.KPIs(a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Categories(c *gin.Context) {
	x, e := h.service.Categories()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Notifications(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	x, e := h.service.Notifications(a.UserID)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(http.StatusOK, x)
}
func (h *Handler) Agents(c *gin.Context) {
	x, e := h.service.Agents()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(http.StatusOK, x)
}
func (h *Handler) History(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Detail(n, a)
	if e != nil {
		fail(c, e)
		return
	}
	if !a.Has("ticket.audit.read") && !a.Has("ticket.update") {
		fail(c, coreerrors.Forbidden("Historique interdit"))
		return
	}
	c.JSON(200, x.History)
}

const maxAttachmentSize = 5 << 20

var allowedAttachmentMIME = map[string]bool{"image/png": true, "image/jpeg": true, "application/pdf": true, "text/plain; charset=utf-8": true, "text/plain": true}

func attachmentDir() string {
	if value := strings.TrimSpace(os.Getenv("TICKET_UPLOAD_DIR")); value != "" {
		return value
	}
	return filepath.Join("data", "ticket-attachments")
}

func (h *Handler) Upload(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	if _, err := h.service.Detail(n, a); err != nil {
		fail(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAttachmentSize+1024)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fail(c, coreerrors.Validation("Pièce jointe requise (5 Mo maximum)", nil))
		return
	}
	defer file.Close()
	buffer := make([]byte, 512)
	read, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		fail(c, err)
		return
	}
	mime := http.DetectContentType(buffer[:read])
	if !allowedAttachmentMIME[mime] {
		fail(c, coreerrors.Validation("Type de pièce jointe interdit", nil))
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		fail(c, err)
		return
	}
	original := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if original == "." || original == "" || strings.Contains(original, "..") || len(original) > 255 {
		fail(c, coreerrors.Validation("Nom de fichier dangereux", nil))
		return
	}
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "application/pdf": ".pdf", "text/plain": ".txt", "text/plain; charset=utf-8": ".txt"}[mime]
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		fail(c, err)
		return
	}
	stored := hex.EncodeToString(random) + ext
	dir := attachmentDir()
	if err = os.MkdirAll(dir, 0700); err != nil {
		fail(c, err)
		return
	}
	path := filepath.Join(dir, stored)
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(c, err)
		return
	}
	written, copyErr := io.Copy(output, io.LimitReader(file, maxAttachmentSize+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || written > maxAttachmentSize {
		_ = os.Remove(path)
		fail(c, coreerrors.Validation("Pièce jointe trop volumineuse", nil))
		return
	}
	item := Attachment{TicketID: n, UploadedBy: a.UserID, OriginalName: original, StoredName: stored, MIMEType: mime, Size: written, CreatedAt: time.Now()}
	err = h.service.db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&item).Error; e != nil {
			return e
		}
		return hist(tx, n, a.UserID, "ATTACHMENT_ADDED", "attachment", "", fmt.Sprint(item.ID), item.CreatedAt)
	})
	if err != nil {
		_ = os.Remove(path)
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) Download(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	attachmentID, err := strconv.ParseUint(c.Param("attachmentId"), 10, 64)
	if err != nil || attachmentID == 0 {
		fail(c, coreerrors.BadRequest("Pièce jointe invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	if _, err = h.service.Detail(n, a); err != nil {
		fail(c, err)
		return
	}
	var item Attachment
	if err = h.service.db.Where("id=? AND ticket_id=?", attachmentID, n).First(&item).Error; err != nil {
		fail(c, coreerrors.NotFound("ATTACHMENT"))
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", item.OriginalName))
	c.File(filepath.Join(attachmentDir(), item.StoredName))
}
